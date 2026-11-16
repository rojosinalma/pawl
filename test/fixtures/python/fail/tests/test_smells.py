from unittest.mock import patch

import pytest

from greeter.dynamic import run


@pytest.mark.skip()
def test_skipped() -> None:
    assert run("1") == 1


def test_patches_its_own_module() -> None:
    with patch("greeter.dynamic.run", return_value=2):
        assert run("1") == 1
