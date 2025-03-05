import datetime
import os
import shutil
import tempfile
import uuid
import pytest
from unittest.mock import patch, MagicMock

from core.services.version_discovery_service import (
    VersionDiscoveryService,
)


ASSETS_DIR = os.path.join(os.path.dirname(os.path.dirname(__file__)), "assets")


@pytest.fixture
def mock_uuid():
    fixed_id = "fixed-uuid-for-testing"
    with patch("uuid.uuid4", return_value=uuid.UUID(int=0)):
        yield fixed_id


@pytest.fixture
def mock_datetime():
    fixed_time = datetime.datetime(2025, 3, 15, 12, 0, 0)
    with patch("core.services.version_discovery_service.datetime") as mock_dt:
        mock_dt.now.return_value = fixed_time
        yield fixed_time


def create_mock_repo(name: str, tag: str, is_prerelease: bool = False):
    mock_repo = MagicMock()
    mock_release = MagicMock(name=name, tag_name=tag, prerelease=is_prerelease)
    mock_repo.get_latest_release.return_value = mock_release
    return mock_repo


@pytest.fixture
def temp_snapshot_file():
    tempdir = tempfile.mkdtemp()
    target = os.path.join(tempdir, "release-candidates.yaml")
    yield target
    shutil.rmtree(tempdir)

@pytest.fixture
def temp_components_file():
    return f"{ASSETS_DIR}/components.yaml"

@pytest.fixture
def mock_github():
    with patch("core.services.version_discovery_service.GitHubClient") as p:
        yield p.return_value


@pytest.fixture
def mock_registry():
    with patch("core.services.version_discovery_service.ImageRegistryClient") as p:
        yield p.return_value


@pytest.fixture
def mock_rc_repo():
    with patch(
        "core.services.version_discovery_service.ReleaseCandidateRepository"
    ) as p:
        yield p.return_value


def test_discovery_success(mock_github, mock_registry, mock_rc_repo, temp_snapshot_file, mock_datetime, temp_components_file):
    svc = VersionDiscoveryService(temp_snapshot_file, temp_components_file)
    svc.github = mock_github
    svc.registry = mock_registry
    svc.rc_repository = mock_rc_repo

    # mocks for repositories with releases
    capi_repo = create_mock_repo("cluster-api", "v1.9.5")
    capm3_repo = create_mock_repo("cluster-api-provider-metal3", "v1.9.3")
    
    # repos without releases but with images
    assisted_service_repo = MagicMock()
    assisted_service_repo.get_latest_release.return_value = None
    service_commit = MagicMock(sha="76d29d2a7f0899dcede9700fc88fcbad37b6ccca")
    assisted_service_repo.get_commits.return_value = [service_commit]
    
    assisted_image_service_repo = MagicMock()
    assisted_image_service_repo.get_latest_release.return_value = None
    image_service_commit = MagicMock(sha="2249c85d05600191b24e93dd92e733d49a1180ec")
    assisted_image_service_repo.get_commits.return_value = [image_service_commit]
    
    assisted_installer_agent_repo = MagicMock()
    assisted_installer_agent_repo.get_latest_release.return_value = None
    agent_commit = MagicMock(sha="cfe93a9779dea6ad2a628280b40071d23f3cb429")
    assisted_installer_agent_repo.get_commits.return_value = [agent_commit]
    
    assisted_installer_repo = MagicMock()
    assisted_installer_repo.get_latest_release.return_value = None
    installer_commit = MagicMock(sha="c389a38405383961d26191799161c86127451635")
    assisted_installer_repo.get_commits.return_value = [installer_commit]
    
    # configure get_repo to return the appropriate repositories
    def get_repo_side_effect(name):
        repos = {
            "kubernetes-sigs/cluster-api": capi_repo,
            "metal3-io/cluster-api-provider-metal3": capm3_repo,
            "openshift/assisted-service": assisted_service_repo,
            "openshift/assisted-image-service": assisted_image_service_repo,
            "openshift/assisted-installer-agent": assisted_installer_agent_repo,
            "openshift/assisted-installer": assisted_installer_repo
        }
        return repos.get(name)
    
    mock_github.get_repo.side_effect = get_repo_side_effect
    
    # configure registry.exists to return True for specific image tags
    def registry_exists_side_effect(image, tag):
        valid_combinations = [
            ("quay.io/edge-infrastructure/assisted-service", "latest-76d29d2a7f0899dcede9700fc88fcbad37b6ccca"),
            ("quay.io/edge-infrastructure/assisted-image-service", "latest-2249c85d05600191b24e93dd92e733d49a1180ec"),
            ("quay.io/edge-infrastructure/assisted-installer-agent", "latest-cfe93a9779dea6ad2a628280b40071d23f3cb429"),
            ("quay.io/edge-infrastructure/assisted-installer-controller", "latest-c389a38405383961d26191799161c86127451635"),
            ("quay.io/edge-infrastructure/assisted-installer", "latest-c389a38405383961d26191799161c86127451635")
        ]
        return (image, tag) in valid_combinations
    
    mock_registry.exists.side_effect = registry_exists_side_effect
    
    svc.run()
    
    assert mock_rc_repo.save.called
    snapshot = mock_rc_repo.save.call_args[0][0]
    
    assert snapshot.metadata.status == "pending"
    assert snapshot.metadata.generated_at == mock_datetime
    
    assert len(snapshot.artifacts) == 7
    
    assert snapshot.artifacts[0].repository == "https://github.com/kubernetes-sigs/cluster-api"
    assert snapshot.artifacts[0].ref == "v1.9.5"
    assert snapshot.artifacts[0].image_url is None
    
    assert snapshot.artifacts[1].repository == "https://github.com/metal3-io/cluster-api-provider-metal3"
    assert snapshot.artifacts[1].ref == "v1.9.3"
    assert snapshot.artifacts[1].image_url is None
    
    assert snapshot.artifacts[2].repository == "https://github.com/openshift/assisted-service"
    assert snapshot.artifacts[2].ref == "76d29d2a7f0899dcede9700fc88fcbad37b6ccca"
    assert snapshot.artifacts[2].image_url == "quay.io/edge-infrastructure/assisted-service:latest-76d29d2a7f0899dcede9700fc88fcbad37b6ccca"
    
    assert snapshot.artifacts[3].repository == "https://github.com/openshift/assisted-image-service"
    assert snapshot.artifacts[3].ref == "2249c85d05600191b24e93dd92e733d49a1180ec"
    assert snapshot.artifacts[3].image_url == "quay.io/edge-infrastructure/assisted-image-service:latest-2249c85d05600191b24e93dd92e733d49a1180ec"
    
    assert snapshot.artifacts[4].repository == "https://github.com/openshift/assisted-installer-agent"
    assert snapshot.artifacts[4].ref == "cfe93a9779dea6ad2a628280b40071d23f3cb429"
    assert snapshot.artifacts[4].image_url == "quay.io/edge-infrastructure/assisted-installer-agent:latest-cfe93a9779dea6ad2a628280b40071d23f3cb429"
    
    installer_components = [c for c in snapshot.artifacts if c.repository == "https://github.com/openshift/assisted-installer"]
    assert len(installer_components) == 2
    
    installer_image_urls = [c.image_url for c in installer_components]
    assert "quay.io/edge-infrastructure/assisted-installer-controller:latest-c389a38405383961d26191799161c86127451635" in installer_image_urls
    assert "quay.io/edge-infrastructure/assisted-installer:latest-c389a38405383961d26191799161c86127451635" in installer_image_urls

def test_discovery_no_components_found(mock_github, mock_rc_repo, temp_snapshot_file, temp_components_file):
    service = VersionDiscoveryService(temp_snapshot_file, temp_components_file)
    
    def get_repo_with_no_data(name):
        mock_rc_repo = MagicMock()
        mock_rc_repo.get_latest_release.return_value = None
        mock_rc_repo.get_commits.return_value = []
        return mock_rc_repo
    
    mock_github.get_repo.side_effect = get_repo_with_no_data
    
    with pytest.raises(Exception, match="No components discovered. Exiting."):
        service.run()
    
    assert not mock_rc_repo.save.called

def test_discovery_github_api_failure(mock_github, mock_rc_repo, temp_snapshot_file, temp_components_file):
    service = VersionDiscoveryService(temp_snapshot_file, temp_components_file)
    
    mock_github.get_repo.side_effect = Exception("GitHub API error")
    
    with pytest.raises(Exception, match="Failed to resolve component: Failed to process kubernetes-sigs/cluster-api"):
        service.run()
    
    assert not mock_rc_repo.save.called

def test_discovery_repository_save_failure(mock_github, mock_rc_repo, temp_snapshot_file, temp_components_file):
    service = VersionDiscoveryService(temp_snapshot_file, temp_components_file)
    mock_repo = create_mock_repo("cluster-api", "v1.9.5")
    mock_github.get_repo.return_value = mock_repo
    mock_rc_repo.save.return_value = False
    with pytest.raises(Exception, match="Failed to save snapshot"):
        service.run()
    assert mock_rc_repo.save.called
