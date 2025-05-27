import os
import shutil
from unittest.mock import MagicMock, patch, call

import pytest
from core.models import Artifact, Version
from core.services.tag_reconciliation_service import TagReconciliationService

CAPOA_REPO = "openshift-assisted/cluster-api-provider-openshift-assisted"
ASSETS_DIR = os.path.join(os.path.dirname(os.path.dirname(__file__)), "assets")


@pytest.fixture
def versions_file(tmp_path):
    source_file = os.path.join(ASSETS_DIR, "versions.yaml")
    dest_file = tmp_path / "versions.yaml"
    shutil.copy(source_file, dest_file)
    return dest_file


@pytest.fixture
def mock_github():
    with patch("core.services.tag_reconciliation_service.GitHubClient") as mock:
        github_client = MagicMock()
        mock.return_value = github_client
        yield github_client


@pytest.fixture
def mock_version_repo():
    with patch("core.services.tag_reconciliation_service.VersionRepository") as mock:
        version_repo = MagicMock()
        mock.return_value = version_repo
        yield version_repo


@pytest.fixture
def service(versions_file, mock_version_repo, mock_github):
    with (
        patch("core.services.tag_reconciliation_service.GitHubClient"),
        patch("core.services.tag_reconciliation_service.VersionRepository"),
    ):
        svc = TagReconciliationService(versions_file, dry_run=False)
        svc.logger = MagicMock()
        svc.github = mock_github
        svc.versions_repo = mock_version_repo
        svc.github.get_repo.return_value = MagicMock()
        svc.versions_repo.find_all.return_value = [
            Version(
                name="v1.0.0",
                tested_with_ref="ref",
                artifacts=[
                    Artifact(
                        repository="https://github.com/openshift/repo",
                        ref="abc123",
                        name="openshift/repo",
                        versioning_selection_mechanism="commit",
                    )
                ],
            )
        ]
        return svc


@pytest.fixture
def versions():
    return [
        Version(
            name="v1.0.0",
            tested_with_ref="ref",
            artifacts=[
                Artifact(
                    repository="https://github.com/openshift/repo1",
                    ref="abc123",
                    name="openshift/repo1",
                    versioning_selection_mechanism="commit",
                ),
                Artifact(
                    repository="https://github.com/openshift/repo2",
                    ref="def456",
                    name="openshift/repo2",
                    versioning_selection_mechanism="commit",
                ),
                Artifact(
                    repository="https://github.com/kubernetes-sigs/repo3",
                    ref="ghi789",
                    name="kubernetes-sigs/repo3",
                    versioning_selection_mechanism="commit",
                ),
            ],
        ),
        Version(
            name="v1.1.0",
            tested_with_ref="ref",
            artifacts=[
                Artifact(
                    repository="https://github.com/openshift/repo1",
                    ref="jkl012",
                    name="openshift/repo1",
                    versioning_selection_mechanism="commit",
                ),
                Artifact(
                    repository="https://github.com/openshift/repo2",
                    ref="mno345",
                    name="openshift/repo2",
                    versioning_selection_mechanism="commit",
                ),
            ],
        ),
        Version(
            name="",  # Empty version name to test skipping
            artifacts=[
                Artifact(
                    repository="https://github.com/openshift/repo4",
                    ref="pqr678",
                    name="openshift/repo",
                    versioning_selection_mechanism="commit",
                ),
            ],
        ),
    ]


def test_reconciler_creates_tag(service):
    service.tag_exists = MagicMock(return_value=False)
    service.create_tag = MagicMock()
    service.run()
    # Should be called for both the artifact and CAPOA_REPO for each version
    assert service.create_tag.call_count == 2
    calls = [call_args.args for call_args in service.create_tag.call_args_list]
    expected = [
        ("openshift/repo", "abc123", "capoa-v1.0.0"),
        (CAPOA_REPO, "ref", "v1.0.0"),
    ]
    for exp in expected:
        assert exp in calls


def test_reconciler_skips_existing_tag(service):
    service.tag_exists = MagicMock(return_value=True)
    service.create_tag = MagicMock()
    service.run()
    service.create_tag.assert_not_called()


def test_run_happy_path_complete(service, mock_github, mock_version_repo, versions):
    mock_version_repo.find_all.return_value = versions
    service.tag_exists = MagicMock(return_value=False)
    service.create_tag = MagicMock()

    mock_repo1 = MagicMock()
    mock_repo2 = MagicMock()
    mock_capoa = MagicMock()

    def get_repo_side_effect(name):
        if name == "openshift/repo1":
            return mock_repo1
        elif name == "openshift/repo2":
            return mock_repo2
        elif name == CAPOA_REPO:
            return mock_capoa
        else:
            return MagicMock()

    mock_github.get_repo.side_effect = get_repo_side_effect

    service.run()

    # Check that tag_exists was called for all openshift artifacts and for the CAPOA_REPO for each version
    expected_tag_exists_calls = [
        call("openshift/repo1", "capoa-v1.0.0"),
        call("openshift/repo2", "capoa-v1.0.0"),
        call(CAPOA_REPO, "v1.0.0"),
        call("openshift/repo1", "capoa-v1.1.0"),
        call("openshift/repo2", "capoa-v1.1.0"),
        call(CAPOA_REPO, "v1.1.0"),
    ]
    for expected in expected_tag_exists_calls:
        assert expected in service.tag_exists.call_args_list

    expected_create_tag_calls = [
        ("openshift/repo1", "abc123", "capoa-v1.0.0"),
        ("openshift/repo2", "def456", "capoa-v1.0.0"),
        (CAPOA_REPO, "ref", "v1.0.0"),
        ("openshift/repo1", "jkl012", "capoa-v1.1.0"),
        ("openshift/repo2", "mno345", "capoa-v1.1.0"),
        (CAPOA_REPO, "ref", "v1.1.0"),
    ]

    create_tag_call_args = [c.args for c in service.create_tag.call_args_list]
    for ec in expected_create_tag_calls:
        assert ec in create_tag_call_args

    # Non-openshift repos were skipped
    for call_args in service.tag_exists.call_args_list:
        repo = call_args.args[0]
        assert repo.startswith("openshift/") or repo == CAPOA_REPO, (
            f"Non-openshift repo {repo} was checked"
        )

    # Empty version name warning
    assert service.logger.warning.called
    found = any(
        "Skipping version without name" in str(args)
        for args, _ in service.logger.warning.call_args_list
    )
    assert found


def test_run_with_existing_tags(service, mock_version_repo, versions):
    mock_version_repo.find_all.return_value = versions[:1]

    def tag_exists_side_effect(repo, tag):
        # repo1 tag exists and CAPOA_REPO exists; repo2 does not
        return repo in ("openshift/repo1", CAPOA_REPO)

    service.tag_exists = MagicMock(side_effect=tag_exists_side_effect)
    service.create_tag = MagicMock()
    service.run()
    # Only repo2 should get a tag; also CAPOA_REPO should be skipped as "exists"
    expected_calls = [
        call("openshift/repo2", "def456", "capoa-v1.0.0"),
    ]
    actual_calls = [c for c in service.create_tag.call_args_list]
    # But, due to CAPOA_REPO, make sure it's not called for it if tag exists
    assert actual_calls == expected_calls


def test_run_with_tag_creation_failure(service, mock_version_repo, versions):
    mock_version_repo.find_all.return_value = versions[:1]
    service.tag_exists = MagicMock(return_value=False)

    def create_tag_side_effect(repo, ref, tag):
        if repo == "openshift/repo2":
            raise Exception(f"Failed to create tag {tag} on {repo}")

    service.create_tag = MagicMock(side_effect=create_tag_side_effect)
    with pytest.raises(
        Exception, match="Failed to create tag capoa-v1.0.0 on openshift/repo2"
    ):
        service.run()
    service.create_tag.assert_any_call("openshift/repo1", "abc123", "capoa-v1.0.0")


def test_create_tag_implementation(service, mock_github):
    service.create_tag = TagReconciliationService.create_tag.__get__(
        service, TagReconciliationService
    )
    mock_repo = MagicMock()
    mock_tag_obj = MagicMock()
    mock_tag_obj.sha = "tag_sha_123"
    mock_repo.create_git_tag.return_value = mock_tag_obj
    mock_github.get_repo.return_value = mock_repo
    service.create_tag("openshift/repo1", "commit_sha_123", "capoa-v1.0.0")
    mock_repo.create_git_tag.assert_called_once_with(
        tag="capoa-v1.0.0",
        message="Tagged by CI",
        object="commit_sha_123",
        type="commit",
    )
    mock_repo.create_git_ref.assert_called_once_with(
        "refs/tags/capoa-v1.0.0", "tag_sha_123"
    )


def test_tag_exists_implementation(service, mock_github):
    service.tag_exists = TagReconciliationService.tag_exists.__get__(
        service, TagReconciliationService
    )
    mock_repo = MagicMock()
    mock_github.get_repo.return_value = mock_repo
    mock_repo.get_git_ref.return_value = MagicMock()
    assert service.tag_exists("openshift/repo1", "capoa-v1.0.0") is True
    mock_repo.get_git_ref.assert_called_with("tags/capoa-v1.0.0")
    mock_repo.get_git_ref.side_effect = Exception("Not found")
    assert service.tag_exists("openshift/repo1", "capoa-v1.0.0") is False


def test_run_no_versions(service, mock_version_repo):
    mock_version_repo.find_all.return_value = []
    service.tag_exists = MagicMock()
    service.create_tag = MagicMock()
    service.run()
    service.tag_exists.assert_not_called()
    service.create_tag.assert_not_called()


def test_tag_already_exists_detailed(service, mock_github, mock_version_repo, versions):
    mock_version_repo.find_all.return_value = versions[:1]
    repo1 = MagicMock()
    repo2 = MagicMock()
    capoa_repo = MagicMock()

    def get_repo_side_effect(name):
        if name == "openshift/repo1":
            return repo1
        elif name == "openshift/repo2":
            return repo2
        elif name == CAPOA_REPO:
            return capoa_repo
        else:
            return MagicMock()

    mock_github.get_repo.side_effect = get_repo_side_effect
    repo1.get_git_ref.return_value = MagicMock()
    repo2.get_git_ref.side_effect = Exception("Tag not found")
    capoa_repo.get_git_ref.return_value = MagicMock()
    original_create_tag = service.create_tag
    create_tag_calls = []

    def spy_create_tag(repo, ref, tag):
        create_tag_calls.append((repo, ref, tag))
        return original_create_tag(repo, ref, tag)

    service.create_tag = MagicMock(side_effect=spy_create_tag)
    service.run()
    # repo1 and CAPOA_REPO tag exist, so only repo2 gets created
    assert len(create_tag_calls) == 1
    assert create_tag_calls[0][0] == "openshift/repo2"
    assert create_tag_calls[0][2] == "capoa-v1.0.0"
    repo1.get_git_ref.assert_called_with("tags/capoa-v1.0.0")
    repo2.get_git_ref.assert_called_with("tags/capoa-v1.0.0")
    capoa_repo.get_git_ref.assert_called_with("tags/v1.0.0")
    log_messages = [args[0] for args, _ in service.logger.info.call_args_list]
    repo2_message_found = any(
        "Created tag capoa-v1.0.0 on openshift/repo2" in msg for msg in log_messages
    )
    assert repo2_message_found


def test_tag_creation_failure_detailed(
    service, mock_github, mock_version_repo, versions
):
    mock_version_repo.find_all.return_value = versions[:1]
    mock_repo = MagicMock()
    mock_repo.get_git_ref.side_effect = Exception("Tag not found")
    mock_github.get_repo.return_value = mock_repo

    def create_git_tag_side_effect(tag, message, object, type):
        if tag == "capoa-v1.0.0" and "repo2" in str(
            mock_github.get_repo.call_args[0][0]
        ):
            raise Exception("GitHub API error: Reference already exists")
        return MagicMock(sha="fake_sha")

    mock_repo.create_git_tag.side_effect = create_git_tag_side_effect
    with pytest.raises(Exception) as excinfo:
        service.run()
    error_message = str(excinfo.value)
    assert (
        "Failed to create tag capoa-v1.0.0 on openshift/repo2" in error_message
        or "Failed to create tag capoa-v1.0.0 on https://github.com/openshift/repo2"
        in error_message
    )
    assert "GitHub API error: Reference already exists" in error_message


def test_dry_run_mode(service, mock_version_repo, versions):
    service.dry_run = True
    mock_version_repo.find_all.return_value = versions[:1]
    service.tag_exists = MagicMock(return_value=False)
    service.create_tag = MagicMock()
    service.run()
    service.create_tag.assert_not_called()
    dry_run_messages = [
        args[0]
        for args, kwargs in service.logger.info.call_args_list
        if args and "Dry run mode" in args[0]
    ]
    assert len(dry_run_messages) > 0
    for artifact in versions[0].artifacts:
        if artifact.repository.startswith("https://github.com/openshift/"):
            repo = artifact.repository.replace("https://github.com/", "")
            expected_msg = f"Dry run mode. tag capoa-v1.0.0 on {artifact.ref} in repo {repo} has not been created"
            assert any(expected_msg in msg for msg in dry_run_messages), (
                f"Expected log message not found for {repo}"
            )
    # CAPOA_REPO dry run log
    expected_msg_full = f"Dry run mode. tag v1.0.0 on ref in repo {CAPOA_REPO} has not been created"
    assert any(expected_msg_full in msg for msg in dry_run_messages)
