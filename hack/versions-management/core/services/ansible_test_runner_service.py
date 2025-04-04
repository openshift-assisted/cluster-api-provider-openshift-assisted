import logging
import os
from dataclasses import replace
from typing import override
from zlib import compressobj

from core.clients.ansible_client import AnsibleClient
from core.models import Snapshot
from core.repositories import ReleaseCandidateRepository
from core.services.service import Service
from core.utils.logging import setup_logger


class AnsibleTestRunnerService(Service):
    def __init__(self, file_path: str):
        self.repo: ReleaseCandidateRepository = ReleaseCandidateRepository(file_path)
        self.ansible: AnsibleClient = AnsibleClient()
        self.logger: logging.Logger = setup_logger("AnsibleTestRunnerService")

    @override
    def run(self) -> None:
        snapshots = self.repo.find_all()
        pending_snapshot = next((s for s in snapshots if s.metadata.status == "pending"), None)

        if not pending_snapshot:
            self.logger.info("No pending snapshot found")
            return

        self.export_env(pending_snapshot)

        try:
            self.ansible.run_playbook("test/ansible/run_test.yaml", "test/ansible/inventory.yaml")
            updated = replace(pending_snapshot.metadata, status="successful")
        except Exception:
            updated = replace(pending_snapshot.metadata, status="failed")

        updated_snapshot = Snapshot(metadata=updated, artifacts=pending_snapshot.artifacts)
        self.repo.update(updated_snapshot)
        
    def export_env(self, snapshot: Snapshot) -> None:
        name_map = {
            "kubernetes-sigs/cluster-api": "CAPI_VERSION",
            "metal3-io/cluster-api-provider-metal3": "CAPM3_VERSION",
            "quay.io/edge-infrastructure/assisted-service": "ASSISTED_SERVICE_IMAGE",
            "quay.io/edge-infrastructure/assisted-image-service": "ASSISTED_IMAGE_SERVICE_IMAGE",
            "quay.io/edge-infrastructure/assisted-installer-agent": "ASSISTED_INSTALLER_AGENT_IMAGE",
            "quay.io/edge-infrastructure/assisted-installer-controller": "ASSISTED_INSTALLER_CONTROLLER_IMAGE",
            "quay.io/edge-infrastructure/assisted-installer": "ASSISTED_INSTALLER_IMAGE",
        }

        for comp in snapshot.artifacts:
            match comp.versioning_selection_mechanism:
                case "release": 
                    env_key = name_map.get(comp.name)
                    if env_key:
                        os.environ[env_key] = comp.ref
                        self.logger.info(f"Exported {env_key}={comp.ref}")
                case "commit":
                    image_key = comp.image_url.split(":")[0]
                    env_key = name_map.get(image_key)
                    if env_key:
                        os.environ[env_key] = comp.image_url
                        self.logger.info(f"Exported {env_key}={comp.image_url}")
                case _:
                    raise Exception("Unsupported versioning selection mechanism")
