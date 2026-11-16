"""Fetches a greeting from a remote service."""

from urllib.request import urlopen


def fetch_greeting(url: str, timeout: float) -> str:
    """Return the body of the greeting service's answer."""
    with urlopen(url, timeout=timeout) as response:  # noqa: S310 -- the caller passes a configured https URL
        body: bytes = response.read()
    return body.decode()
