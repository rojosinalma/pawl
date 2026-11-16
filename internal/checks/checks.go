// Package checks is the catalog verification behind `pawl check`: the manifest against the catalog, the make
// targets, the adapters' configuration verifiers, pawl's own text checks, and the ratchet against the base commit.
package checks

import (
	"errors"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/ratchet"
	"github.com/rojosinalma/pawl/internal/report"
)

// Options select what a run covers.
type Options struct {
	// Ring is "" or "check" for the full verification, "precommit" for the fast checks on staged files, "nightly"
	// for the full verification plus the nightly-only rules.
	Ring string
	// Only limits the run to these rule ids (used by make recipes such as `pawl check --only escape.duplication`).
	Only []string
}

// Run verifies the project and returns the findings. The error is for failures to run at all (unreadable files,
// git errors), never for findings.
func Run(p *project.Project, opts Options) (*report.Report, error) {
	return nil, errors.New("checks.Run: not implemented")
}

// Measure computes the current ratchet metrics (suppressions, oversized files, duplicated blocks, TODOs without a
// link, quarantined tests) without comparing them; `pawl baseline` records the result.
func Measure(p *project.Project) (*ratchet.Current, error) {
	return nil, errors.New("checks.Measure: not implemented")
}
