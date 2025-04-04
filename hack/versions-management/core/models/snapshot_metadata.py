from dataclasses import dataclass
from datetime import datetime

@dataclass(frozen=True)
class SnapshotMetadata:
    id: str
    generated_at: datetime
    status: str #can be either failed, successful or pending
