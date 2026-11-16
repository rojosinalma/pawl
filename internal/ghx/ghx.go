// Package ghx reads pull request state through the gh command line for `pawl status`.
package ghx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/rojosinalma/pawl/internal/project"
)

const cmdTimeout = 30 * time.Second

// runner runs an outbound command and returns its standard output.
type runner interface {
	Output(dir, name string, args ...string) ([]byte, error)
}

type execRunner struct{ timeout time.Duration }

// Output runs the command in dir under the runner's timeout and returns its standard output; a failure carries
// the command line and its standard error.
func (e execRunner) Output(dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// check is one entry of statusCheckRollup: a CheckRun (name, status, conclusion, detailsUrl) or a StatusContext
// (context, state, targetUrl).
type check struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"detailsUrl"`
	Context    string `json:"context"`
	State      string `json:"state"`
	TargetURL  string `json:"targetUrl"`
}

type pullRequest struct {
	Number           int     `json:"number"`
	URL              string  `json:"url"`
	Title            string  `json:"title"`
	State            string  `json:"state"`
	MergeStateStatus string  `json:"mergeStateStatus"`
	Checks           []check `json:"statusCheckRollup"`
}

var (
	runIDPattern    = regexp.MustCompile(`/actions/runs/(\d+)`)
	locationPattern = regexp.MustCompile(`^[\w./-]+:\d+(:\d+)?:? `)
	timestampPrefix = regexp.MustCompile(`^\x{FEFF}?\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z ?`)
	makeTarget      = regexp.MustCompile(`make (lint|test|check|coverage)\b`)
	ruleIDPattern   = regexp.MustCompile(`(?:^|\s)([a-z]+\.[a-z-]+):`)
)

// Status prints the state of the current branch's pull request: each failing check, the first file and line of its
// failure when the log has one, and the command that fixes it. failed is true when a required check failed.
// Without gh, a remote or a pull request it explains what is missing and returns failed=false, err=nil.
func Status(p *project.Project, w io.Writer) (failed bool, err error) {
	return status(p, w, execRunner{timeout: cmdTimeout})
}

// printer writes lines and keeps the first write error.
type printer struct {
	w   io.Writer
	err error
}

func (o *printer) printf(format string, args ...any) {
	if o.err == nil {
		_, o.err = fmt.Fprintf(o.w, format, args...)
	}
}

func status(p *project.Project, w io.Writer, run runner) (bool, error) {
	pr, why, err := loadPR(p, run)
	if err != nil {
		return false, err
	}
	o := &printer{w: w}
	if why != "" {
		o.printf("pawl status: %s\n", why)
		return false, o.err
	}
	merge := ""
	if pr.MergeStateStatus != "" {
		merge = ", merge " + strings.ToLower(pr.MergeStateStatus)
	}
	o.printf("#%d %s (%s%s) %s\n", pr.Number, pr.Title, strings.ToLower(pr.State), merge, pr.URL)
	if len(pr.Checks) == 0 {
		o.printf("  no checks\n")
	}
	failed := false
	for _, c := range pr.Checks {
		state := c.state()
		o.printf("  %-9s %s\n", state, c.title())
		if isFailure(state) {
			failed = true
			explain(p, o, run, c)
		}
	}
	if o.err != nil {
		return failed, fmt.Errorf("write status: %w", o.err)
	}
	return failed, nil
}

// hasOrigin reports whether the repository has an origin remote.
func hasOrigin(p *project.Project, run runner) bool {
	out, err := run.Output(p.Root, "git", "remote", "get-url", "origin")
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// loadPR returns the pull request, or why there is none to show, or an error from gh itself.
func loadPR(p *project.Project, run runner) (*pullRequest, string, error) {
	if p.Repo == nil {
		return nil, "not a git repository", nil
	}
	if !hasOrigin(p, run) {
		return nil, "no origin remote; pull request checks need a GitHub remote", nil
	}
	out, err := run.Output(p.Root, "gh", "pr", "view", "--json", "number,url,title,state,mergeStateStatus,statusCheckRollup")
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return nil, "gh is not installed; get it from https://cli.github.com to see pull request checks", nil
	case err != nil && strings.Contains(err.Error(), "no pull requests found"):
		return nil, "no pull request for this branch; open one with `gh pr create`", nil
	case err != nil:
		return nil, "", fmt.Errorf("read the pull request: %w", err)
	}
	var pr pullRequest
	if err := json.Unmarshal(out, &pr); err != nil {
		return nil, "", fmt.Errorf("parse gh pr view output: %w", err)
	}
	return &pr, "", nil
}

func (c check) title() string {
	if c.Name != "" {
		return c.Name
	}
	return c.Context
}

// state is the lower-case outcome: the conclusion of a completed check run, "pending" for a running one, the state of
// a status context.
func (c check) state() string {
	switch {
	case c.Context != "" && c.Name == "":
		if s := strings.ToLower(c.State); s != "pending" && s != "expected" {
			return s
		}
		return "pending"
	case c.Status != "" && c.Status != "COMPLETED":
		return "pending"
	case c.Conclusion == "":
		return "pending"
	}
	return strings.ToLower(c.Conclusion)
}

func isFailure(state string) bool {
	switch state {
	case "failure", "timed_out", "cancelled", "action_required", "error":
		return true
	}
	return false
}

// explain prints the first file:line of a failed GitHub Actions check and the commands that help fix it.
func explain(p *project.Project, o *printer, run runner, c check) {
	m := runIDPattern.FindStringSubmatch(c.DetailsURL + " " + c.TargetURL)
	if m == nil {
		return
	}
	out, err := run.Output(p.Root, "gh", "run", "view", m[1], "--log-failed")
	if err != nil {
		o.printf("            log unavailable: %v\n", err)
		return
	}
	step, line := firstFinding(string(out))
	if line != "" {
		o.printf("            %s\n", line)
	}
	if t := makeTarget.FindString(step); t != "" {
		o.printf("            fix: run %s locally\n", t)
	}
	if id := ruleID(p, line); id != "" {
		o.printf("            see: pawl rules show %s\n", id)
	}
}

// firstFinding scans `gh run view --log-failed` output (job TAB step TAB timestamp message) and returns the failed
// step's name and the first line that looks like path:line: message.
func firstFinding(log string) (step, line string) {
	for _, raw := range strings.Split(log, "\n") {
		parts := strings.SplitN(raw, "\t", 3)
		msg := parts[len(parts)-1]
		if len(parts) == 3 && step == "" {
			step = parts[1]
		}
		msg = strings.TrimSpace(strings.TrimPrefix(timestampPrefix.ReplaceAllString(msg, ""), "##[error]"))
		if locationPattern.MatchString(msg + " ") {
			if len(parts) == 3 {
				step = parts[1]
			}
			return step, msg
		}
	}
	return step, ""
}

// ruleID returns the pawl rule named in the message part of a finding line, when the catalog knows it.
func ruleID(p *project.Project, line string) string {
	msg := locationPattern.ReplaceAllString(line+" ", "")
	for _, m := range ruleIDPattern.FindAllStringSubmatch(msg, -1) {
		if p.Catalog == nil {
			return m[1]
		}
		if _, ok := p.Catalog.Get(m[1]); ok {
			return m[1]
		}
	}
	return ""
}
