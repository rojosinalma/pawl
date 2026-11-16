package testrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewCommitsEveryFileOnMain(t *testing.T) {
	root := New(t, map[string]string{"a.txt": "one\n", "dir/b.txt": "two\n"})
	raw, err := os.ReadFile(filepath.Join(root, "dir", "b.txt"))
	if err != nil || string(raw) != "two\n" {
		t.Fatalf("dir/b.txt = %q, %v", raw, err)
	}
	cmd := exec.CommandContext(t.Context(), "git", "log", "--format=%an %s", "main", "--", ".")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "test first" {
		t.Fatalf("log = %q, %v", out, err)
	}
	cmd = exec.CommandContext(t.Context(), "git", "status", "--porcelain")
	cmd.Dir = root
	if out, err := cmd.Output(); err != nil || len(out) != 0 {
		t.Fatalf("status = %q, %v", out, err)
	}
}
