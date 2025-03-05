#!/usr/bin/env python3
import os
import sys
import argparse
from core.services.tag_reconciliation_service import TagReconciliationService
from core.utils.logging import setup_logger

ROOT_DIR = os.path.dirname(os.path.dirname(os.path.dirname(__file__)))

def main():
    parser = argparse.ArgumentParser(description='Tag Reconciliation Service')
    parser.add_argument('--dry-run', action='store_true', help='Run in dry-run mode without making any changes')
    args = parser.parse_args()
    
    logger = setup_logger("TagReconciler")
    
    try:
        versions_file = os.environ.get("VERSIONS_FILE", f"{ROOT_DIR}/versions.yaml")
        logger.info(f"Starting tag reconciliationer with versions file: {versions_file}")
        service = TagReconciliationService(versions_file, dry_run=args.dry_run)
        service.run()
        logger.info("Tag reconciliation run completed successfully")
        return 0
    except Exception as e:
        logger.error(f"Tag reconciliation run failed: {e}")
        return 1

if __name__ == "__main__":
    sys.exit(main())
