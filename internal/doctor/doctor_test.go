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
	"github.com/rojosinalma/pawl/internal/stack"
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
	r, err := run(p, &fakeRunner{replies: replies}, false)
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

func TestDoctorHealthyVerbose(t *testing.T) {
	p := newProject(t)
	replies := baseReplies()
	replies["go version"] = reply{out: "go version go1.25.14 linux/amd64\n"}
	replies["golangci-lint version --short"] = reply{out: "2.15.0\n"}
	replies["gremlins --version"] = reply{out: "gremlins version 0.6.0\n"}
	r, err := run(p, &fakeRunner{replies: replies}, true)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(messages(r), "\n")
	if !strings.Contains(got, "note repo.pinned go 1.25.14") || !strings.Contains(got, "note repo.pinned golangci-lint 2.15.0") || r.Failed() {
		t.Fatalf("got\n%s", got)
	}
	r, _ = run(p, &fakeRunner{replies: replies}, false)
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
	r, _ := run(p, &fakeRunner{replies: replies}, false)
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
	r, _ := run(p, fr, false)
	if fr.peak.Load() > parallel {
		t.Fatalf("peak %d > %d", fr.peak.Load(), parallel)
	}
	if len(r.Findings) != 30 || !strings.HasPrefix(r.Findings[29].Message, "t29 ") {
		t.Fatalf("order: %v", messages(r))
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
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
	git(t, root, "init", "-q")
	p := &project.Project{Root: root, Repo: &gitx.Repo{Root: root}}
	if got := hookMessages(p); got != "core.hooksPath is unset, not .githooks | git config core.hooksPath .githooks\nthe pre-commit hook is missing | pawl init" {
		t.Fatalf("fresh repo:\n%s", got)
	}
	git(t, root, "config", "core.hooksPath", ".githooks")
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
	git(t, root, "config", "core.hooksPath", filepath.Join(root, ".githooks"))
	if got := hookMessages(p); got != "" {
		t.Fatalf("absolute path:\n%s", got)
	}
	git(t, root, "config", "core.hooksPath", ".husky")
	if got := hookMessages(p); !strings.HasPrefix(got, "core.hooksPath is .husky") {
		t.Fatalf("other path:\n%s", got)
	}
}

func TestRunRealExec(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q")
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
	if _, err := (execRunner{timeout: time.Second}).Output(root, "git", "no-such-command"); err == nil {
		t.Fatal("want error")
	}
}
