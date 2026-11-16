"""Reads the environment outside the configuration module."""

import os


def api_base() -> str:
    return os.environ["ACME_API_BASE"]
