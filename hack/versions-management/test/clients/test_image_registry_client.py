import pytest
import requests
from core.clients.image_registry_client import ImageRegistryClient

class DummyResponse:
    def __init__(self, status_code):
        self.status_code = status_code

@pytest.fixture
def client(): 
    return ImageRegistryClient()

@pytest.mark.parametrize("status,exists", [(200, True), (404, False)])
def test_exists(monkeypatch, client, status, exists):
    monkeypatch.setattr(requests, "head", lambda url, headers: DummyResponse(status))
    assert client.exists("quay.io/ns/repo", "tag") is exists

def test_exists_error(monkeypatch, client):
    monkeypatch.setattr(requests, "head", lambda url, headers: (_ for _ in ()).throw(Exception("fail")))
    assert client.exists("quay.io/ns/repo", "tag") is False
