"""An outbound call without a timeout."""

from urllib.request import urlopen


def download(url: str) -> bytes:
    with urlopen(url) as response:
        body: bytes = response.read()
    return body
