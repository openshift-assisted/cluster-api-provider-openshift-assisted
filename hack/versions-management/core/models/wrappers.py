from pydantic.dataclasses import dataclass

from core.models.component import Component
from core.models.version import Version
from core.models.snapshot import Snapshot

@dataclass(frozen=True)
class VersionsFile:
    versions: list[Version]

@dataclass(frozen=True)
class SnapshotsFile:
    snapshots: list[Snapshot]

@dataclass(frozen=True)
class ComponentsFile:
    components: list[Component]

