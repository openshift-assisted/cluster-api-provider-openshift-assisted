from unittest.mock import MagicMock
import pytest
import subprocess
from core.clients.ansible_client import AnsibleClient

@pytest.fixture
def client(): 
    return AnsibleClient()

def test_run_playbook_success(monkeypatch, client):
    class DummyResult: 
        returncode = 0
    monkeypatch.setattr(subprocess, "run", lambda cmd, check=False, env=MagicMock(): DummyResult())
    client.run_playbook("pb.yml", "inv.yml")


def test_run_playbook_failure(monkeypatch, client):
    class DummyResult: 
        returncode = 1
    monkeypatch.setattr(subprocess, "run", lambda cmd, check=False, env=MagicMock(): DummyResult())
    with pytest.raises(RuntimeError):
        client.run_playbook("pb.yml", "inv.yml")
