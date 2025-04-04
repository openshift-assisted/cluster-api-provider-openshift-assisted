import os
from ruamel.yaml import YAML
from core.models import VersionsFile, Version
from core.utils.yaml_loader import get_yaml_instance


class VersionRepository:
    def __init__(self, file_path: str):
        self.file_path: str = file_path
        self.yaml: YAML = get_yaml_instance()

    def find_all(self) -> list[Version]:
        if not os.path.isfile(self.file_path):
            return []
        with open(self.file_path, "r") as f:
            data = self.yaml.load(f)
            try:
                parsed = VersionsFile(**data)
                return parsed.versions
            except Exception as e:
                raise ValueError(f"Invalid versions.yaml structure: {e}") from e
