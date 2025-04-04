from pydantic.dataclasses import dataclass
from .artifact import Artifact

@dataclass(frozen=True)
class Version:
    name: str
    artifacts: list[Artifact]
