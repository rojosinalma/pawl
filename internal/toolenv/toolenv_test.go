package toolenv

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rojosinalma/pawl/internal/runner/runnertest"
)

// writeExe writes an executable shell script.
func writeExe(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func miseProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mise.toml"), []byte("[tools]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestEnvironPrependsExistingDirsInOrder(t *testing.T) {
	root := miseProject(t)
	a, b := t.TempDir(), t.TempDir()
	r := runnertest.Script{"/m bin-paths": {Out: a + "\n/no/such/dir\n\n" + b + "\n"}}
	got := environ(t.Context(), r, "/m", []string{"HOME=/h", "PATH=/usr/bin"}, root)
	want := []string{"HOME=/h", "PATH=" + a + ":" + b + ":/usr/bin"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEnvironUnchanged(t *testing.T) {
	base := []string{"PATH=/usr/bin"}
	dir := t.TempDir()
	ok := runnertest.Script{"/m bin-paths": {Out: dir}}
	failing := runnertest.Script{"/m bin-paths": {Err: errors.New("boom")}}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	cases := map[string]struct {
		ctx  context.Context
		r    runnertest.Script
		mise string
		root string
	}{
		"no config":   {t.Context(), ok, "/m", t.TempDir()},
		"no mise":     {t.Context(), ok, "", miseProject(t)},
		"failing":     {t.Context(), failing, "/m", miseProject(t)},
		"cancelled":   {cancelled, ok, "/m", miseProject(t)},
		"no dirs out": {t.Context(), runnertest.Script{"/m bin-paths": {Out: "\n"}}, "/m", miseProject(t)},
	}
	for name, c := range cases {
		if got := environ(c.ctx, c.r, c.mise, base, c.root); !slices.Equal(got, base) {
			t.Errorf("%s: got %v", name, got)
		}
	}
}

func TestEnvironRealMise(t *testing.T) {
	root := miseProject(t)
	tools := t.TempDir()
	bin := t.TempDir()
	// the fake mise prints the tool directory only when pawl passed the trust variables
	writeExe(t, filepath.Join(bin, "mise"), `[ "$MISE_YES" = 1 ] && [ "$MISE_TRUSTED_CONFIG_PATHS" = "`+root+`" ] && echo `+tools)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("HOME", t.TempDir())
	env, err := Environ(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got := pathOf(env); got != tools+":"+bin+":/usr/bin:/bin" {
		t.Errorf("PATH = %q", got)
	}
}

func TestMisePathPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	if got := MisePath(); got != "" {
		t.Errorf("nothing installed: %q", got)
	}
	local := filepath.Join(home, ".local", "bin", "mise")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := MisePath(); got != "" {
		t.Errorf("not executable: %q", got)
	}
	writeExe(t, local, "")
	if got := MisePath(); got != local {
		t.Errorf("installed: %q", got)
	}
	onPath := t.TempDir()
	writeExe(t, filepath.Join(onPath, "mise"), "")
	t.Setenv("PATH", onPath)
	if got := MisePath(); got != filepath.Join(onPath, "mise") {
		t.Errorf("on PATH first: %q", got)
	}
	t.Setenv("HOME", "")
	t.Setenv("PATH", "")
	if got := MisePath(); got != "" {
		t.Errorf("no home: %q", got)
	}
}

func TestHasConfig(t *testing.T) {
	for _, f := range ConfigFiles {
		root := t.TempDir()
		if HasConfig(root) {
			t.Fatal("empty dir")
		}
		if err := os.WriteFile(filepath.Join(root, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if !HasConfig(root) {
			t.Errorf("%s not seen", f)
		}
	}
}

func TestPrependPath(t *testing.T) {
	if got := PrependPath([]string{"A=1"}, []string{"/x"}); !slices.Equal(got, []string{"A=1", "PATH=/x"}) {
		t.Errorf("no PATH: %v", got)
	}
	if got := PrependPath([]string{"PATH="}, []string{"/x", "/y"}); !slices.Equal(got, []string{"PATH=/x:/y"}) {
		t.Errorf("empty PATH: %v", got)
	}
	base := []string{"PATH=/b"}
	if got := PrependPath(base, nil); !slices.Equal(got, base) {
		t.Errorf("no dirs: %v", got)
	}
}

func TestLookPathUsesTheGivenPath(t *testing.T) {
	dir := t.TempDir()
	writeExe(t, filepath.Join(dir, "only-here"), "")
	env := []string{"PATH=/first", "PATH=:" + dir}
	if got, err := LookPath(env, "only-here"); err != nil || got != filepath.Join(dir, "only-here") {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := LookPath(env, "sh"); !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("sh is not on that PATH: %v", err)
	}
	if got, err := LookPath(nil, filepath.Join(dir, "only-here")); err != nil || got == "" {
		t.Errorf("with a slash: %q, %v", got, err)
	}
}

func TestExec(t *testing.T) {
	dir := t.TempDir()
	writeExe(t, filepath.Join(dir, "tool"), `echo "out $FOO $(pwd)"; echo err >&2; [ "$1" != fail ]`)
	e := Exec{Env: []string{"PATH=" + dir + ":/usr/bin:/bin", "FOO=bar"}, Timeout: 5 * time.Second}
	out, err := e.Output(dir, "tool")
	if err != nil || !strings.HasPrefix(string(out), "out bar ") {
		t.Errorf("Output = %q, %v", out, err)
	}
	if _, err := e.Output(dir, "tool", "fail"); err == nil || !strings.HasSuffix(err.Error(), ": err") {
		t.Errorf("failure carries stderr: %v", err)
	}
	e.IsCombined = true
	if out, _ := e.Output(dir, "tool"); !strings.Contains(string(out), "err") {
		t.Errorf("combined: %q", out)
	}
	if _, err := e.Output(dir, "missing-tool"); !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if err := e.Run(t.Context(), dir, &stdout, &stderr, "tool"); err != nil || stderr.String() != "err\n" {
		t.Errorf("Run: %v, %q", err, stderr.String())
	}
	if err := (Exec{Timeout: 5 * time.Second}).Run(t.Context(), dir, &stdout, &stderr, "sh", "-c", "exit 3"); err == nil {
		t.Error("exit 3 is a failure")
	}
}

func TestAppendPath(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct{ env, want []string }{
		"appended":    {[]string{"PATH=/a", "B=1"}, []string{"PATH=/a:" + dir, "B=1"}},
		"already on":  {[]string{"PATH=" + dir + ":/a"}, []string{"PATH=" + dir + ":/a"}},
		"no PATH":     {[]string{"B=1"}, []string{"B=1", "PATH=" + dir}},
		"last PATH":   {[]string{"PATH=/x", "PATH=/a"}, []string{"PATH=/x", "PATH=/a:" + dir}},
		"missing dir": {[]string{"PATH=/a"}, []string{"PATH=/a"}},
	}
	for name, c := range cases {
		d := dir
		if name == "missing dir" {
			d = filepath.Join(dir, "none")
		}
		if got := appendPath(c.env, d); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
}

func TestEnvironAppendsLocalBinOnce(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin")
	env, err := Environ(t.Context(), t.TempDir())
	if err != nil || pathOf(env) != "/usr/bin:"+local {
		t.Errorf("PATH = %q, %v", pathOf(env), err)
	}
	t.Setenv("PATH", local+":/usr/bin")
	if env, _ := Environ(t.Context(), t.TempDir()); pathOf(env) != local+":/usr/bin" {
		t.Errorf("already on PATH: %q", pathOf(env))
	}
}
