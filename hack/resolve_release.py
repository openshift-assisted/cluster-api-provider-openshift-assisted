#!/usr/bin/env python3
import argparse
import json
import logging
import re
import sys
from urllib.error import URLError
from urllib.request import urlopen

"""
Resolve OCP release names and RHCOS image URLs for the CAPI e2e tests.

OCP versions are resolved from the OpenShift release controller, selecting
the releasestream by major version ("{major}-stable") and falling back to
"{major}-dev-preview" for pre-GA (ec/rc) builds. RHCOS images are resolved
from the OpenShift mirror, preferring the stable per-minor directory and
falling back to the shared pre-release directory.

Uses only the Python standard library so it can run inside the CI test
step without installing any dependencies.

Usage:
    # Resolve a major.minor (e.g. 5.0) to a full release name (e.g. 5.0.0-rc.4)
    python resolve_release.py version --version 5.0

    # Resolve the RHCOS nutanix qcow2 image URL for a major.minor
    python resolve_release.py rhcos --version 5.0

The resolved value is printed to stdout so it can be captured in shell:
    OPENSHIFT_VERSION="$(python resolve_release.py version --version 5.0)"
"""

logging.basicConfig(
    format="%(asctime)s %(levelname)s: %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S", level=logging.INFO
)
logger = logging.getLogger(__name__)

RELEASE_API = (
    "https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestream"
)
RHCOS_MIRROR = (
    "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos"
)


def _get(url):
    try:
        with urlopen(url, timeout=30) as response:
            if response.status == 200:
                return response.read().decode("utf-8")
            logger.warning(
                "GET %s returned status code %s", url, response.status)
    except URLError as e:
        logger.error("Error fetching %s: %s", url, e)
    except Exception as e:
        logger.error("Error fetching %s: %s", url, e)
    return None


def _version_key(name):
    """Split a version string into a sortable list so rc.10 > rc.2."""
    return [int(p) if p.isdigit() else p for p in re.split(r"[.\-]", name)]


def _latest_dir(base_url, prefix):
    """Return highest version-sorted dir under base_url matching prefix."""
    body = _get(base_url + "/")
    if not body:
        return None
    dirs = re.findall(r'href="(' + re.escape(prefix) + r'[^"/]*)/"', body)
    if not dirs:
        return None
    return sorted(dirs, key=_version_key)[-1]


def resolve_ocp_version(major_minor):
    """
    Resolve a major.minor version (e.g. "5.0") to a full release name
    (e.g. "5.0.0-rc.4"), or None when no stream has a matching release.
    """
    major = major_minor.split(".")[0]
    minor = int(major_minor.split(".")[1])
    query = "latest?in=%3E{mm}.0-0+%3C{maj}.{next}.0-0".format(
        mm=major_minor, maj=major, next=minor + 1)

    streams = (
        "{maj}-stable".format(maj=major),
        "{maj}-dev-preview".format(maj=major),
    )
    for stream in streams:
        body = _get("{api}/{stream}/{query}".format(
            api=RELEASE_API, stream=stream, query=query))
        if not body:
            continue
        try:
            name = json.loads(body).get("name")
        except ValueError:
            logger.warning("Non-JSON response from stream %s", stream)
            continue
        if name:
            logger.info(
                "Resolved %s to %s (stream: %s)", major_minor, name, stream)
            return name
        logger.info("No match in %s, trying next stream", stream)

    logger.error("Failed to resolve OCP version for %s", major_minor)
    return None


def resolve_rhcos_url(major_minor):
    """
    Resolve the RHCOS nutanix qcow2 image URL for a major.minor version,
    or None when no matching directory or image is published on the mirror.
    """
    candidates = [
        "{mirror}/{mm}".format(mirror=RHCOS_MIRROR, mm=major_minor),
        "{mirror}/pre-release".format(mirror=RHCOS_MIRROR),
    ]

    files_base = None
    version_dir = None
    for base in candidates:
        version_dir = _latest_dir(base, major_minor)
        if version_dir:
            files_base = base
            if base.endswith("pre-release"):
                logger.info("Using pre-release RHCOS mirror")
            break

    if not version_dir:
        logger.error(
            "No RHCOS version found for %s in stable or pre-release mirrors "
            "(pre-release images may not be published yet)", major_minor)
        return None

    files_url = "{base}/{ver}/".format(base=files_base, ver=version_dir)
    body = _get(files_url)
    if not body:
        return None
    match = re.search(r'href="(rhcos-[^"]*-nutanix[^"]*\.qcow2)"', body)
    if not match:
        logger.error("No RHCOS nutanix qcow2 image found at %s", files_url)
        return None

    url = files_url + match.group(1)
    logger.info("RHCOS Image URL: %s", url)
    return url


def _major_minor(version):
    match = re.match(r"^(\d+\.\d+)", version)
    if not match:
        raise ValueError("Not a valid version: {v}".format(v=version))
    return match.group(1)


def main():
    parser = argparse.ArgumentParser(description="OCP/RHCOS release resolver")
    parser.add_argument(
        "mode", choices=["version", "rhcos"], help="what to resolve")
    parser.add_argument(
        "--version", required=True,
        help="OCP version or major.minor, e.g. 5.0")
    args = parser.parse_args()

    try:
        major_minor = _major_minor(args.version)
    except ValueError as e:
        logger.error("%s", e)
        return 1

    if args.mode == "version":
        result = resolve_ocp_version(major_minor)
    else:
        result = resolve_rhcos_url(major_minor)

    if not result:
        return 1
    print(result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
