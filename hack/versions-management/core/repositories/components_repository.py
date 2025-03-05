import os
from ruamel.yaml import YAML
from core.models.component import Component
from core.models.wrappers import ComponentsFile
from core.utils.yaml_loader import get_yaml_instance


class ComponentRepository:
    def __init__(self, file_path: str):
        self.file_path: str = file_path
        self.yaml: YAML = get_yaml_instance()

    def find_all(self) -> list[Component]:
        if not os.path.isfile(self.file_path):
            return []
        with open(self.file_path, "r") as f:
            data = self.yaml.load(f)
            try:
                parsed = ComponentsFile(**data)
                return parsed.components
            except Exception as e:
                raise ValueError(f"Invalid versions.yaml structure: {e}") from e
