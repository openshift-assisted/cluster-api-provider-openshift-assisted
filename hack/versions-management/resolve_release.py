#!/usr/bin/env python3
from __future__ import annotations

import argparse
import re
import sys

from core.clients.release_resolver_client import ReleaseResolverClient
from core.utils.logging import setup_logger

"""
Resolve OCP release names and RHCOS image URLs for the CAPI on K8s pipeline.

Usage:
    # Resolve a major.minor (e.g. 5.1) to a full release name (e.g. 5.1.0-ec.0)
    python resolve_release.py version --version 5.1

    # Resolve the RHCOS nutanix qcow2 image URL for a major.minor
    python resolve_release.py rhcos --version 5.1

The resolved value is printed to stdout so it can be captured in shell:
    OPENSHIFT_VERSION="$(python resolve_release.py version --version 5.1)"
"""


def _major_minor(version: str) -> str:
    match = re.match(r"^(\d+\.\d+)", version)
    if not match:
        raise ValueError(f"Not a valid version: {version}")
    return match.group(1)


def main() -> int:
    parser = argparse.ArgumentParser(description="OCP/RHCOS release resolver")
    parser.add_argument("mode", choices=["version", "rhcos"], help="what to resolve")
    parser.add_argument("--version", required=True, help="OCP version or major.minor, e.g. 5.1")
    args = parser.parse_args()

    logger = setup_logger("ReleaseResolver")
    try:
        major_minor = _major_minor(args.version)
    except ValueError as e:
        logger.error(f"{e}")
        return 1

    client = ReleaseResolverClient()
    if args.mode == "version":
        result = client.resolve_ocp_version(major_minor)
    else:
        result = client.resolve_rhcos_url(major_minor)

    if not result:
        return 1
    print(result)
    return 0


if __name__ == "__main__":
    sys.exit(main())
