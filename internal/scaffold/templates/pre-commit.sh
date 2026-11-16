#!/bin/sh
# pawl: the precommit ring. Runs `make precommit` and records its wall time for `pawl budget`.
# Project-owned: edit freely. PAWL overrides the pawl binary.
set -eu

start="$(date +%s)"
status=0
make precommit || status=$?
end="$(date +%s)"

"${PAWL:-pawl}" record precommit "$((end - start))" >/dev/null 2>&1 || true

exit "${status}"
