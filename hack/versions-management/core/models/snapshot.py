from pydantic.dataclasses import dataclass
from .snapshot_metadata import SnapshotMetadata
from .artifact import Artifact

@dataclass(frozen=True)
class Snapshot:
    metadata: SnapshotMetadata
    artifacts: list[Artifact]
