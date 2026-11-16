// Package install implements `pawl install`: mise itself when the machine lacks it, the tools pinned in mise.toml,
// then the steps each stack does with its own package manager.
package install

import (
	"context"
	"errors"
	"io"

	"github.com/rojosinalma/pawl/internal/project"
)

// Options of one install.
type Options struct {
	// SkipMise leaves a machine without mise alone: only the stack's own steps run.
	SkipMise bool
	Stdout   io.Writer
	Stderr   io.Writer
}

// Step is one thing install did or tried.
type Step struct {
	Name    string   // what it is for ("mise", "tools of mise.toml", "go tools", "gems")
	Command []string // the command that ran; empty for the mise download
	Skipped string   // why it did not run; "" when it ran
	Err     error    // the failure; nil when it passed or was skipped
}

// Result lists the steps in the order they ran.
type Result struct {
	Steps []Step
}

// Failed reports whether a step failed.
func (r *Result) Failed() bool {
	for _, s := range r.Steps {
		if s.Err != nil {
			return true
		}
	}
	return false
}

// Run installs what the project's rings need. The error is for failures to start at all; a failing step is in the
// result.
func Run(ctx context.Context, p *project.Project, opts Options) (*Result, error) {
	return nil, errors.New("install.Run: not implemented")
}
