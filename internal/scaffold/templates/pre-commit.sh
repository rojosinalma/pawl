#!/bin/sh
# pawl: the precommit ring. `pawl run precommit` runs `make precommit` quietly and records its wall time for
# `pawl budget`. Project-owned: edit freely. PAWL overrides the pawl binary.
#
# A machine without the tools runs the ring in a container: set the image once per clone with
#   git config pawl.toolbox ghcr.io/rojosinalma/pawl-toolbox:latest
set -eu

exec "${PAWL:-pawl}" run precommit
