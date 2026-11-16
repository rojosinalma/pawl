#!/bin/sh
# pawl: the precommit ring. `pawl run precommit` runs `make precommit` quietly and records its wall time for
# `pawl budget`. Project-owned: edit freely. PAWL overrides the pawl binary; without pawl the hook runs make itself.
#
# A machine without the tools runs the ring in a container: set the image once per clone with
#   git config pawl.toolbox ghcr.io/rojosinalma/pawl-toolbox:latest
set -eu

pawl="${PAWL:-pawl}"
if command -v "${pawl}" >/dev/null 2>&1; then
  exec "${pawl}" run precommit
fi
echo "pre-commit: warning: pawl is not installed; running make precommit" >&2
exec make precommit
