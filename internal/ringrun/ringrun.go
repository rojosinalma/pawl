// Package ringrun runs a ring (precommit, check, nightly) through the project's make target and reports it the way
// an agent or a person needs it: one line when it passes, only the failing steps when it does not.
package ringrun

import (
	"context"
	"errors"
	"io"

	"github.com/rojosinalma/pawl/internal/project"
)

// Options of one run.
type Options struct {
	// Verbose streams make's own output instead of the summary.
	Verbose bool
	// NoToolbox runs make on this machine even when the clone names a toolbox image (git config pawl.toolbox).
	NoToolbox bool
	// Environ is the environment make runs with (the caller adds the directories of the pinned tools to PATH).
	Environ []string
	Stdout  io.Writer
	Stderr  io.Writer
}

// Run executes `make <ring>` for precommit, check or nightly and returns whether the ring passed. The error is for
// failures to run at all (no Makefile, unknown ring, make or docker missing), never for a failing ring.
func Run(ctx context.Context, p *project.Project, ring string, opts Options) (passed bool, err error) {
	return false, errors.New("ringrun.Run: not implemented")
}
