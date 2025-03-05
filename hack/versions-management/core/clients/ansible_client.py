import os
import subprocess
import logging

logger = logging.getLogger(__name__)


class AnsibleClient:
    def run_playbook(self, playbook: str, inventory: str) -> None:
        cmd = ["ansible-playbook", playbook, "-i", inventory]
        result = subprocess.run(cmd, check=False, env=os.environ)
        if result.returncode != 0:
            logger.error(f"Ansible playbook failed with code {result.returncode}")
            raise RuntimeError("Ansible tests failed")
