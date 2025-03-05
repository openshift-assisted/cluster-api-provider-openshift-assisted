from pydantic.dataclasses import dataclass

@dataclass(frozen=True)
class Artifact:
    repository: str
    ref: str
    name: str
    versioning_selection_mechanism: str
    image_url: str | None = None

