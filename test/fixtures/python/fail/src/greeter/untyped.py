"""Untyped definition, a lint error, a naming error, dead code and a dropped result."""

import os


def shout(text):
    return text.upper()


def getValue(value: int) -> int:
    value == 1
    return value
    print("unreachable")
