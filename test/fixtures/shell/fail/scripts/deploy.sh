#!/usr/bin/env bash
# Copies a file to a target directory.
unused_target=/srv
copy() {
    cp $1 $2
}
copy "$@"
