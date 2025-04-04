import pytest
import os
import shutil
from datetime import datetime
from core.models import Snapshot, SnapshotMetadata, Artifact
from core.repositories import ReleaseCandidateRepository

ASSETS_DIR = os.path.join(os.path.dirname(os.path.dirname(__file__)), "assets")


@pytest.fixture
def snapshots_file(tmp_path):
    source_file = os.path.join(ASSETS_DIR, "release-candidates.yaml")
    dest_file = tmp_path / "release-candidates.yaml"
    shutil.copy(source_file, dest_file)
    return dest_file

def test_release_repository_find_all_file_not_exists():
    repo = ReleaseCandidateRepository("notexistingfile")
    snapshots = repo.find_all()
    assert len(snapshots) == 0

def test_release_repository_find_all(snapshots_file):
    repo = ReleaseCandidateRepository(str(snapshots_file))
    snapshots = repo.find_all()

    assert len(snapshots) == 1
    snapshot = snapshots[0]

    assert snapshot.metadata.id == "rc-20250310-001"
    assert snapshot.metadata.status == "pending"
    assert snapshot.metadata.generated_at == datetime.fromisoformat("2025-03-10T10:32:04.642635")

    assert len(snapshot.artifacts) == 7
    assert snapshot.artifacts[0].repository == "https://github.com/kubernetes-sigs/cluster-api"
    assert snapshot.artifacts[1].repository == "https://github.com/metal3-io/cluster-api-provider-metal3"
    assert snapshot.artifacts[2].repository == "https://github.com/openshift/assisted-service"
    assert snapshot.artifacts[3].repository == "https://github.com/openshift/assisted-image-service"
    assert snapshot.artifacts[4].repository == "https://github.com/openshift/assisted-installer-agent"
    assert snapshot.artifacts[5].repository == "https://github.com/openshift/assisted-installer"
    assert snapshot.artifacts[6].repository == "https://github.com/openshift/assisted-installer"


def test_release_repository_find_by_id(snapshots_file):
    repo = ReleaseCandidateRepository(str(snapshots_file))
    snapshot = repo.find_by_id("rc-20250310-001")
    assert snapshot is not None
    assert snapshot.metadata.id == "rc-20250310-001"
    assert repo.find_by_id("non-existent") is None


def test_release_repository_save_new(snapshots_file):
    repo = ReleaseCandidateRepository(str(snapshots_file))
    timestamp = datetime.now()
    new_snapshot = Snapshot(
        metadata=SnapshotMetadata(
            id="rc-20250311-001",
            generated_at=timestamp,
            status="pending"
        ),
        artifacts=[Artifact(repository="https://github.com/new/repo", ref="v1.0.0", name="new/repo", versioning_selection_mechanism="release")],
    )

    assert repo.save(new_snapshot)

    snapshots = repo.find_all()
    assert len(snapshots) == 2
    assert snapshots[0].metadata.id == "rc-20250311-001"
    assert snapshots[0].metadata.status == "pending"
    assert snapshots[0].metadata.generated_at == timestamp
    assert len(snapshots[0].artifacts) == 1
    assert snapshots[0].artifacts[0].repository == "https://github.com/new/repo"
    assert snapshots[0].artifacts[0].ref == "v1.0.0"


def test_release_repository_update_existing(snapshots_file):
    repo = ReleaseCandidateRepository(str(snapshots_file))
    original: Snapshot = repo.find_by_id("rc-20250310-001")

    # Create a new SnapshotMetadata with updated status
    updated_metadata = SnapshotMetadata(
        id=original.metadata.id,
        generated_at=original.metadata.generated_at,
        status="successful",
    )

    updated = Snapshot(metadata=updated_metadata, artifacts=original.artifacts)

    assert repo.update(updated)

    modified = repo.find_by_id("rc-20250310-001")
    assert modified is not None
    assert modified.metadata.status == "successful"

def test_invalid_snapshot_file_schema(tmp_path):
    bad_file = tmp_path / "bad.yaml"
    
    bad_file.write_text("snapshots:\n  - metadata: \n")

    repo = ReleaseCandidateRepository(str(bad_file))
    with pytest.raises(ValueError, match="Invalid release-candidates.yaml"):
        repo.find_all()
