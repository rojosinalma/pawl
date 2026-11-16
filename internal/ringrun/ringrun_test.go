package ringrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/testrepo"
)

// ringMakefile has the three ring targets; check runs three lint steps, two of which fail when FAIL=1.
const ringMakefile = `.PHONY: precommit check nightly lint lint-a lint-b lint-c slow
precommit:
	@echo precommit-out
check: lint-a lint-b lint-c
	@echo gate-out
lint-a:
	@echo a-out
lint-b:
	@echo b-out; echo b-err >&2; [ "$(FAIL)" != 1 ]
lint-c:
	@echo c-out; [ "$(FAIL)" != 1 ]
nightly: slow
slow:
	sleep 30
`

func newProject(t *testing.T, files map[string]string) *project.Project {
	t.Helper()
	return &project.Project{Root: testrepo.New(t, files)}
}

// fakeExecutor answers `make --version` with version and the ring run with output and code; it records the calls.
type fakeExecutor struct {
	version string
	output  string
	code    int
	err     error
	calls   []command
}

func (f *fakeExecutor) run(_ context.Context, c command) (int, error) {
	f.calls = append(f.calls, c)
	if f.err != nil {
		return -1, f.err
	}
	if c.args[len(c.args)-1] == "--version" {
		_, _ = io.WriteString(c.stdout, f.version)
		return 0, nil
	}
	_, _ = io.WriteString(c.stdout, f.output)
	return f.code, nil
}

func TestRunRejectsBadNamesAndMissingTargets(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		ring  string
		want  string
	}{
		{"not a target name", map[string]string{"Makefile": ringMakefile}, "--eval=x", "is not a make target name"},
		{"an assignment", map[string]string{"Makefile": ringMakefile}, "PAWL=evil", "is not a make target name"},
		{"no Makefile", map[string]string{"README.md": "x\n"}, "check", "no make target check"},
		{"target missing", map[string]string{"Makefile": "precommit:\n\t@true\n"}, "nightly", "no make target nightly in Makefile; run `pawl init`"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ex := &fakeExecutor{}
			_, err := run(t.Context(), ex, newProject(t, c.files), c.ring, Options{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
			if len(ex.calls) != 0 {
				t.Errorf("make ran: %v", ex.calls)
			}
		})
	}
}

func TestRunToolbox(t *testing.T) {
	p := newProject(t, map[string]string{"Makefile": ringMakefile})
	testrepo.Git(t, p.Root, "config", "pawl.toolbox", "ghcr.io/x/toolbox:v1")
	environ := []string{"PATH=/usr/bin", "PAWL_FULL=1"}
	ex := &fakeExecutor{version: "GNU Make 4.4.1\n", output: "Makefile:2: update target 'check' due to: target is .PHONY\n"}
	passed, err := run(t.Context(), ex, p, "check", Options{Environ: environ})
	if err != nil || !passed {
		t.Fatalf("passed %v, err %v", passed, err)
	}
	user := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	want := []string{
		"docker run --rm -u " + user + " -v " + p.Root + ":/work -w /work -e PAWL_FULL ghcr.io/x/toolbox:v1 make --version",
		"docker run --rm -u " + user + " -v " + p.Root + ":/work -w /work -e PAWL_FULL ghcr.io/x/toolbox:v1 make -k --trace check",
	}
	for i, c := range ex.calls {
		if got := c.name + " " + strings.Join(c.args, " "); got != want[i] || c.dir != p.Root || len(c.env) != 2 {
			t.Errorf("call %d = %s in %s with %v, want\n%s", i, got, c.dir, c.env, want[i])
		}
	}
}

func TestRunNoToolboxAndMissingTools(t *testing.T) {
	p := newProject(t, map[string]string{"Makefile": ringMakefile})
	testrepo.Git(t, p.Root, "config", "pawl.toolbox", "ghcr.io/x/toolbox:v1")
	ex := &fakeExecutor{version: "GNU Make 4.4.1\n"}
	if _, err := run(t.Context(), ex, p, "check", Options{NoToolbox: true}); err != nil {
		t.Fatal(err)
	}
	if got := ex.calls[1].name + " " + strings.Join(ex.calls[1].args, " "); got != "make -k --trace check" || ex.calls[1].env != nil {
		t.Errorf("NoToolbox ran %q with env %v, want make on this machine with the inherited environment", got, ex.calls[1].env)
	}

	ex = &fakeExecutor{err: fmt.Errorf("run docker: %w", exec.ErrNotFound)}
	_, err := run(t.Context(), ex, p, "check", Options{})
	if err == nil || !strings.Contains(err.Error(), "docker is not available") || !strings.Contains(err.Error(), "--no-toolbox") {
		t.Errorf("docker missing: err = %v", err)
	}
	if _, err := run(t.Context(), ex, p, "check", Options{NoToolbox: true}); err == nil || !strings.Contains(err.Error(), "make is not available") {
		t.Errorf("make missing: err = %v", err)
	}
	if _, err := run(t.Context(), ex, p, "check", Options{NoToolbox: true, Verbose: true}); err == nil || !strings.Contains(err.Error(), "make is not available") {
		t.Errorf("make missing, verbose: err = %v", err)
	}
}

func TestRunFakeMake381(t *testing.T) {
	p := newProject(t, map[string]string{"Makefile": ringMakefile})
	ex := &fakeExecutor{version: "GNU Make 3.81\n", output: sample381, code: 2}
	var out bytes.Buffer
	passed, err := run(t.Context(), ex, p, "check", Options{Stdout: &out})
	if err != nil || passed {
		t.Fatalf("passed %v, err %v", passed, err)
	}
	if got := strings.Join(ex.calls[1].args, " "); got != "-k check" {
		t.Errorf("make 3.81 ran with %q, want no --trace", got)
	}
	lines := splitLines(out.String())
	if !strings.HasSuffix(lines[0], ": lint-b, lint-c") || len(lines) != 1+len(splitLines(sample381)) {
		t.Errorf("output:\n%s", out.String())
	}

	ex = &fakeExecutor{version: "GNU Make 3.81\n"}
	out.Reset()
	if _, err := run(t.Context(), ex, p, "check", Options{Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "pawl: check passed in ") || strings.Contains(out.String(), "steps") {
		t.Errorf("pass without trace: %q", out.String())
	}
}

// The tests below run the real make of the test environment.

func TestRunRealPass(t *testing.T) {
	p := newProject(t, map[string]string{"Makefile": ringMakefile})
	var out, errOut bytes.Buffer
	passed, err := Run(t.Context(), p, "check", Options{Stdout: &out, Stderr: &errOut})
	if err != nil || !passed {
		t.Fatalf("passed %v, err %v, output %s", passed, err, out.String())
	}
	lines := splitLines(out.String())
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "pawl: check passed in ") || errOut.Len() != 0 {
		t.Fatalf("quiet pass printed:\n%s\nstderr:\n%s", out.String(), errOut.String())
	}
	if hasTrace(versionOf(t)) && !strings.HasSuffix(lines[0], "(4 steps)") {
		t.Errorf("want 4 steps: %s", lines[0])
	}
	if n := recorded(t, p.Root, "check"); n != 1 {
		t.Errorf("timings has %d check runs, want 1", n)
	}
}

func TestRunRealFail(t *testing.T) {
	p := newProject(t, map[string]string{"Makefile": ringMakefile})
	var out bytes.Buffer
	passed, err := Run(t.Context(), p, "check", Options{Stdout: &out, Environ: []string{"PATH=" + pathOf(t), "FAIL=1"}})
	if err != nil || passed {
		t.Fatalf("passed %v, err %v", passed, err)
	}
	got := out.String()
	t.Logf("quiet output of a failing ring:\n%s", got)
	if !strings.HasPrefix(got, "pawl: check failed in ") || !strings.Contains(strings.SplitN(got, "\n", 2)[0], ": lint-b, lint-c") {
		t.Errorf("summary: %s", got)
	}
	for _, want := range []string{"b-out", "b-err", "c-out", "lint-b] Error 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if hasTrace(versionOf(t)) && (strings.Contains(got, "a-out") || strings.Contains(got, "update target") ||
		!strings.Contains(got, "--- lint-b\n") || !strings.Contains(got, "--- lint-c\n")) {
		t.Errorf("want the segments of lint-b and lint-c only, without trace lines")
	}
	if n := recorded(t, p.Root, "check"); n != 1 {
		t.Errorf("timings has %d check runs, want 1", n)
	}
}

func TestRunRealVerbose(t *testing.T) {
	p := newProject(t, map[string]string{"Makefile": ringMakefile})
	var out, errOut bytes.Buffer
	passed, err := Run(t.Context(), p, "check", Options{Verbose: true, Stdout: &out, Stderr: &errOut, Environ: []string{"PATH=" + pathOf(t), "FAIL=1"}})
	if err != nil || passed {
		t.Fatalf("passed %v, err %v", passed, err)
	}
	for _, want := range []string{"a-out\n", "b-out\n", "c-out\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, out.String())
		}
	}
	if !strings.Contains(errOut.String(), "b-err") || strings.Contains(out.String(), "update target") {
		t.Errorf("stderr:\n%s\nstdout:\n%s", errOut.String(), out.String())
	}
	lines := splitLines(out.String())
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "pawl: check failed in ") || !strings.HasSuffix(last, ": lint-b, lint-c") {
		t.Errorf("summary: %s", last)
	}

	out.Reset()
	if passed, err := Run(t.Context(), p, "precommit", Options{Verbose: true, Stdout: &out}); err != nil || !passed {
		t.Fatalf("precommit: passed %v, err %v", passed, err)
	}
	if lines := splitLines(out.String()); len(lines) != 2 || lines[0] != "precommit-out" || !strings.HasPrefix(lines[1], "pawl: precommit passed in ") {
		t.Errorf("verbose precommit:\n%s", out.String())
	}
}

func TestRunRealCancel(t *testing.T) {
	p := newProject(t, map[string]string{"Makefile": ringMakefile})
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, p, "nightly", Options{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context's error", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("cancelled run took %s", d)
	}
	if n := recorded(t, p.Root, "nightly"); n != 0 {
		t.Errorf("a cancelled run was recorded")
	}
}

func versionOf(t *testing.T) string {
	t.Helper()
	out, _ := exec.CommandContext(t.Context(), "make", "--version").Output()
	return string(out)
}

func pathOf(t *testing.T) string {
	t.Helper()
	dir, err := exec.LookPath("make")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(dir) + ":/usr/bin:/bin"
}

func recorded(t *testing.T, root, ring string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".pawl/reports/timings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Runs map[string][]json.RawMessage `json:"runs"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return len(v.Runs[ring])
}

func TestRunAnyTargetButRecordOnlyRings(t *testing.T) {
	p := newProject(t, map[string]string{"Makefile": "fmt:\n\t@echo formatted\nprecommit:\n\t@true\n"})
	ex := &fakeExecutor{}
	if passed, err := run(t.Context(), ex, p, "fmt", Options{NoToolbox: true}); err != nil || !passed {
		t.Fatalf("fmt: passed=%v err=%v", passed, err)
	}
	if _, err := os.Stat(filepath.Join(p.Root, ".pawl", "reports", "timings.json")); err == nil {
		t.Error("a target that is not a ring was recorded")
	}
	if passed, err := run(t.Context(), ex, p, "precommit", Options{NoToolbox: true}); err != nil || !passed {
		t.Fatalf("precommit: passed=%v err=%v", passed, err)
	}
	if _, err := os.Stat(filepath.Join(p.Root, ".pawl", "reports", "timings.json")); err != nil {
		t.Errorf("the ring was not recorded: %v", err)
	}
}

func TestRunFindsMakeOnTheGivenPath(t *testing.T) {
	p := newProject(t, map[string]string{"Makefile": ringMakefile})
	environ := []string{"PATH=" + pathOf(t)}
	t.Setenv("PATH", t.TempDir())
	if passed, err := Run(t.Context(), p, "precommit", Options{Environ: environ}); err != nil || !passed {
		t.Fatalf("make on the PATH of Environ only: passed %v, err %v", passed, err)
	}
	_, err := Run(t.Context(), p, "precommit", Options{Environ: []string{"PATH=" + t.TempDir()}})
	if err == nil || !strings.Contains(err.Error(), "make is not available") {
		t.Errorf("no make anywhere: %v", err)
	}
}
