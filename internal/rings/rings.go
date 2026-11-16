// Package rings records how long each ring takes and compares the medians with the budgets in the manifest.
package rings

import (
	"errors"
	"time"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/report"
)

// Record stores one run of a ring (the git hook calls `pawl record precommit <seconds>`). Local timings live in
// .pawl/reports/timings.json, which is ignored by git; at most the last 50 per ring are kept.
func Record(root, ring string, d time.Duration) error {
	return errors.New("rings.Record: not implemented")
}

// Budget compares the median of the last ten runs of every ring with its target (warning) and limit (error).
// The precommit ring reads the local timings; the check and nightly rings read the last ten completed runs on the
// default branch of the workflows named check and nightly through `gh run list`, when gh and a remote exist.
func Budget(p *project.Project) (*report.Report, error) {
	return nil, errors.New("rings.Budget: not implemented")
}
