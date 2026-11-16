// Package doctor verifies the machine: every tool the project's rings need is installed at the pinned version, the
// hook is active, make and git exist.
package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rojosinalma/pawl/internal/config"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/report"
	"github.com/rojosinalma/pawl/internal/stack"
)

const (
	rulePinned  = "repo.pinned"
	ruleHook    = "repo.hook"
	toolTimeout = 20 * time.Second
	parallel    = 8
	hooksDir    = ".githooks"
)

var versionPattern = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

// runner runs a command and returns its combined output (some tools print their version on stderr).
type runner interface {
	Output(dir, name string, args ...string) ([]byte, error)
}

type execRunner struct{ timeout time.Duration }

// Output runs the command in dir under the runner's timeout and returns standard output and standard error
// combined, since a version banner may land on either; a failure carries the command line.
func (e execRunner) Output(dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out.Bytes(), nil
}

// item is one program to check; required programs that are missing are errors, the others warnings.
type item struct {
	tool     stack.Tool
	required bool
	missing  string // the message when the program is absent; "" for the default
}

// result is the outcome of one item: at most one finding, or a skip reason.
type result struct {
	finding *report.Finding
	skip    string
}

// Run checks the tools of every active adapter (missing tool for the precommit or check ring: error; nightly-only:
// warning; version differs from the pin: error), make, git, the hook path, and gh (warning when absent).
func Run(p *project.Project) (*report.Report, error) {
	return run(p, execRunner{timeout: toolTimeout}, config.Verbose())
}

func run(p *project.Project, r runner, verbose bool) (*report.Report, error) {
	rep := &report.Report{Checked: []string{rulePinned, ruleHook}}
	for _, res := range checkAll(p.Root, items(p), r, verbose) {
		if res.finding != nil {
			rep.Add(*res.finding)
		}
		if res.skip != "" {
			rep.Skip(rulePinned, res.skip)
		}
	}
	if p.Repo == nil {
		rep.Skip(ruleHook, "not a git repository")
	} else {
		rep.Add(hookFindings(p)...)
	}
	return rep, nil
}

// items lists make, git and gh, then every adapter's tools in adapter order, each tool once.
func items(p *project.Project) []item {
	out := []item{
		{tool: stack.Tool{Name: "make", Check: []string{"make", "--version"}, Install: "install GNU make with the system package manager"}, required: true},
		{tool: stack.Tool{Name: "git", Check: []string{"git", "--version"}, Install: "install git with the system package manager"}, required: true},
		{tool: stack.Tool{Name: "gh", Check: []string{"gh", "--version"}, Install: "https://cli.github.com"},
			missing: "gh is not installed: `pawl status` and the CI ring budgets need it"},
	}
	seen := map[string]bool{}
	for _, a := range p.Adapters {
		for _, t := range a.Tools() {
			if seen[t.Name] {
				continue
			}
			seen[t.Name] = true
			out = append(out, item{tool: t, required: needed(t.Rings)})
		}
	}
	return out
}

func needed(rings []string) bool {
	for _, r := range rings {
		if r == "precommit" || r == "check" {
			return true
		}
	}
	return false
}

// checkAll runs the checks with at most `parallel` at once and returns the results in item order.
func checkAll(root string, list []item, r runner, verbose bool) []result {
	results := make([]result, len(list))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, it := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = checkOne(root, it, r, verbose)
		}()
	}
	wg.Wait()
	return results
}

func checkOne(root string, it item, r runner, verbose bool) result {
	t := it.tool
	if len(t.Check) == 0 {
		return result{skip: t.Name + ": no version command"}
	}
	out, err := r.Output(root, t.Check[0], t.Check[1:]...)
	if err != nil {
		f := absent(it, out, err)
		return result{finding: &f}
	}
	found := foundVersion(string(out))
	if want := strings.TrimPrefix(t.Version, "v"); want != "" && !strings.Contains(string(out), want) {
		f := report.Errorf(rulePinned, "", 0, "%s is %s, the pin is %s", t.Name, found, want).WithFix(t.Install)
		return result{finding: &f}
	}
	if verbose {
		return result{finding: &report.Finding{RuleID: rulePinned, Severity: report.Note, Message: t.Name + " " + found}}
	}
	return result{}
}

// absent builds the finding for a program that is missing or whose version command fails.
func absent(it item, out []byte, err error) report.Finding {
	t := it.tool
	var msg string
	switch {
	case errors.Is(err, exec.ErrNotFound) && it.missing != "":
		msg = it.missing
	case errors.Is(err, exec.ErrNotFound):
		msg = fmt.Sprintf("%s is not installed (rings: %s)", t.Name, ringList(it))
	default:
		msg = fmt.Sprintf("%s: `%s` failed: %s", t.Name, strings.Join(t.Check, " "), firstLine(string(out), err))
	}
	if it.required {
		return report.Errorf(rulePinned, "", 0, "%s", msg).WithFix(t.Install)
	}
	return report.Warnf(rulePinned, "", 0, "%s", msg).WithFix(t.Install)
}

func ringList(it item) string {
	if len(it.tool.Rings) == 0 {
		return "all"
	}
	return strings.Join(it.tool.Rings, ", ")
}

func firstLine(out string, err error) string {
	if line, _, _ := strings.Cut(strings.TrimSpace(out), "\n"); line != "" {
		return line
	}
	return err.Error()
}

// foundVersion returns the first version-looking string of the output, or its first line.
func foundVersion(out string) string {
	if v := versionPattern.FindString(out); v != "" {
		return v
	}
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return line
}

// hookFindings checks that core.hooksPath is .githooks and that the pre-commit hook is there and executable.
func hookFindings(p *project.Project) []report.Finding {
	var out []report.Finding
	hp := p.Repo.HooksPath()
	if !samePath(p.Root, hp, hooksDir) {
		shown := hp
		if shown == "" {
			shown = "unset"
		}
		out = append(out, report.Errorf(ruleHook, "", 0, "core.hooksPath is %s, not %s", shown, hooksDir).
			WithFix("git config core.hooksPath "+hooksDir))
	}
	hook := filepath.Join(hooksDir, "pre-commit")
	info, err := os.Stat(filepath.Join(p.Root, hook))
	switch {
	case err != nil:
		out = append(out, report.Errorf(ruleHook, hook, 0, "the pre-commit hook is missing").WithFix("pawl init"))
	case info.Mode()&0o111 == 0:
		out = append(out, report.Errorf(ruleHook, hook, 0, "the pre-commit hook is not executable").WithFix("chmod +x "+hook))
	}
	return out
}

// samePath reports whether the configured path names root/want, given relative or absolute.
func samePath(root, configured, want string) bool {
	if configured == "" {
		return false
	}
	if !filepath.IsAbs(configured) {
		configured = filepath.Join(root, configured)
	}
	return filepath.Clean(configured) == filepath.Join(root, want)
}
