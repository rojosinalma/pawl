import pytest

from greeter import Tone, greet


def test_greets_plainly_by_default() -> None:
    assert greet(" Ada ") == "Hello, Ada."


def test_greets_warmly() -> None:
    assert greet("Ada", Tone.WARM) == "Hello, Ada! Good to see you."


def test_rejects_an_empty_name() -> None:
    with pytest.raises(ValueError, match="must not be empty"):
        greet("  ")
