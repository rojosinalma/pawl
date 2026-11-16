"""Swallowed errors."""


def ignore_failure(fn: object) -> None:
    try:
        print(fn)
    except Exception:
        pass


def bare(fn: object) -> None:
    try:
        str(fn)
    except:
        pass
