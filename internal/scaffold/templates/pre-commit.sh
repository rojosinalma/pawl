#!/bin/sh
# pawl: the precommit ring. Runs `make precommit` and records its wall time for `pawl budget`.
# Project-owned: edit freely. PAWL overrides the pawl binary.
#
# A machine without the tools runs the ring in a container: set the image once per clone with
#   git config pawl.toolbox ghcr.io/rojosinalma/pawl-toolbox:latest
set -eu

toolbox="$(git config --get pawl.toolbox || true)"
if [ -n "${toolbox}" ]; then
  # one container for the ring and its timing; the working tree is mounted with the caller's user
  exec docker run --rm -u "$(id -u):$(id -g)" -v "${PWD}":/work "${toolbox}" sh -c '
    start="$(date +%s)"
    status=0
    make precommit || status=$?
    "${PAWL:-pawl}" record precommit "$(($(date +%s) - start))" >/dev/null 2>&1 || true
    exit "${status}"'
fi

start="$(date +%s)"
status=0
make precommit || status=$?
end="$(date +%s)"

"${PAWL:-pawl}" record precommit "$((end - start))" >/dev/null 2>&1 || true

exit "${status}"
