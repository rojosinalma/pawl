// Package toolenv finds mise and builds the environment in which the project's pinned tools are on PATH, so the
// rings and `pawl doctor` work without the user activating mise in a shell.
package toolenv

import (
	"context"
	"errors"
)

// MisePath returns the mise binary to use: the one on PATH, else the one pawl installed (~/.local/bin/mise), else "".
func MisePath() string { return "" }

// Environ returns the process environment with the directories of the project's mise tools (`mise bin-paths`, run
// in root) in front of PATH. Without mise or without a mise.toml or .tool-versions in root it returns the process
// environment unchanged.
func Environ(ctx context.Context, root string) ([]string, error) {
	return nil, errors.New("toolenv.Environ: not implemented")
}
