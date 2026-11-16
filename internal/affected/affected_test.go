package affected

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rojosinalma/pawl/internal/gitx"
	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/stack"
)

type reply struct {
	out string
	err error
}

type fakeRunner map[string]reply

func (f fakeRunner) Output(_, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	r, ok := f[key]
	if !ok {
		return nil, fmt.Errorf("unexpected command %q", key)
	}
	return []byte(r.out), r.err
}

func fakeProject(root string, files ...string) *project.Project {
	m := manifest.Defaults()
	return &project.Project{Root: root, Manifest: &m, Files: files, Params: stack.Params{Facts: map[string]string{}}}
}

func decideFor(t *testing.T, p *project.Project, name string, run runner, changed ...string) string {
	t.Helper()
	st := strategies()[name]
	return decide(p, st, run, changed, st.everything(p))
}

const goList = `{"ImportPath":"ex.com/m","Dir":"/r","Imports":["ex.com/m/internal/b"]}
{"ImportPath":"ex.com/m/internal/a","Dir":"/r/internal/a","Imports":["fmt"]}
{"ImportPath":"ex.com/m/internal/b","Dir":"/r/internal/b","Imports":["ex.com/m/internal/a"]}
{"ImportPath":"ex.com/m/internal/c","Dir":"/r/internal/c","TestImports":["ex.com/m/internal/b"]}
{"ImportPath":"ex.com/m/cmd/x","Dir":"/r/cmd/x","XTestImports":["ex.com/m/internal/c"]}
{"ImportPath":"ex.com/m/internal/d","Dir":"/r/internal/d"}
`

func TestGoClosure(t *testing.T) {
	p := fakeProject("/r")
	run := fakeRunner{"go list -json ./...": {out: goList}}
	cases := []struct {
		changed []string
		want    string
	}{
		{[]string{"internal/a/a.go"}, ". ./cmd/x ./internal/a ./internal/b ./internal/c"},
		{[]string{"internal/c/c_test.go"}, "./cmd/x ./internal/c"},
		{[]string{"internal/d/testdata/in.txt"}, "./internal/d"},
		{[]string{"internal/d/gen/tool.go"}, "./internal/d"},
		{[]string{"README.md"}, "-run ^$ ./..."},
		{[]string{"docs/x.go"}, "."},
	}
	for _, tc := range cases {
		if got := decideFor(t, p, "go", run, tc.changed...); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.changed, got, tc.want)
		}
	}
}

func TestGoFailures(t *testing.T) {
	p := fakeProject("/r")
	if got := decideFor(t, p, "go", fakeRunner{}, "a.go"); got != "./..." {
		t.Fatalf("go missing: %q", got)
	}
	if got := decideFor(t, p, "go", fakeRunner{"go list -json ./...": {out: "{bad"}}, "a.go"); got != "./..." {
		t.Fatalf("bad json: %q", got)
	}
	if got := decideFor(t, p, "go", fakeRunner{"go list -json ./...": {out: `{"ImportPath":"x","Dir":"/elsewhere"}`}}, "a.go"); got != "-run ^$ ./..." {
		t.Fatalf("no package: %q", got)
	}
}

const cargoMeta = `{"packages":[
 {"name":"core","manifest_path":"/r/crates/core/Cargo.toml","dependencies":[{"name":"serde"}]},
 {"name":"api","manifest_path":"/r/crates/api/Cargo.toml","dependencies":[{"name":"core","path":"/r/crates/core"}]},
 {"name":"cli","manifest_path":"/r/Cargo.toml","dependencies":[{"name":"api","path":"/r/crates/api"}]},
 {"name":"tool","manifest_path":"/r/crates/tool/Cargo.toml","dependencies":[]}
]}`

func TestRust(t *testing.T) {
	p := fakeProject("/r")
	run := fakeRunner{"cargo metadata --format-version 1 --no-deps": {out: cargoMeta}}
	cases := []struct {
		changed []string
		want    string
	}{
		{[]string{"crates/core/src/lib.rs"}, "-p api -p cli -p core"},
		{[]string{"crates/tool/src/main.rs", "README.md"}, "-p tool"},
		{[]string{"src/main.rs"}, "-p cli"},
		{[]string{"docs/readme.md"}, "--workspace --no-run"},
	}
	for _, tc := range cases {
		if got := decideFor(t, p, "rust", run, tc.changed...); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.changed, got, tc.want)
		}
	}
	single := fakeRunner{"cargo metadata --format-version 1 --no-deps": {out: `{"packages":[{"name":"one","manifest_path":"/r/Cargo.toml"}]}`}}
	if got := decideFor(t, p, "rust", single, "src/lib.rs"); got != "--workspace" {
		t.Errorf("single crate: %q", got)
	}
	if got := decideFor(t, p, "rust", fakeRunner{}, "src/lib.rs"); got != "--workspace" {
		t.Errorf("cargo missing: %q", got)
	}
	outside := fakeRunner{"cargo metadata --format-version 1 --no-deps": {out: `{"packages":[{"name":"a","manifest_path":"/r/a/Cargo.toml"},{"name":"b","manifest_path":"/r/b/Cargo.toml"}]}`}}
	if got := decideFor(t, p, "rust", outside, "xtask/main.rs"); got != "--workspace" {
		t.Errorf("file outside members: %q", got)
	}
}

func TestPython(t *testing.T) {
	p := fakeProject("/r", "pkg/foo.py", "tests/test_foo.py", "tests/bar_test.py", "pkg/bar.py", "pkg/lonely.py", "tests/test_x.py")
	cases := []struct {
		changed []string
		want    string
	}{
		{[]string{"pkg/foo.py"}, "tests/test_foo.py"},
		{[]string{"pkg/bar.py", "tests/test_x.py"}, "tests/bar_test.py tests/test_x.py"},
		{[]string{"pkg/lonely.py"}, ""},
		{[]string{"tests/conftest.py", "tests/test_x.py"}, ""},
		{[]string{"tests/helpers.py"}, ""},
		{[]string{"README.md"}, "--collect-only -q"},
		{[]string{"tests/test_gone.py"}, "--collect-only -q"},
	}
	for _, tc := range cases {
		if got := decideFor(t, p, "python", nil, tc.changed...); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.changed, got, tc.want)
		}
	}
}

func TestRuby(t *testing.T) {
	p := fakeProject("/r", "lib/foo.rb", "test/foo_test.rb", "test/bar_test.rb", "test/test_helper.rb", "spec/foo_spec.rb")
	if got := decideFor(t, p, "ruby", nil, "lib/foo.rb"); got != "spec/foo_spec.rb test/foo_test.rb" {
		t.Errorf("mapped: %q", got)
	}
	if got := decideFor(t, p, "ruby", nil, "test/test_helper.rb"); got != "test/bar_test.rb test/foo_test.rb" {
		t.Errorf("helper runs everything: %q", got)
	}
	if got := decideFor(t, p, "ruby", nil, "notes.txt"); got != "" {
		t.Errorf("nothing: %q", got)
	}
	p.Params.Facts["test_framework"] = "rspec"
	if got := rubyEverything(p); got != "spec/foo_spec.rb" {
		t.Errorf("rspec everything: %q", got)
	}
}

func TestTypescript(t *testing.T) {
	p := fakeProject("/r", "src/foo.ts", "src/foo.test.ts", "src/foo.spec.ts", "src/view.tsx", "src/view.test.tsx", "src/types.d.ts")
	if got := decideFor(t, p, "typescript", nil, "src/foo.ts"); got != "src/foo.spec.ts src/foo.test.ts" {
		t.Errorf("foo: %q", got)
	}
	if got := decideFor(t, p, "typescript", nil, "src/view.tsx"); got != "src/view.test.tsx" {
		t.Errorf("view: %q", got)
	}
	if got := decideFor(t, p, "shell", nil, "x.sh"); got != "" {
		t.Errorf("shell: %q", got)
	}
}

func TestGlobalFiles(t *testing.T) {
	p := fakeProject("/r")
	for _, f := range []string{"pawl.toml", "Makefile", "sub/GNUmakefile", ".pawl/baseline.json", ".github/workflows/check.yml",
		"mise.toml", ".tool-versions", "go.sum", "web/package.json", "Gemfile.lock", ".golangci.yml", "pytest.ini"} {
		if got := decideFor(t, p, "go", fakeRunner{}, "internal/a/a.go", f); got != "./..." {
			t.Errorf("%s: got %q", f, got)
		}
	}
	if isGlobal("sub/.golangci.yml") {
		t.Error("a nested tool configuration is not global")
	}
}

func TestUnknownStack(t *testing.T) {
	if _, err := Scope(fakeProject("/r"), "cobol"); err == nil || !strings.Contains(err.Error(), "go") {
		t.Fatalf("want error listing stacks, got %v", err)
	}
}

func TestScopeWithoutRepo(t *testing.T) {
	if got, err := Scope(fakeProject("/r"), "rust"); err != nil || got != "--workspace" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repoProject(t *testing.T) *project.Project {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	write(t, root, "pkg/foo.py", "x = 1\n")
	write(t, root, "pkg/old.py", "y = 1\n")
	write(t, root, "tests/test_foo.py", "def test_x(): pass\n")
	write(t, root, "tests/test_new.py", "def test_y(): pass\n")
	git(t, root, "add", "pkg", "tests/test_foo.py")
	git(t, root, "commit", "-q", "-m", "init")
	p := fakeProject(root, "pkg/foo.py", "tests/test_foo.py", "tests/test_new.py")
	p.Repo = &gitx.Repo{Root: root}
	p.Manifest.Base = "main"
	return p
}

func TestScopeInRepo(t *testing.T) {
	p := repoProject(t)
	run := execRunner{timeout: 30 * time.Second}
	got, err := scope(p, "python", run)
	if err != nil || got != "tests/test_new.py" {
		t.Fatalf("untracked test: %q, %v", got, err)
	}
	write(t, p.Root, "pkg/foo.py", "x = 2\n")
	if err := os.Remove(filepath.Join(p.Root, "pkg/old.py")); err != nil {
		t.Fatal(err)
	}
	got, err = scope(p, "python", run)
	if err != nil || got != "tests/test_foo.py tests/test_new.py" {
		t.Fatalf("changed and deleted sources: %q, %v", got, err)
	}
	t.Setenv("PAWL_FULL", "1")
	if got, _ := scope(p, "go", run); got != "./..." {
		t.Fatalf("PAWL_FULL: %q", got)
	}
}

func TestScopeEmptyChangeAndNoBase(t *testing.T) {
	p := repoProject(t)
	p.Files = []string{"pkg/foo.py", "tests/test_foo.py"}
	run := execRunner{timeout: 30 * time.Second}
	if got, _ := scope(p, "rust", run); got != "--workspace" {
		t.Fatalf("empty change: %q", got)
	}
	p.Manifest.Base = "origin/nowhere"
	if got, _ := scope(p, "go", run); got != "./..." {
		t.Fatalf("no base: %q", got)
	}
	p.Manifest.Base = "main"
	if got, _ := scope(p, "go", fakeRunner{}); got != "./..." {
		t.Fatalf("git failure: %q", got)
	}
}

func TestChangesStatusFailure(t *testing.T) {
	p := repoProject(t)
	base := p.BaseRef()
	run := fakeRunner{"git diff --name-only --diff-filter=D -z " + base: {}}
	if _, ok := changes(p, base, run); ok {
		t.Fatal("want failure when git status fails")
	}
	p.Repo = &gitx.Repo{Root: t.TempDir()}
	if _, ok := changes(p, base, run); ok {
		t.Fatal("want failure outside a repository")
	}
}

func TestRelToSymlink(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	resolved, _ := filepath.EvalSymlinks(target)
	if rel, ok := relTo(link, filepath.Join(resolved, "a")); !ok || rel != "a" {
		t.Fatalf("got %q %v", rel, ok)
	}
	if _, ok := relTo("/r", "/x"); ok {
		t.Fatal("outside root")
	}
}
