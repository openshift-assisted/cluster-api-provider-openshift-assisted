import os
import logging
from github import Github, GithubIntegration, Repository
logger = logging.getLogger(__name__)


class GitHubClient:
    def __init__(self):
        app_id = os.getenv("GITHUB_APP_ID")
        install_id = os.getenv("GITHUB_APP_INSTALLATION_ID")
        private_key = os.getenv("GITHUB_APP_PRIVATE_KEY")
        if not (app_id and install_id and private_key):
            logger.error("GitHub credentials env vars are mandatory")
            raise EnvironmentError("Missing GitHub App credentials")
        integration = GithubIntegration(int(app_id), private_key)
        token = integration.get_access_token(int(install_id)).token
        self.client: Github = Github(token)

    def get_repo(self, full_name: str) -> Repository.Repository:
        return self.client.get_repo(full_name)
