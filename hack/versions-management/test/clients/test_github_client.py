import pytest
from core.clients.github_client import GitHubClient

class DummyIntegration:
    def __init__(self, app_id, private_key):
        pass
    def get_access_token(self, installation_id):
        class Token:
            token = "fake-token"
        return Token()

@pytest.fixture(autouse=True)
def patch_integration(monkeypatch):
    monkeypatch.setenv("GITHUB_APP_ID", "123")
    monkeypatch.setenv("GITHUB_APP_INSTALLATION_ID", "456")
    monkeypatch.setenv("GITHUB_APP_PRIVATE_KEY", "---KEY---")
    monkeypatch.setattr("core.clients.github_client.GithubIntegration", DummyIntegration)
    yield

def test_github_client_get_repo(monkeypatch):
    client = GitHubClient()
    dummy = object()
    monkeypatch.setattr(client.client, "get_repo", lambda full_name: dummy)
    assert client.get_repo("org/repo") is dummy


def test_missing_credentials(monkeypatch):
    monkeypatch.delenv("GITHUB_APP_ID", raising=False)
    with pytest.raises(EnvironmentError):
        GitHubClient()
