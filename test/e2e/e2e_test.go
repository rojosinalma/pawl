//go:build e2e

// Package e2e runs pawl against the fixture projects with the real stack tools. It needs every tool on PATH (the
// toolbox image has them) and the pawl binary named by PAWL_BIN (default: pawl on PATH).
//
// A fixture is test/fixtures/<stack>/pass or test/fixtures/<stack>/fail: a small project without any pawl file.
// The test copies it to a temporary git repository, runs `pawl init`, then:
//   - pass: the commands in test/fixtures/<stack>/setup (when present), then `make fmt-check lint test` must exit 0;
//     with PAWL_E2E_CHECK=1, `pawl baseline` and `pawl check` must exit 0 too.
//   - fail: `make lint` must exit non-zero and its output must contain every line of
//     test/fixtures/<stack>/fail.expect (when present), so the fixture fails for the reasons it was written for.
package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func pawlBin() string {
	if bin := os.Getenv("PAWL_BIN"); bin != "" {
		return bin
	}
	return "pawl"
}

func run(t *testing.T, dir string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PAWL="+pawlBin(), "PAWL_FULL=1",
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.com",
	)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

func must(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	out, err := run(t, dir, name, args...)
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return out
}

func prepare(t *testing.T, fixture string) string {
	t.Helper()
	dir := t.TempDir()
	must(t, ".", "cp", "-a", fixture+"/.", dir)
	must(t, dir, "git", "init", "-q", "-b", "main")
	must(t, dir, "git", "add", "-A")
	must(t, dir, "git", "commit", "-q", "-m", "fixture")
	must(t, dir, pawlBin(), "init")
	return dir
}

func TestFixtures(t *testing.T) {
	stacks, err := filepath.Glob("../fixtures/*")
	if err != nil || len(stacks) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	only := os.Getenv("PAWL_E2E_STACK")
	for _, stackDir := range stacks {
		stack := filepath.Base(stackDir)
		if only != "" && only != stack {
			continue
		}
		if _, err := os.Stat(filepath.Join(stackDir, "pass")); err == nil {
			t.Run(stack+"/pass", func(t *testing.T) {
				dir := prepare(t, filepath.Join(stackDir, "pass"))
				if setup, err := os.ReadFile(filepath.Join(stackDir, "setup")); err == nil {
					must(t, dir, "sh", "-ec", string(setup))
				}
				must(t, dir, "make", "fmt-check", "lint", "test")
				if os.Getenv("PAWL_E2E_CHECK") == "1" {
					must(t, dir, "git", "add", "-A")
					must(t, dir, pawlBin(), "baseline")
					must(t, dir, pawlBin(), "check")
				}
			})
		}
		if _, err := os.Stat(filepath.Join(stackDir, "fail")); err == nil {
			t.Run(stack+"/fail", func(t *testing.T) {
				dir := prepare(t, filepath.Join(stackDir, "fail"))
				if setup, err := os.ReadFile(filepath.Join(stackDir, "setup")); err == nil {
					must(t, dir, "sh", "-ec", string(setup))
				}
				out, err := run(t, dir, "make", "--keep-going", "lint")
				if err == nil {
					t.Fatalf("make lint passed on the fail fixture\n%s", out)
				}
				expect, err := os.ReadFile(filepath.Join(stackDir, "fail.expect"))
				if err != nil {
					return
				}
				for _, want := range strings.Split(strings.TrimSpace(string(expect)), "\n") {
					if want = strings.TrimSpace(want); want != "" && !strings.Contains(out, want) {
						t.Errorf("make lint output lacks %q\n%s", want, out)
					}
				}
			})
		}
	}
}
