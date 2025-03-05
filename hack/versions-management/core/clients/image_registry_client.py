import requests
import logging

logger = logging.getLogger(__name__)


class ImageRegistryClient:
    def __init__(self, registry_url: str = "https://quay.io"):
        self.registry_url: str = registry_url

    def exists(self, image: str, tag: str) -> bool:
        path = image.replace("quay.io/", "")
        url = f"{self.registry_url}/v2/{path}/manifests/{tag}"
        headers = {"Accept": "application/vnd.docker.distribution.manifest.v2+json"}
        try:
            response = requests.head(url, headers=headers)
            return response.status_code == 200
        except Exception as e:
            logger.error(f"Error checking image {image} existence: {e}")
            return False
