#!/usr/bin/env python3
import os
import sys
from core.services.version_discovery_service import VersionDiscoveryService
from core.utils.logging import setup_logger

ROOT_DIR = os.path.dirname(os.path.dirname(os.path.dirname(__file__)))


def main():
    logger = setup_logger("VersionDiscovery")
    try:
        rc_file = os.environ.get("RELEASE_CANDIDATES_FILE", f"{ROOT_DIR}/release-candidates.yaml")
        components_file = os.environ.get("COMPONENTS_FILE", f"{ROOT_DIR}/components.yaml")
        logger.info(f"Starting version discovery with RC file: {rc_file} and components file {components_file}")
        service = VersionDiscoveryService(rc_file, components_file)
        service.run()
        logger.info("Version discovery completed successfully")
        return 0
    except Exception as e:
        logger.error(f"Version discovery failed: {e}")
        return 1


if __name__ == "__main__":
    sys.exit(main())
