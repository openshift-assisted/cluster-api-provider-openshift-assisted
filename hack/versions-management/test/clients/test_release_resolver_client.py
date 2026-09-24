import json

import pytest
import requests

from core.clients.release_resolver_client import ReleaseResolverClient


class DummyResponse:
    def __init__(self, status_code, text=""):
        self.status_code = status_code
        self.text = text


@pytest.fixture
def client():
    return ReleaseResolverClient()


def _responder(mapping):
    """Return a fake requests.get serving bodies by url-substring (longest match wins)."""
    def _get(url, timeout):
        best = None
        for needle, response in mapping.items():
            if needle in url and (best is None or len(needle) > len(best[0])):
                best = (needle, response)
        return best[1] if best else DummyResponse(404)
    return _get


# --- resolve_ocp_version -----------------------------------------------------

def test_resolve_ocp_version_stable(monkeypatch, client):
    body = json.dumps({"name": "4.20.39"})
    monkeypatch.setattr(requests, "get",
                        _responder({"4-stable": DummyResponse(200, body)}))
    assert client.resolve_ocp_version("4.20") == "4.20.39"


def test_resolve_ocp_version_falls_back_to_dev_preview(monkeypatch, client):
    body = json.dumps({"name": "5.1.0-ec.0"})
    # 5-stable has no match for the 5.1 range; 5-dev-preview does.
    monkeypatch.setattr(requests, "get", _responder({
        "5-stable": DummyResponse(404),
        "5-dev-preview": DummyResponse(200, body),
    }))
    assert client.resolve_ocp_version("5.1") == "5.1.0-ec.0"


def test_resolve_ocp_version_non_json_is_not_fatal(monkeypatch, client):
    # A non-JSON body (the original bug) must not crash; it degrades to fallback.
    body = json.dumps({"name": "5.0.0-rc.3"})
    monkeypatch.setattr(requests, "get", _responder({
        "5-stable": DummyResponse(200, "<html>not json</html>"),
        "5-dev-preview": DummyResponse(200, body),
    }))
    assert client.resolve_ocp_version("5.0") == "5.0.0-rc.3"


def test_resolve_ocp_version_none_when_no_match(monkeypatch, client):
    monkeypatch.setattr(requests, "get", lambda url, timeout: DummyResponse(404))
    assert client.resolve_ocp_version("5.1") is None


# --- resolve_rhcos_url -------------------------------------------------------

_LISTING = (
    '<a href="4.20.36/">4.20.36/</a>'
    '<a href="4.20.35/">4.20.35/</a>'
)
_FILES = '<a href="rhcos-4.20.36-x86_64-nutanix.x86_64.qcow2">img</a>'


def test_resolve_rhcos_url_stable(monkeypatch, client):
    monkeypatch.setattr(requests, "get", _responder({
        "/rhcos/4.20/4.20.36/": DummyResponse(200, _FILES),
        "/rhcos/4.20/": DummyResponse(200, _LISTING),
    }))
    url = client.resolve_rhcos_url("4.20")
    assert url.endswith("4.20.36/rhcos-4.20.36-x86_64-nutanix.x86_64.qcow2")


def test_resolve_rhcos_url_prefers_highest_version(monkeypatch, client):
    listing = (
        '<a href="5.0.0-rc.2/">a</a>'
        '<a href="5.0.0-rc.10/">b</a>'
    )
    files = '<a href="rhcos-5.0.0-rc.10-x86_64-nutanix.x86_64.qcow2">img</a>'
    monkeypatch.setattr(requests, "get", _responder({
        "/rhcos/5.0/": DummyResponse(404),
        "/pre-release/5.0.0-rc.10/": DummyResponse(200, files),
        "/pre-release/": DummyResponse(200, listing),
    }))
    url = client.resolve_rhcos_url("5.0")
    assert "5.0.0-rc.10" in url


def test_resolve_rhcos_url_none_when_missing(monkeypatch, client):
    monkeypatch.setattr(requests, "get", lambda url, timeout: DummyResponse(404))
    assert client.resolve_rhcos_url("5.1") is None
