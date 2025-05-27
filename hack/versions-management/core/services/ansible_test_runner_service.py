import json
import logging
import os
from dataclasses import replace
from typing import Callable, override

from core.clients.ansible_client import AnsibleClient
from core.models import Snapshot
from core.repositories import ReleaseCandidateRepository
from core.services.service import Service
from core.utils.logging import setup_logger


class AnsibleTestRunnerService(Service):
    def __init__(self, file_path: str, pending_snapshot_id: str, dry_run: bool = False):
        self.repo: ReleaseCandidateRepository = ReleaseCandidateRepository(file_path)
        self.ansible: AnsibleClient = AnsibleClient()
        self.logger: logging.Logger = setup_logger("AnsibleTestRunnerService")
        self.pending_snapshot_id: str = pending_snapshot_id
        self.dry_run: bool = dry_run

    def get_execution_parameters(self) -> tuple[str, str, Callable[[Snapshot], bool | None]]:
        if self.dry_run:
            return (
                "test/playbooks/no_op.yaml",
                "test/playbooks/inventories/local_host.yaml",
                lambda snapshot: print(json.dumps(snapshot))
            )
        return (
            "test/playbooks/run_test.yaml", 
            "test/playbooks/inventories/remote_host.yaml",
            lambda snapshot: self.repo.update(snapshot)
        )

    @override
    def run(self) -> None:
        pending_snapshot = self.repo.find_by_id(self.pending_snapshot_id)

        if not pending_snapshot:
            self.logger.info("No pending snapshot found")
            return

        playbook, inventory, on_success = self.get_execution_parameters()
        self.export_env(pending_snapshot)

        try:
            self.ansible.run_playbook(playbook, inventory)
            updated = replace(pending_snapshot.metadata, status="successful")
        except Exception:
            updated = replace(pending_snapshot.metadata, status="failed")

        updated_snapshot = Snapshot(metadata=updated, artifacts=pending_snapshot.artifacts)
        on_success(updated_snapshot)
        

    def export_env(self, snapshot: Snapshot) -> None:
        names_map = self.get_env_var_map()
        for comp in snapshot.artifacts:
            match comp.versioning_selection_mechanism:
                case "release": 
                    env_key = names_map.get(comp.name, {}).get("version")
                    if env_key:
                        os.environ[env_key] = comp.ref
                        self.logger.info(f"Exported {env_key}={comp.ref}")
                case "commit":
                    key = comp.name
                    image_key = names_map.get(key, {}).get("image")
                    version_key = names_map.get(key, {}).get("version")
                    if image_key and version_key and comp.image_url and comp.image_digest:
                        os.environ[image_key] = comp.image_url
                        os.environ[version_key] = comp.image_digest
                        self.logger.info(f"Exported {image_key}={comp.image_url}, {version_key}={comp.image_digest}")
                    else:
                        self.logger.warning(f"No environment variable mapping found for {comp.name}")
                case _:
                    raise Exception(f"Unsupported versioning selection mechanism: {comp.versioning_selection_mechanism}") 


    def get_env_var_map(self) -> dict[str, dict[str, str]]:
        return {
            "kubernetes-sigs/cluster-api": {
                "version": "CAPI_VERSION",
            },
            "metal3-io/cluster-api-provider-metal3": {
                "version": "CAPM3_VERSION",
            },
            "openshift/assisted-service": {
                "image": "ASSISTED_SERVICE_IMAGE",
                "version": "ASSISTED_SERVICE_VERSION",
            },
            "openshift/assisted-service-el8": {
                "image": "ASSISTED_SERVICE_EL8_IMAGE",
                "version": "ASSISTED_SERVICE_EL8_VERSION",
            },
            "openshift/assisted-image-service": {
                "image": "ASSISTED_IMAGE_SERVICE_IMAGE",
                "version": "ASSISTED_IMAGE_SERVICE_VERSION",
            },
            "openshift/assisted-installer-agent": {
                "image": "ASSISTED_INSTALLER_AGENT_IMAGE",
                "version": "ASSISTED_INSTALLER_AGENT_VERSION",
            },
            "openshift/assisted-installer": {
                "image": "ASSISTED_INSTALLER_IMAGE",
                "version": "ASSISTED_INSTALLER_VERSION",
            },
            "openshift/assisted-installer-controller": {
                "image": "ASSISTED_INSTALLER_CONTROLLER_IMAGE",
                "version": "ASSISTED_INSTALLER_CONTROLLER_VERSION",
            },
        }

