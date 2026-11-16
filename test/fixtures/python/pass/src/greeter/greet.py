"""Greetings in two tones."""

from enum import Enum


class Tone(Enum):
    """How warm a greeting is."""

    PLAIN = "plain"
    WARM = "warm"


def greet(name: str, tone: Tone = Tone.PLAIN) -> str:
    """Return a greeting for the name."""
    trimmed = name.strip()
    if not trimmed:
        msg = "name must not be empty"
        raise ValueError(msg)
    match tone:
        case Tone.PLAIN:
            return f"Hello, {trimmed}."
        case Tone.WARM:
            return f"Hello, {trimmed}! Good to see you."
