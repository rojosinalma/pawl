package rings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/runner"
)

// ciSource reads ring durations from GitHub Actions through gh; it resolves the remote and default branch once.
type ciSource struct {
	p        *project.Project
	run      runner.Runner
	resolved bool
	branch   string
	why      string // why the CI rings cannot be read; "" when they can
}

type ghRun struct {
	DatabaseID int64     `json:"databaseId"`
	Conclusion string    `json:"conclusion"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type ghStep struct {
	Name        string    `json:"name"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

type ghJobs struct {
	Jobs []struct {
		Steps []ghStep `json:"steps"`
	} `json:"jobs"`
}

func (c *ciSource) resolve() {
	if c.resolved {
		return
	}
	c.resolved = true
	if c.p.Repo == nil {
		c.why = "not a git repository"
		return
	}
	if out, err := c.run.Output(c.p.Root, "git", "remote", "get-url", "origin"); err != nil || strings.TrimSpace(string(out)) == "" {
		c.why = "no origin remote"
		return
	}
	out, err := c.run.Output(c.p.Root, "gh", "repo", "view", "--json", "defaultBranchRef", "-q", ".defaultBranchRef.name")
	if errors.Is(err, exec.ErrNotFound) {
		c.why = "gh is not installed (https://cli.github.com)"
		return
	}
	c.branch = strings.TrimSpace(string(out))
	if err != nil || c.branch == "" {
		c.branch = strings.TrimPrefix(c.p.Manifest.Base, "origin/")
	}
}

func (c *ciSource) durations(ring string) ([]time.Duration, string) {
	c.resolve()
	if c.why != "" {
		return nil, c.why
	}
	out, err := c.run.Output(c.p.Root, "gh", "run", "list", "--workflow", ring+".yml", "--branch", c.branch,
		"--status", "completed", "--limit", strconv.Itoa(sampleSize), "--json", "databaseId,conclusion,createdAt,updatedAt")
	if err != nil {
		return nil, fmt.Sprintf("gh run list failed: %v", err)
	}
	var runs []ghRun
	if err := json.Unmarshal(out, &runs); err != nil {
		return nil, fmt.Sprintf("gh run list output: %v", err)
	}
	var samples []time.Duration
	for _, r := range runs {
		if r.Conclusion != "success" {
			continue
		}
		samples = append(samples, c.runDuration(r))
	}
	return samples, ""
}

// runDuration sums the work steps of a run's jobs; it falls back to the run's wall time when the jobs are unreadable.
func (c *ciSource) runDuration(r ghRun) time.Duration {
	wall := r.UpdatedAt.Sub(r.CreatedAt)
	out, err := c.run.Output(c.p.Root, "gh", "run", "view", strconv.FormatInt(r.DatabaseID, 10), "--json", "jobs")
	if err != nil {
		return wall
	}
	var jobs ghJobs
	if err := json.Unmarshal(out, &jobs); err != nil {
		return wall
	}
	var sum time.Duration
	for _, j := range jobs.Jobs {
		for _, s := range j.Steps {
			if isSetup(s.Name) || s.CompletedAt.Before(s.StartedAt) {
				continue
			}
			sum += s.CompletedAt.Sub(s.StartedAt)
		}
	}
	if sum <= 0 {
		return wall
	}
	return sum
}

// isSetup reports whether a step is runner setup or teardown rather than the ring's own work.
func isSetup(name string) bool {
	for _, prefix := range []string{"Set up", "Post ", "Checkout", "Install", "Complete job"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	for _, action := range []string{"actions/checkout", "actions/setup-", "actions/cache"} {
		if strings.Contains(name, action) {
			return true
		}
	}
	return false
}
