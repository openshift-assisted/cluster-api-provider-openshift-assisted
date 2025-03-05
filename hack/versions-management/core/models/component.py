from pydantic.dataclasses import dataclass

@dataclass(frozen=True)
class Component:
    repository: str
    name: str
    versioning_selection_mechanism: str
    image_pattern: str | None = None
