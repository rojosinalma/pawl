// Package doctor verifies the machine: every tool the project's rings need is installed at the pinned version, the
// hook is active, make and git exist.
package doctor

import (
	"errors"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/report"
)

// Run checks the tools of every active adapter (missing tool for the precommit or check ring: error; nightly-only:
// warning; version differs from the pin: error), make, git, the hook path, and gh (warning when absent).
func Run(p *project.Project) (*report.Report, error) {
	return nil, errors.New("doctor.Run: not implemented")
}
