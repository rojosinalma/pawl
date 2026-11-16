package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// testEnv makes git usable in a bare test container.
var testEnv = []string{
	"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
	"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
}

// newProject writes the files into a temp dir; with git it is a repository.
func newProject(t *testing.T, git bool, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo")
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if git {
		runIn(t, dir, "git", "init", "-q", "-b", "main")
	}
	return dir
}

func runIn(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), testEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func mustInit(t *testing.T, dir string, opts Options) *Result {
	t.Helper()
	res, err := Init(dir, opts)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return res
}

func assertContains(t *testing.T, what, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("%s lacks %q:\n%s", what, sub, s)
		}
	}
}

func assertLacks(t *testing.T, what, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			t.Errorf("%s contains %q:\n%s", what, sub, s)
		}
	}
}

func hasItem(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func hasHint(res *Result, prefix string) bool {
	for _, h := range res.Hints {
		if strings.HasPrefix(h, prefix) {
			return true
		}
	}
	return false
}

var shaRef = regexp.MustCompile(`@[0-9a-f]{40}$`)

// checkWorkflow parses a workflow and asserts every uses: is pinned and no run: body interpolates an expression.
func checkWorkflow(t *testing.T, name, content string) {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatalf("%s does not parse: %v\n%s", name, err, content)
	}
	for _, key := range []string{"name", "on", "permissions", "jobs"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("%s has no %s", name, key)
		}
	}
	walk(doc, func(key string, v any) {
		s, _ := v.(string)
		switch key {
		case "uses":
			if !shaRef.MatchString(s) {
				t.Errorf("%s: uses %q is not pinned to a commit SHA", name, s)
			}
		case "run":
			if strings.Contains(s, "${{") {
				t.Errorf("%s: run body interpolates an expression: %q", name, s)
			}
		}
	})
}

func walk(v any, fn func(key string, v any)) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			fn(k, child)
			walk(child, fn)
		}
	case []any:
		for _, child := range x {
			walk(child, fn)
		}
	}
}

// execHook prepares the pre-commit hook with a fake pawl.
func execHook(dir, pawl string) *exec.Cmd {
	cmd := exec.Command("sh", hookPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PAWL="+pawl)
	return cmd
}

func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), testEnv...)
	return cmd
}

// listTree lists every path under dir with its size, .git/config included (core.hooksPath lives there).
func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out = append(out, rel+" "+info.Mode().String()+" "+strconv.FormatInt(info.Size(), 10))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
