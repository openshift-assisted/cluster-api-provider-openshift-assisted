#!/usr/bin/env python3
import os
import sys
from core.services.ansible_test_runner_service import AnsibleTestRunnerService
from core.utils.logging import setup_logger

ROOT_DIR = os.path.dirname(os.path.dirname(os.path.dirname(__file__)))

def main():
    logger = setup_logger("AnsibleTestRunner")
    
    try:
        rc_file = os.environ.get("RELEASE_CANDIDATES_FILE", f"{ROOT_DIR}/release-candidates.yaml")
        logger.info(f"Starting ansible test runner with RC file: {rc_file}")
        service = AnsibleTestRunnerService(rc_file)
        service.run()
        logger.info("Ansible test run completed successfully")
        return 0
    except Exception as e:
        logger.error(f"Ansible test run failed: {e}")
        return 1

if __name__ == "__main__":
    sys.exit(main())
