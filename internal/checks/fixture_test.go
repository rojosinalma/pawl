package checks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/makefile"
	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/ratchet"
	"github.com/rojosinalma/pawl/internal/report"
)

// pinnedSHA is a well-formed commit SHA for workflow fixtures.
const pinnedSHA = "0123456789abcdef0123456789abcdef01234567"

const checkWorkflow = `name: check
on: [pull_request]
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@` + pinnedSHA + ` # v4
      - run: make check
`

// fixture is a small Go project in a temporary directory.
type fixture struct {
	t   *testing.T
	dir string
	git bool
}

// newFixture writes a project that passes every check: manifest from the adapters' defaults, the generated
// Makefile and configurations, the hook, a pinned workflow and a recorded baseline. With git it is committed on
// main and checked out on feature/work, so main is the base ref.
func newFixture(t *testing.T, git bool) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir(), git: git}
	f.write("go.mod", "module example.com/app\n\ngo 1.25\n")
	f.write("go.sum", "")
	f.write("main.go", "package main\n\nfunc main() {}\n")
	f.write(".github/workflows/check.yml", checkWorkflow)
	f.writeMode(hookFile, "#!/bin/sh\nexec make precommit\n", 0o755)
	f.writeManifestAndConfigs()
	if git {
		f.run("git", "init", "-q", "-b", "main")
		f.run("git", "config", "core.hooksPath", hookDir)
	}
	f.rebaseline()
	if git {
		f.commit("base")
		f.run("git", "checkout", "-q", "-b", "feature/work")
	}
	return f
}

func (f *fixture) writeManifestAndConfigs() {
	f.t.Helper()
	m := manifest.Defaults()
	m.Stacks = []string{"go"}
	m.Base = "main"
	f.writeManifest(&m)
	p := f.load()
	m.Rules = p.DefaultRules()
	f.writeManifest(&m)
	for _, a := range p.Adapters {
		for _, c := range a.Configs(p.Params) {
			f.write(c.Path, c.Content)
		}
	}
	f.write("Makefile", makefile.Generate(p.Adapters, p.Params, m.Budgets.DiffCoverage, m.Base))
}

func (f *fixture) writeManifest(m *manifest.Manifest) {
	f.t.Helper()
	p, err := project.Detect(f.dir, m.Stacks)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := manifest.Write(f.dir, m, p.Catalog); err != nil {
		f.t.Fatal(err)
	}
}

// editManifest loads pawl.toml, applies the edit and writes it back.
func (f *fixture) editManifest(edit func(m *manifest.Manifest)) {
	f.t.Helper()
	m, err := manifest.Load(f.dir)
	if err != nil {
		f.t.Fatal(err)
	}
	edit(m)
	f.writeManifest(m)
}

// rebaseline records the current measurement as the working-tree baseline.
func (f *fixture) rebaseline() {
	f.t.Helper()
	cur, err := Measure(f.load())
	if err != nil {
		f.t.Fatal(err)
	}
	prev, _, err := ratchet.Load(f.dir)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := ratchet.Write(f.dir, ratchet.FromCurrent(cur, prev)); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) write(rel, content string) { f.writeMode(rel, content, 0o644) }

func (f *fixture) writeMode(rel, content string, mode os.FileMode) {
	f.t.Helper()
	path := filepath.Join(f.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) remove(rel string) {
	f.t.Helper()
	if err := os.RemoveAll(filepath.Join(f.dir, rel)); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) run(name string, args ...string) string {
	f.t.Helper()
	cmd := exec.CommandContext(f.t.Context(), name, args...)
	cmd.Dir = f.dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func (f *fixture) commit(msg string) {
	f.t.Helper()
	f.run("git", "add", "-A")
	f.run("git", "commit", "-q", "--no-verify", "--allow-empty", "-m", msg)
}

func (f *fixture) load() *project.Project {
	f.t.Helper()
	p, err := project.Load(f.dir)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

// check runs the verification and fails the test on a run error.
func (f *fixture) check(ring string, only ...string) *report.Report {
	f.t.Helper()
	rep, err := Run(f.load(), Options{Ring: ring, Only: only})
	if err != nil {
		f.t.Fatal(err)
	}
	return rep
}

// find returns the findings of the rule whose message contains substr.
func find(rep *report.Report, rule, substr string) []report.Finding {
	var out []report.Finding
	for _, x := range rep.Findings {
		if x.RuleID == rule && strings.Contains(x.Message, substr) {
			out = append(out, x)
		}
	}
	return out
}

// requireFinding fails unless the rule has a finding of the severity containing substr, and returns it.
func requireFinding(t *testing.T, rep *report.Report, rule string, sev report.Severity, substr string) report.Finding {
	t.Helper()
	for _, x := range find(rep, rule, substr) {
		if x.Severity == sev {
			if x.Fix == "" {
				t.Errorf("%s finding %q has no fix", rule, x.Message)
			}
			return x
		}
	}
	t.Fatalf("no %s %s finding containing %q; findings:\n%s", sev, rule, substr, dump(rep))
	return report.Finding{}
}

func requireClean(t *testing.T, rep *report.Report) {
	t.Helper()
	if len(rep.Findings) > 0 {
		t.Fatalf("expected no findings, got:\n%s", dump(rep))
	}
}

func dump(rep *report.Report) string {
	var b strings.Builder
	for _, x := range rep.Findings {
		b.WriteString(string(x.Severity) + " " + x.RuleID + " " + x.File + ": " + x.Message + "\n")
	}
	for _, s := range rep.Skipped {
		b.WriteString("skipped " + s.RuleID + ": " + s.Reason + "\n")
	}
	return b.String()
}

func skipped(rep *report.Report, rule string) bool {
	for _, s := range rep.Skipped {
		if s.RuleID == rule {
			return true
		}
	}
	return false
}
