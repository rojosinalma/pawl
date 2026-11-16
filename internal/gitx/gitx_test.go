package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repo(t *testing.T) (*Repo, string) {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	write(t, root, "a.txt", "one\n")
	write(t, root, ".gitignore", "ignored/\n")
	write(t, root, "ignored/x.txt", "x\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "first commit")
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return r, root
}

func TestOpenAndFiles(t *testing.T) {
	r, root := repo(t)
	if !IsRepo(root) || IsRepo(t.TempDir()) {
		t.Error("IsRepo is wrong")
	}
	write(t, root, "new.txt", "new\n")
	files, err := r.Files()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, []string{".gitignore", "a.txt", "new.txt"}) {
		t.Errorf("Files = %v", files)
	}
	tracked, err := r.Tracked()
	if err != nil || !reflect.DeepEqual(tracked, []string{".gitignore", "a.txt"}) {
		t.Errorf("Tracked = %v %v", tracked, err)
	}
	if r.Branch() != "main" {
		t.Errorf("Branch = %q", r.Branch())
	}
	if r.RemoteURL() != "" {
		t.Errorf("RemoteURL = %q", r.RemoteURL())
	}
}

func TestStagedChangedAndShow(t *testing.T) {
	r, root := repo(t)
	git(t, root, "branch", "base")
	write(t, root, "a.txt", "one\ntwo\nthree\n")
	write(t, root, "b.txt", "b\n")
	git(t, root, "add", "b.txt")
	staged, err := r.Staged()
	if err != nil || !reflect.DeepEqual(staged, []string{"b.txt"}) {
		t.Errorf("Staged = %v %v", staged, err)
	}
	base := r.MergeBase("base")
	if base == "" {
		t.Fatal("MergeBase returned nothing")
	}
	if r.MergeBase("no-such-ref") != "" {
		t.Error("MergeBase of an unknown ref is not empty")
	}
	changed, err := r.Changed(base)
	if err != nil || !reflect.DeepEqual(changed, []string{"a.txt", "b.txt"}) {
		t.Errorf("Changed = %v %v", changed, err)
	}
	added, deleted, err := r.ChangedLines(base)
	if err != nil || added != 3 || deleted != 0 {
		t.Errorf("ChangedLines = +%d -%d %v", added, deleted, err)
	}
	content, ok := r.Show(base, "a.txt")
	if !ok || string(content) != "one\n" {
		t.Errorf("Show = %q %v", content, ok)
	}
	if _, ok := r.Show(base, "b.txt"); ok {
		t.Error("Show returned a file that did not exist at the base")
	}
	git(t, root, "commit", "-q", "-am", "second commit")
	subjects, err := r.Log(base)
	if err != nil || !reflect.DeepEqual(subjects, []string{"second commit"}) {
		t.Errorf("Log = %v %v", subjects, err)
	}
}

func TestHooksPath(t *testing.T) {
	r, _ := repo(t)
	if r.HooksPath() != "" {
		t.Errorf("HooksPath before setting = %q", r.HooksPath())
	}
	if err := r.SetHooksPath(".githooks"); err != nil {
		t.Fatal(err)
	}
	if r.HooksPath() != ".githooks" {
		t.Errorf("HooksPath = %q", r.HooksPath())
	}
}
