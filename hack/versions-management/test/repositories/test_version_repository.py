import pytest
import os
import shutil

from core.repositories.version_repository import VersionRepository

ASSETS_DIR = os.path.join(os.path.dirname(os.path.dirname(__file__)), "assets")


@pytest.fixture
def versions_file(tmp_path):
    source_file = os.path.join(ASSETS_DIR, "versions.yaml")
    dest_file: str = tmp_path / "versions.yaml"
    shutil.copy(source_file, dest_file)
    return dest_file


def test_version_repository_find_all(versions_file):
    repo = VersionRepository(str(versions_file))
    versions = repo.find_all()

    assert len(versions) == 1
    version = versions[0]

    assert version.name == "v0.0.1"
    assert len(version.artifacts) == 7

    assert version.artifacts[0].repository == "https://github.com/kubernetes-sigs/cluster-api"
    assert version.artifacts[2].image_url.startswith("quay.io/edge-infrastructure/assisted-service")


def test_invalid_versions_file_schema(tmp_path):
    bad_file = tmp_path / "bad_versions.yaml"
    bad_file.write_text("versions:\n  - name: 1.0.0")

    repo = VersionRepository(str(bad_file))
    with pytest.raises(ValueError, match="Invalid versions.yaml structure"):
        repo.find_all()
