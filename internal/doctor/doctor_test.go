package doctor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rojosinalma/pawl/internal/gitx"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/report"
	"github.com/rojosinalma/pawl/internal/runner"
	"github.com/rojosinalma/pawl/internal/stack"
	"github.com/rojosinalma/pawl/internal/testrepo"
)

type reply struct {
	out string
	err error
}

// fakeRunner answers by command line; an unknown command is "not found".
type fakeRunner struct {
	replies map[string]reply
	running atomic.Int32
	peak    atomic.Int32
}

func (f *fakeRunner) Output(_, name string, args ...string) ([]byte, error) {
	n := f.running.Add(1)
	defer f.running.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	time.Sleep(time.Millisecond)
	key := strings.Join(append([]string{name}, args...), " ")
	r, ok := f.replies[key]
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, exec.ErrNotFound)
	}
	return []byte(r.out), r.err
}

type fakeAdapter struct {
	stack.Adapter
	tools []stack.Tool
}

func (f fakeAdapter) Tools() []stack.Tool { return f.tools }

func baseReplies() map[string]reply {
	return map[string]reply{
		"make --version": {out: "GNU Make 4.3\n"},
		"git --version":  {out: "git version 2.47.0\n"},
		"gh --version":   {out: "gh version 2.60.0 (2026-01-01)\n"},
	}
}

func tools() []stack.Tool {
	return []stack.Tool{
		{Name: "go", Check: []string{"go", "version"}, Install: "mise use go", Rings: []string{"precommit", "check"}},
		{Name: "golangci-lint", Version: "v2.15.0", Check: []string{"golangci-lint", "version", "--short"}, Install: "mise use golangci-lint@2.15.0", Rings: []string{"check"}},
		{Name: "gremlins", Version: "0.6.0", Check: []string{"gremlins", "--version"}, Install: "mise use gremlins", Rings: []string{"nightly"}},
		{Name: "nover", Install: "x"},
	}
}

func newProject(t *testing.T) *project.Project {
	t.Helper()
	return &project.Project{Root: t.TempDir(), Adapters: []stack.Adapter{fakeAdapter{tools: tools()}, fakeAdapter{tools: tools()[:1]}}}
}

func messages(r *report.Report) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, string(f.Severity)+" "+f.RuleID+" "+f.Message)
	}
	return out
}

func TestDoctorMissingAndMismatch(t *testing.T) {
	p := newProject(t)
	replies := baseReplies()
	delete(replies, "gh --version")
	replies["golangci-lint version --short"] = reply{out: "2.14.0\n"}
	r, err := run(p, &fakeRunner{replies: replies}, false, "/usr/bin/mise")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(messages(r), "\n")
	want := strings.Join([]string{
		"warning repo.pinned gh is not installed: `pawl status` and the CI ring budgets need it",
		"error repo.pinned go is not installed (rings: precommit, check)",
		"error repo.pinned golangci-lint is 2.14.0, the pin is 2.15.0",
		"warning repo.pinned gremlins is not installed (rings: nightly)",
	}, "\n")
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if r.Findings[1].Fix != "mise use go" {
		t.Fatalf("fix %q", r.Findings[1].Fix)
	}
	if len(r.Skipped) != 2 || r.Skipped[0].Reason != "nover: no version command" || r.Skipped[1].RuleID != ruleHook {
		t.Fatalf("skipped %+v", r.Skipped)
	}
	if strings.Join(r.Checked, ",") != "repo.pinned,repo.hook" {
		t.Fatalf("checked %v", r.Checked)
	}
}

func TestDoctorMissingMakeFix(t *testing.T) {
	replies := baseReplies()
	delete(replies, "make --version")
	r, _ := run(&project.Project{Root: t.TempDir()}, &fakeRunner{replies: replies}, false, "")
	if len(r.Findings) != 1 || r.Findings[0].Message != "make is not installed (rings: all)" ||
		r.Findings[0].Fix != "`pawl install` installs it (through mise)" || !r.Failed() {
		t.Fatalf("findings %+v", r.Findings)
	}
}

func TestDoctorHealthyVerbose(t *testing.T) {
	p := newProject(t)
	replies := baseReplies()
	replies["go version"] = reply{out: "go version go1.25.14 linux/amd64\n"}
	replies["golangci-lint version --short"] = reply{out: "2.15.0\n"}
	replies["gremlins --version"] = reply{out: "gremlins version 0.6.0\n"}
	r, err := run(p, &fakeRunner{replies: replies}, true, "/usr/bin/mise")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(messages(r), "\n")
	if !strings.Contains(got, "note repo.pinned go 1.25.14") || !strings.Contains(got, "note repo.pinned golangci-lint 2.15.0") || r.Failed() {
		t.Fatalf("got\n%s", got)
	}
	r, _ = run(p, &fakeRunner{replies: replies}, false, "/usr/bin/mise")
	if len(r.Findings) != 0 {
		t.Fatalf("quiet run has findings: %v", messages(r))
	}
}

func TestDoctorFailingCheckCommand(t *testing.T) {
	p := &project.Project{Root: t.TempDir(), Adapters: []stack.Adapter{fakeAdapter{tools: []stack.Tool{
		{Name: "cargo-clippy", Check: []string{"cargo", "clippy", "--version"}, Install: "rustup component add clippy", Rings: []string{"check"}},
		{Name: "weird", Check: []string{"weird"}, Rings: []string{"nightly"}},
	}}}}
	replies := baseReplies()
	replies["cargo clippy --version"] = reply{out: "error: no such command: `clippy`\n", err: errors.New("exit status 101")}
	replies["weird"] = reply{err: errors.New("exit status 2")}
	r, _ := run(p, &fakeRunner{replies: replies}, false, "/usr/bin/mise")
	got := strings.Join(messages(r), "\n")
	want := "error repo.pinned cargo-clippy: `cargo clippy --version` failed: error: no such command: `clippy`\n" +
		"warning repo.pinned weird: `weird` failed: exit status 2"
	if got != want {
		t.Fatalf("got\n%s", got)
	}
	if foundVersion("v-less\nmore") != "v-less" {
		t.Fatal("first line fallback")
	}
}

func TestDoctorBound(t *testing.T) {
	var list []stack.Tool
	for i := 0; i < 30; i++ {
		list = append(list, stack.Tool{Name: fmt.Sprintf("t%d", i), Check: []string{fmt.Sprintf("t%d", i)}})
	}
	p := &project.Project{Root: t.TempDir(), Adapters: []stack.Adapter{fakeAdapter{tools: list}}}
	fr := &fakeRunner{replies: baseReplies()}
	r, _ := run(p, fr, false, "/usr/bin/mise")
	if fr.peak.Load() > parallel {
		t.Fatalf("peak %d > %d", fr.peak.Load(), parallel)
	}
	if len(r.Findings) != 30 || !strings.HasPrefix(r.Findings[29].Message, "t29 ") {
		t.Fatalf("order: %v", messages(r))
	}
}

func hookMessages(p *project.Project) string {
	var out []string
	for _, f := range hookFindings(p) {
		out = append(out, f.Message+" | "+f.Fix)
	}
	return strings.Join(out, "\n")
}

func TestHook(t *testing.T) {
	root := t.TempDir()
	testrepo.Git(t, root, "init", "-q")
	p := &project.Project{Root: root, Repo: &gitx.Repo{Root: root}}
	if got := hookMessages(p); got != "core.hooksPath is unset, not .githooks | git config core.hooksPath .githooks\nthe pre-commit hook is missing | pawl init" {
		t.Fatalf("fresh repo:\n%s", got)
	}
	testrepo.Git(t, root, "config", "core.hooksPath", ".githooks")
	hook := filepath.Join(root, ".githooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nmake precommit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := hookMessages(p); got != "the pre-commit hook is not executable | chmod +x .githooks/pre-commit" {
		t.Fatalf("not executable:\n%s", got)
	}
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := hookMessages(p); got != "" {
		t.Fatalf("healthy:\n%s", got)
	}
	testrepo.Git(t, root, "config", "core.hooksPath", filepath.Join(root, ".githooks"))
	if got := hookMessages(p); got != "" {
		t.Fatalf("absolute path:\n%s", got)
	}
	testrepo.Git(t, root, "config", "core.hooksPath", ".husky")
	if got := hookMessages(p); !strings.HasPrefix(got, "core.hooksPath is .husky") {
		t.Fatalf("other path:\n%s", got)
	}
}

func TestRunRealExec(t *testing.T) {
	root := t.TempDir()
	testrepo.Git(t, root, "init", "-q")
	p := &project.Project{Root: root, Repo: &gitx.Repo{Root: root}}
	r, err := Run(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Findings {
		if f.Message == "git is not installed (rings: all)" {
			t.Fatal("git is installed in the test environment")
		}
	}
	if _, err := (runner.Exec{Timeout: time.Second, IsCombined: true}).Output(root, "git", "no-such-command"); err == nil {
		t.Fatal("want error")
	}
}

// writeScript writes an executable shell script at dir/name.
func writeScript(t *testing.T, dir, name, body string) {
	t.Helper()
	testrepo.Write(t, dir, name, "#!/bin/sh\n"+body+"\n")
	if err := os.Chmod(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestMiseNotInstalled(t *testing.T) {
	p := newProject(t)
	replies := baseReplies()
	for _, k := range []string{"go version", "golangci-lint version --short", "gremlins --version"} {
		replies[k] = reply{out: "2.15.0 0.6.0"}
	}
	const warning = "warning repo.pinned mise is not installed; `pawl install` installs it and the pinned tools"
	r, _ := run(p, &fakeRunner{replies: replies}, false, "")
	if strings.Contains(strings.Join(messages(r), "\n"), warning) {
		t.Error("no mise config, no warning")
	}
	if err := os.WriteFile(filepath.Join(p.Root, "mise.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ = run(p, &fakeRunner{replies: replies}, false, "")
	if got := strings.Join(messages(r), "\n"); !strings.Contains(got, warning) {
		t.Errorf("findings:\n%s", got)
	}
	r, _ = run(p, &fakeRunner{replies: replies}, false, "/usr/bin/mise")
	if strings.Contains(strings.Join(messages(r), "\n"), warning) {
		t.Error("mise is there")
	}
}

// TestToolsFromTheToolEnvironment: a tool only mise's bin-paths know is found and passes its pin.
func TestToolsFromTheToolEnvironment(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mise.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tools, bin := t.TempDir(), t.TempDir()
	writeScript(t, tools, "pinnedtool", "echo pinnedtool 1.2.3")
	writeScript(t, bin, "mise", "echo "+tools)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("HOME", t.TempDir())
	tool := stack.Tool{Name: "pinnedtool", Version: "1.2.3", Check: []string{"pinnedtool", "--version"}, Rings: []string{"check"}}
	p := &project.Project{Root: root, Adapters: []stack.Adapter{fakeAdapter{tools: []stack.Tool{tool}}}}
	r, err := Run(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range messages(r) {
		if strings.Contains(m, "pinnedtool") || strings.Contains(m, "mise is not installed") {
			t.Errorf("finding: %s", m)
		}
	}
}
