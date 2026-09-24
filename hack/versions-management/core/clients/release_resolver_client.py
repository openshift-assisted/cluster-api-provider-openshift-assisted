from __future__ import annotations

import json
import re
import logging

import requests

logger = logging.getLogger(__name__)


class ReleaseResolverClient:
    """
    Resolves OCP releases and RHCOS image URLs for the CAPI on K8s pipeline.

    OCP versions are resolved from the OpenShift release controller, selecting
    the releasestream by major version ("{major}-stable") and falling back to
    "{major}-dev-preview" for pre-GA (ec/rc) builds. RHCOS images are resolved
    from the OpenShift mirror, preferring the stable per-minor directory and
    falling back to the shared pre-release directory.
    """

    def __init__(
        self,
        release_api: str = "https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestream",
        rhcos_mirror: str = "https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos",
    ):
        self.release_api: str = release_api
        self.rhcos_mirror: str = rhcos_mirror

    def _get(self, url: str) -> str | None:
        try:
            response = requests.get(url=url, timeout=30)
            if response.status_code == 200:
                return response.text
            logger.warning(f"GET {url} returned status code {response.status_code}")
        except Exception as e:
            logger.error(f"Error fetching {url}: {e}")
        return None

    def resolve_ocp_version(self, major_minor: str) -> str | None:
        """
        Resolve a major.minor version (e.g. "5.1") to a full release name
        (e.g. "5.1.0-ec.0"), or None when no stream has a matching release.
        """
        major = major_minor.split(".")[0]
        minor = int(major_minor.split(".")[1])
        query = f"latest?in=%3E{major_minor}.0-0+%3C{major}.{minor + 1}.0-0"

        for stream in (f"{major}-stable", f"{major}-dev-preview"):
            body = self._get(f"{self.release_api}/{stream}/{query}")
            if not body:
                continue
            try:
                name = json.loads(body).get("name")
            except ValueError:
                logger.warning(f"Non-JSON response from stream {stream}")
                continue
            if name:
                logger.info(f"Resolved {major_minor} to {name} (stream: {stream})")
                return name
            logger.info(f"No match in {stream}, trying next stream")

        logger.error(f"Failed to resolve OCP version for {major_minor}")
        return None

    def resolve_rhcos_url(self, major_minor: str) -> str | None:
        """
        Resolve the RHCOS nutanix qcow2 image URL for a major.minor version,
        or None when no matching directory or image is published on the mirror.
        """
        candidates = [
            f"{self.rhcos_mirror}/{major_minor}",
            f"{self.rhcos_mirror}/pre-release",
        ]

        files_base = None
        version_dir = None
        for base in candidates:
            version_dir = self._latest_dir(base, major_minor)
            if version_dir:
                files_base = base
                if base.endswith("pre-release"):
                    logger.info("Using pre-release RHCOS mirror")
                break

        if not version_dir:
            logger.error(
                f"No RHCOS version found for {major_minor} in stable or pre-release "
                f"mirrors (pre-release images may not be published yet)"
            )
            return None

        files_url = f"{files_base}/{version_dir}/"
        body = self._get(files_url)
        if not body:
            return None
        match = re.search(r'href="(rhcos-[^"]*-nutanix[^"]*\.qcow2)"', body)
        if not match:
            logger.error(f"No RHCOS nutanix qcow2 image found at {files_url}")
            return None

        url = files_url + match.group(1)
        logger.info(f"RHCOS Image URL: {url}")
        return url

    def _latest_dir(self, base_url: str, prefix: str) -> str | None:
        """Return the highest version-sorted directory under base_url matching prefix."""
        body = self._get(f"{base_url}/")
        if not body:
            return None
        dirs = re.findall(rf'href="({re.escape(prefix)}[^"/]*)/"', body)
        if not dirs:
            return None
        return sorted(dirs, key=self._version_key)[-1]

    @staticmethod
    def _version_key(name: str) -> list:
        """Split a version string into a sortable list so rc.10 > rc.2."""
        return [int(p) if p.isdigit() else p for p in re.split(r"[.\-]", name)]
