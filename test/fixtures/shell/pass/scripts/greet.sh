#!/usr/bin/env bash
# Prints a greeting for each name given on the command line.
set -euo pipefail

greet() {
  local name="$1"
  if [[ -z "${name}" ]]; then
    echo "greet: empty name" >&2
    return 1
  fi
  printf 'Hello, %s.\n' "${name}"
}

main() {
  if [[ "$#" -eq 0 ]]; then
    echo "usage: greet.sh NAME..." >&2
    exit 2
  fi
  local name
  for name in "$@"; do
    greet "${name}"
  done
}

main "$@"
