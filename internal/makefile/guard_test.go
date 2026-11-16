package makefile

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/stack"
	"github.com/rojosinalma/pawl/internal/testrepo"
)

// guardRoot writes the generated Makefile of the fake adapter and a fake pawl that records its arguments, one call
// per line, and exits with PAWL_FAKE_EXIT. It returns the root and the directory of the fake pawl.
func guardRoot(t *testing.T) (root, bin string) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	testrepo.Isolate(t)
	root, bin = t.TempDir(), t.TempDir()
	testrepo.Write(t, root, "Makefile", Generate([]stack.Adapter{fakeAdapter{}}, stack.Params{}, 80, "origin/main"))
	script := "#!/bin/sh\necho \"$*\" >> \"" + filepath.Join(root, "calls") + "\"\nexit \"${PAWL_FAKE_EXIT:-0}\"\n"
	if err := os.WriteFile(filepath.Join(bin, "pawl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, bin
}

// runMake runs the targets with the fake tools succeeding and the extra environment; it returns stderr and the error.
func runMake(t *testing.T, root string, env []string, targets ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "make", append([]string{"--no-print-directory", "FAKE=true"}, targets...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

func TestGuardRunsPawlWhenItIsInstalled(t *testing.T) {
	root, bin := guardRoot(t)
	env := []string{"PAWL=pawl", "PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}
	stderr, err := runMake(t, root, env, "test", "precommit", "nightly")
	if err != nil || stderr != "" {
		t.Fatalf("make with pawl installed: %v\n%s", err, stderr)
	}
	raw, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), "verify --ring precommit\nverify\nverify --ring nightly\nbudget\n"; got != want {
		t.Errorf("pawl calls = %q, want %q", got, want)
	}
	if stderr, err = runMake(t, root, append(env, "PAWL_FAKE_EXIT=3"), "check"); err == nil {
		t.Errorf("a failing pawl verify must fail make check:\n%s", stderr)
	}
}

func TestGuardSkipsPawlWhenItIsMissing(t *testing.T) {
	root, _ := guardRoot(t)
	stderr, err := runMake(t, root, []string{"PAWL=" + filepath.Join(root, "nonexistent", "pawl")}, "test", "precommit", "nightly")
	if err != nil {
		t.Fatalf("make without pawl must pass: %v\n%s", err, stderr)
	}
	want := "make test: warning: pawl is not installed; every test runs\n" +
		"make precommit: warning: pawl is not installed; skipped: pawl verify --ring precommit\n" +
		"make check: warning: pawl is not installed; skipped: pawl verify\n" +
		"make nightly: warning: pawl is not installed; skipped: pawl verify --ring nightly\n" +
		"make nightly: warning: pawl is not installed; skipped: pawl budget\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestGuardFailsWhenPawlIsRequired(t *testing.T) {
	root, _ := guardRoot(t)
	env := []string{"PAWL=" + filepath.Join(root, "nonexistent", "pawl"), "PAWL_REQUIRED=1"}
	// make check stops at its prerequisite test, whose guard line fails first
	for target, failing := range map[string]string{"check": "test", "test": "test", "precommit": "precommit"} {
		stderr, err := runMake(t, root, env, target)
		if err == nil || !strings.Contains(stderr, "make "+failing+": pawl is not installed and PAWL_REQUIRED=1\n") {
			t.Errorf("make %s with PAWL_REQUIRED=1 and no pawl: %v\n%s", target, err, stderr)
		}
	}
}
