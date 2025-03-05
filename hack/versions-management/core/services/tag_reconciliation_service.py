import logging
import re
from typing import override
from core.clients.github_client import GitHubClient
from core.repositories import VersionRepository
from core.services.service import Service
from core.utils.logging import setup_logger

class TagReconciliationService(Service):
    def __init__(self, versions_file_path: str, dry_run: bool):
        self.github: GitHubClient = GitHubClient()
        self.versions_repo: VersionRepository = VersionRepository(versions_file_path)
        self.logger: logging.Logger = setup_logger("TagReconcilerService")
        self.dry_run: bool = dry_run

    @override
    def run(self) -> None:
        versions = self.versions_repo.find_all()
        for version in versions:
            if not version.name:
                self.logger.warning("Skipping version without name")
                continue
            for artifact in version.artifacts:
                repo = artifact.name
                if not re.match(r"^openshift/", repo):
                    continue
                if not self.tag_exists(repo, version.name):
                    if not self.dry_run:
                        self.create_tag(repo, artifact.ref, version.name)
                    else:
                        self.logger.info(f"Dry run mode. tag {version.name} on {artifact.ref} in repo {repo} has not been created")

    def tag_exists(self, repo: str, tag: str) -> bool:
        try:
            self.github.get_repo(repo).get_git_ref(f"tags/{tag}")
            return True
        except Exception:
            return False

    def create_tag(self, repo: str, ref: str, tag: str) -> None:
        try:
            gh_repo = self.github.get_repo(repo)
            tag_obj = gh_repo.create_git_tag(tag=tag, message="Tagged by CI", object=ref, type="commit")
            gh_repo.create_git_ref(f"refs/tags/{tag}", tag_obj.sha)
            self.logger.info(f"Created tag {tag} on {repo}")
        except Exception as e:
            raise Exception(f"Failed to create tag {tag} on {repo}: {e}") from e
