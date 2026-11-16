// Package testrepo builds the files and git repositories that tests run against.
package testrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Write writes content to root/rel, creating the parent directories.
func Write(t testing.TB, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Git runs git in dir and fails the test on an error. The author and committer are set, so a commit works on a
// machine without a git identity.
func Git(t testing.TB, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// Isolate clears the PAWL_* variables for the test, so a run inside `make check` (which sets them for the real
// project) measures the fixture and not the caller's environment.
func Isolate(t testing.TB) {
	t.Helper()
	for _, name := range []string{"PAWL_BASE", "PAWL_FULL", "PAWL_VERBOSE", "PAWL_WORK_TYPE"} {
		t.Setenv(name, "")
	}
}

// New writes the files (path relative to the root → content) into a temporary directory and commits all of them
// as the one commit on main. It returns the root.
func New(t testing.TB, files map[string]string) string {
	t.Helper()
	Isolate(t)
	root := t.TempDir()
	for rel, content := range files {
		Write(t, root, rel, content)
	}
	Git(t, root, "init", "-q", "-b", "main")
	Git(t, root, "add", "-A")
	Git(t, root, "commit", "-q", "-m", "first")
	return root
}
