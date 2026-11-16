"""A match over a closed set that misses a case."""

from enum import Enum


class Tone(Enum):
    """How warm a greeting is."""

    PLAIN = "plain"
    WARM = "warm"


def word(tone: Tone) -> str:
    match tone:
        case Tone.PLAIN:
            return "hello"
