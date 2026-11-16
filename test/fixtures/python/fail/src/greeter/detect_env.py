"""Production code that detects the test environment."""

import os


def is_test() -> bool:
    return os.environ.get("PYTEST_CURRENT_TEST") is not None
