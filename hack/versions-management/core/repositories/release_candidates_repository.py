from dataclasses import asdict
import os
from pathlib import Path

from ruamel.yaml import YAML
from core.models import Snapshot
from core.models.wrappers import SnapshotsFile
from core.utils.yaml_loader import get_yaml_instance


class ReleaseCandidateRepository:
    def __init__(self, file_path: str):
        self.file_path: str = file_path
        self.yaml: YAML = get_yaml_instance()

    def find_all(self) -> list[Snapshot]:
        if not os.path.isfile(path=Path(self.file_path)):
            return []
        with open(self.file_path, "r") as f:
            data = self.yaml.load(f)
            try:
                parsed = SnapshotsFile(**data)
                return parsed.snapshots
            except Exception as e:
                raise ValueError(f"Invalid release-candidates.yaml: {e}") from e

    def find_by_id(self, id: str) -> Snapshot | None:
        snapshots = self.find_all()
        return next((s for s in snapshots if s.metadata.id == id), None)

    def save(self, snapshot: Snapshot) -> bool:
        snapshots = self.find_all()

        # Check if exists
        index_of_snapshot = next(
            (i for i, s in enumerate(snapshots) if s.metadata.id == snapshot.metadata.id),
            None,
        )

        if index_of_snapshot is not None:
            snapshots[index_of_snapshot] = snapshot
        else:
            snapshots.insert(0, snapshot)

        return self._write_snapshots(snapshots)

    def update(self, snapshot: Snapshot) -> bool:
        return self.save(snapshot)

    def _write_snapshots(self, snapshots: list[Snapshot]) -> bool:
        try:
            with open(self.file_path, "w") as f:
                data = {"snapshots": [asdict(s) for s in snapshots]}
                self.yaml.dump(data, f)
            return True
        except Exception as e:
            raise Exception(f"Error writing snapshots: {e}") from e
