"""A module-level collection that grows."""

seen: dict[str, int] = {}


def remember(key: str) -> int:
    seen[key] = seen.get(key, 0) + 1
    return seen[key]
