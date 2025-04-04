import os
import shutil
import pytest
from unittest.mock import MagicMock, patch
from datetime import datetime
from core.services.ansible_test_runner_service import AnsibleTestRunnerService
from core.models import SnapshotMetadata, Snapshot, Artifact

ASSETS_DIR = os.path.join(os.path.dirname(os.path.dirname(__file__)), "assets")


@pytest.fixture
def snapshots_file(tmp_path):
    source_file = os.path.join(ASSETS_DIR, "release-candidates.yaml")
    dest_file = tmp_path / "release-candidates.yaml"
    shutil.copy(source_file, dest_file)
    return dest_file


@pytest.fixture
def default_artifact():
    return Artifact(
        repository="https://github.com/foo/bar",
        ref="abc123",
        name="foo/bar",
        versioning_selection_mechanism="release"
    )


@pytest.fixture
def service(snapshots_file, default_artifact):
    with patch("core.services.ansible_test_runner_service.AnsibleClient"), \
         patch("core.services.ansible_test_runner_service.ReleaseCandidateRepository"):
        svc = AnsibleTestRunnerService(snapshots_file)
        svc.logger = MagicMock()
        svc.ansible.run_playbook = MagicMock()
        svc.repo.find_all.return_value = [
            Snapshot(
                metadata=SnapshotMetadata(
                    id="abc", generated_at=datetime.now(), status="pending"
                ),
                artifacts=[default_artifact]
            )
        ]
        return svc

def test_test_runner_success(service):
    service.run()
    assert service.repo.update.call_count == 1
    updated = service.repo.update.call_args[0][0]
    assert updated.metadata.status == "successful"

def test_test_runner_failure(service):
    service.ansible.run_playbook.side_effect = RuntimeError("fail")
    service.run()
    updated = service.repo.update.call_args[0][0]
    assert updated.metadata.status == "failed"

@pytest.fixture
def service_with_no_pending(snapshots_file, default_artifact):
    with patch("core.services.ansible_test_runner_service.AnsibleClient"), \
         patch("core.services.ansible_test_runner_service.ReleaseCandidateRepository"):
        svc = AnsibleTestRunnerService(snapshots_file)
        svc.logger = MagicMock()
        
        # no pending snapshots
        svc.repo.find_all.return_value = [
            Snapshot(
                metadata=SnapshotMetadata(
                    id="abc", generated_at=datetime.now(), status="successful"
                ),
                artifacts=[default_artifact]
            )
        ]
        
        return svc


@pytest.fixture
def service_with_pending(snapshots_file):
    with patch("core.services.ansible_test_runner_service.AnsibleClient"), \
         patch("core.services.ansible_test_runner_service.ReleaseCandidateRepository"):
        service = AnsibleTestRunnerService(snapshots_file)
        service.logger = MagicMock()
        service.ansible.run_playbook = MagicMock()
        
        service.repo.find_all.return_value = [
            Snapshot(
                metadata=SnapshotMetadata(
                    id="abc", generated_at=datetime.now(), status="pending"
                ),
                artifacts=[
                    Artifact(
                        repository="https://github.com/kubernetes-sigs/cluster-api",
                        ref="ref",
                        name="kubernetes-sigs/cluster-api",
                        versioning_selection_mechanism="release"
                    ),
                    Artifact(
                        repository="https://github.com/metal3-io/cluster-api-provider-metal3",
                        ref="ref",
                        name="metal3-io/cluster-api-provider-metal3",
                        versioning_selection_mechanism="release"
                    ),
                    Artifact(
                        repository="https://github.com/openshift/assisted-service",
                        ref="ref",
                        name="openshift/assisted-service",
                        versioning_selection_mechanism="commit",
                        image_url="quay.io/edge-infrastructure/assisted-service:latest-tag"
                    ),
                    Artifact(
                        repository="https://github.com/openshift/assisted-image-service",
                        ref="ref",
                        name="openshift/assisted-image-service",
                        versioning_selection_mechanism="commit",
                        image_url="quay.io/edge-infrastructure/assisted-image-service:latest-tag"
                    ),
                    Artifact(
                        repository="https://github.com/openshift/assisted-installer-agent",
                        ref="ref",
                        name="openshift/assisted-installer-agent",
                        versioning_selection_mechanism="commit",
                        image_url="quay.io/edge-infrastructure/assisted-installer-agent:latest-tag"
                    ),
                    Artifact(
                        repository="https://github.com/openshift/assisted-installer",
                        ref="ref",
                        name="openshift/assisted-installer-controller",
                        versioning_selection_mechanism="commit",
                        image_url="quay.io/edge-infrastructure/assisted-installer-controller:latest-tag"
                    ),
                    Artifact(
                        repository="https://github.com/openshift/assisted-installer",
                        ref="ref",
                        name="openshift/assisted-installer",
                        versioning_selection_mechanism="commit",
                        image_url="quay.io/edge-infrastructure/assisted-installer:latest-tag"
                    )
                ]
            )
        ]
        
        return service


def test_no_pending_snapshots(service_with_no_pending):
    service_with_no_pending.run()
    assert not service_with_no_pending.ansible.run_playbook.called
    assert "No pending snapshot found" in service_with_no_pending.logger.info.call_args[0][0]


def test_export_env_variables(service_with_pending):
    with patch.dict(os.environ, {}, clear=True):
        service_with_pending.export_env(service_with_pending.repo.find_all()[0])
        
        assert os.environ.get("CAPI_VERSION") == "ref"
        assert os.environ.get("CAPM3_VERSION") == "ref"
        assert os.environ.get("ASSISTED_SERVICE_IMAGE") == "quay.io/edge-infrastructure/assisted-service:latest-tag"
        assert os.environ.get("ASSISTED_IMAGE_SERVICE_IMAGE") == "quay.io/edge-infrastructure/assisted-image-service:latest-tag"
        assert os.environ.get("ASSISTED_INSTALLER_AGENT_IMAGE") == "quay.io/edge-infrastructure/assisted-installer-agent:latest-tag"
        assert os.environ.get("ASSISTED_INSTALLER_CONTROLLER_IMAGE") == "quay.io/edge-infrastructure/assisted-installer-controller:latest-tag"
        assert os.environ.get("ASSISTED_INSTALLER_IMAGE") == "quay.io/edge-infrastructure/assisted-installer:latest-tag"


def test_successful_test_run(service_with_pending):
    service_with_pending.run()
    
    # Verify playbook was run
    service_with_pending.ansible.run_playbook.assert_called_with(
        "test/ansible/run_test.yaml", "test/ansible/inventory.yaml"
    )
    
    # Verify snapshot was updated
    assert service_with_pending.repo.update.call_count == 1
    updated_snapshot = service_with_pending.repo.update.call_args[0][0]
    assert updated_snapshot.metadata.status == "successful"
    assert len(updated_snapshot.artifacts) == 7


def test_failed_test_run(service_with_pending):
    service_with_pending.ansible.run_playbook.side_effect = RuntimeError("Ansible test failed")
    
    service_with_pending.run()
    
    # Verify snapshot was updated with failed status
    assert service_with_pending.repo.update.call_count == 1
    updated_snapshot = service_with_pending.repo.update.call_args[0][0]
    
    assert updated_snapshot.metadata.status == "failed"
