package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rojosinalma/pawl/internal/project"
)

const fakeMise = "#!/bin/sh\necho mise 2026.11.9\n"

// tarGz builds a gzipped tar with the members.
func tarGz(t *testing.T, members map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "mise/bin/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	for name, body := range members {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

// release serves the archive at the linux-x64 asset path and counts the requests.
type release struct {
	archive []byte
	status  int
	mu      sync.Mutex
	hits    int
}

func (r *release) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.hits++
	r.mu.Unlock()
	if r.status != 0 {
		w.WriteHeader(r.status)
		return
	}
	if req.URL.Path != "/"+miseVersion+"/mise-"+miseVersion+"-linux-x64.tar.gz" {
		http.NotFound(w, req)
		return
	}
	_, _ = w.Write(r.archive)
}

// call is one command the fake runner saw.
type call struct {
	dir  string
	env  []string
	argv string
}

// fakeRun records the commands, prints the output listed (else "ran <command>") and fails the ones listed.
type fakeRun struct {
	calls []call
	fail  map[string]error
	out   map[string]string
}

func (f *fakeRun) run(_ context.Context, c command, stdout, _ io.Writer) error {
	line := strings.Join(c.argv, " ")
	f.calls = append(f.calls, call{dir: c.dir, env: c.env, argv: line})
	if out, ok := f.out[line]; ok {
		_, _ = fmt.Fprint(stdout, out)
	} else {
		_, _ = fmt.Fprintln(stdout, "ran", line)
	}
	return f.fail[line]
}

// newTest returns an installer that downloads from srv, installs into a temp dir and runs through fr.
func newTest(t *testing.T, srv *httptest.Server, archive []byte, fr *fakeRun) *installer {
	t.Helper()
	in := newInstaller()
	in.baseURL = srv.URL
	in.client = srv.Client()
	in.binDir = filepath.Join(t.TempDir(), "bin")
	in.goos, in.goarch = "linux", "amd64"
	in.sums = map[string]string{"linux-x64": sum(archive)}
	in.pathVar = "/usr/bin"
	in.baseEnv = []string{"PATH=/usr/bin"}
	in.misePath = func() string { return "" }
	in.makePath = func() string { return "/usr/bin/make" }
	in.osRelease = func() string { return "" }
	in.environ = func(context.Context, string) ([]string, error) { return []string{"PATH=/mise/tools:/usr/bin"}, nil }
	in.run = fr.run
	return in
}

// newProj writes files into a temp dir and detects the project.
func newProj(t *testing.T, files map[string]string) *project.Project {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := project.Detect(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func serve(t *testing.T, r *release) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func TestInstallsMiseThenToolsThenStack(t *testing.T) {
	archive := tarGz(t, map[string]string{"mise/README": "x", "mise/bin/mise": fakeMise})
	rel := &release{archive: archive}
	srv := serve(t, rel)
	fr := &fakeRun{fail: map[string]error{"bundle install": errors.New("exit 1")}}
	in := newTest(t, srv, archive, fr)
	p := newProj(t, map[string]string{"mise.toml": "[tools]\n", "Gemfile": "\n", "Cargo.toml": "[package]\n"})
	var out bytes.Buffer
	res := in.install(t.Context(), p, Options{Stdout: &out})

	mise := filepath.Join(in.binDir, "mise")
	checkInstalled(t, mise)
	names := stepNames(res)
	if names != "mise,make,tools of mise.toml,gems,rust components" {
		t.Fatalf("steps = %s", names)
	}
	if res.Steps[0].Err != nil || !strings.Contains(res.Steps[0].Note, "add "+in.binDir+" to PATH") {
		t.Errorf("mise step: %+v", res.Steps[0])
	}
	if len(fr.calls) != 3 || fr.calls[0].argv != mise+" install --yes" {
		t.Fatalf("calls = %+v", fr.calls)
	}
	tools := strings.Join(fr.calls[0].env, " ")
	if fr.calls[0].dir != p.Root || tools != "PATH=/usr/bin MISE_TRUSTED_CONFIG_PATHS="+p.Root+" MISE_YES=1" {
		t.Errorf("mise install ran in %s with %s", fr.calls[0].dir, tools)
	}
	for _, c := range fr.calls[1:] {
		if c.dir != p.Root || strings.Join(c.env, " ") != "PATH=/mise/tools:/usr/bin" {
			t.Errorf("%s ran in %s with %v", c.argv, c.dir, c.env)
		}
	}
	if res.Steps[1].Skipped != "already at /usr/bin/make" || res.Steps[3].Err == nil || res.Steps[4].Err != nil || !res.Failed() {
		t.Errorf("a failing gems step does not stop the rust one: %+v", res.Steps)
	}
	if !strings.Contains(out.String(), "ran rustup component add") {
		t.Errorf("output not streamed: %q", out.String())
	}
	checkSecondRun(t, in, p, rel, mise)
}

// checkSecondRun: the second run finds the installed mise and downloads nothing.
func checkSecondRun(t *testing.T, in *installer, p *project.Project, rel *release, mise string) {
	t.Helper()
	in.misePath = func() string { return mise }
	res := in.install(t.Context(), p, Options{})
	if res.Steps[0].Skipped != "already at "+mise || rel.hits != 1 {
		t.Errorf("second run: %+v, %d downloads", res.Steps[0], rel.hits)
	}
}

func TestNoteOnlyWhenNotOnPath(t *testing.T) {
	archive := tarGz(t, map[string]string{"mise/bin/mise": fakeMise})
	in := newTest(t, serve(t, &release{archive: archive}), archive, &fakeRun{})
	in.pathVar = "/usr/bin:" + in.binDir
	res := in.install(t.Context(), newProj(t, map[string]string{".tool-versions": ""}), Options{})
	if res.Steps[0].Err != nil || res.Steps[0].Note != "" {
		t.Errorf("mise step: %+v", res.Steps[0])
	}
}

func TestDownloadFailures(t *testing.T) {
	archive := tarGz(t, map[string]string{"mise/bin/mise": fakeMise})
	noMember := tarGz(t, map[string]string{"mise/bin/other": "x"})
	cases := map[string]struct {
		rel  *release
		edit func(*installer)
		want string
	}{
		"wrong hash": {&release{archive: archive}, func(in *installer) { in.sums["linux-x64"] = strings.Repeat("0", 64) },
			"SHA-256 is " + sum(archive) + ", the pin is " + strings.Repeat("0", 64)},
		"404":         {&release{status: http.StatusNotFound}, nil, "404 Not Found"},
		"platform":    {&release{archive: archive}, func(in *installer) { in.goos = "windows" }, "no pinned mise build for windows/amd64; install mise yourself"},
		"arch":        {&release{archive: archive}, func(in *installer) { in.goarch = "386" }, "no pinned mise build for linux/386"},
		"bad URL":     {&release{archive: archive}, func(in *installer) { in.baseURL = "file:///x" }, "is not an http(s) URL"},
		"no server":   {&release{archive: archive}, func(in *installer) { in.baseURL = "http://127.0.0.1:1" }, "download mise-"},
		"not gzip":    {&release{archive: []byte("plain")}, func(in *installer) { in.sums["linux-x64"] = sum([]byte("plain")) }, "not a gzip archive"},
		"no member":   {&release{archive: noMember}, func(in *installer) { in.sums["linux-x64"] = sum(noMember) }, "no mise/bin/mise in the archive"},
		"exists":      {&release{archive: archive}, func(in *installer) { writeFile(t, filepath.Join(in.binDir, "mise"), "old") }, "exists but is not an executable mise"},
		"dir is file": {&release{archive: archive}, func(in *installer) { writeFile(t, in.binDir, "x") }, "create "},
	}
	for name, c := range cases {
		fr := &fakeRun{}
		in := newTest(t, serve(t, c.rel), archive, fr)
		if c.edit != nil {
			c.edit(in)
		}
		res := in.install(t.Context(), newProj(t, map[string]string{"mise.toml": ""}), Options{})
		if err := res.Steps[0].Err; err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
		if res.Steps[2].Skipped != "mise did not install" || len(fr.calls) != 0 {
			t.Errorf("%s: tools step %+v, calls %v", name, res.Steps[2], fr.calls)
		}
		if name != "exists" && name != "dir is file" {
			if _, err := os.Stat(filepath.Join(in.binDir, "mise")); err == nil {
				t.Errorf("%s: a mise was written", name)
			}
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkips(t *testing.T) {
	archive := tarGz(t, map[string]string{"mise/bin/mise": fakeMise})
	rel := &release{archive: archive}
	srv := serve(t, rel)

	fr := &fakeRun{}
	in := newTest(t, srv, archive, fr)
	res := in.install(t.Context(), newProj(t, map[string]string{"mise.toml": "", "Gemfile": ""}), Options{SkipMise: true})
	if res.Steps[0].Skipped != "skipped on request" || res.Steps[2].Skipped != "mise is not installed" {
		t.Errorf("skip mise: %+v", res.Steps)
	}
	if len(fr.calls) != 1 || fr.calls[0].argv != "bundle install" {
		t.Errorf("only the stack runs: %+v", fr.calls)
	}

	in.misePath = func() string { return "/opt/mise" }
	fr.calls = nil
	res = in.install(t.Context(), newProj(t, map[string]string{"mise.toml": ""}), Options{SkipMise: true})
	if res.Steps[2].Err != nil || len(fr.calls) != 1 || fr.calls[0].argv != "/opt/mise install --yes" {
		t.Errorf("an existing mise still installs the tools: %+v, %+v", res.Steps, fr.calls)
	}

	fr.calls = nil
	res = in.install(t.Context(), newProj(t, map[string]string{"go.mod": "module x\n"}), Options{})
	if !strings.HasPrefix(res.Steps[0].Skipped, "the project has no mise.toml") || res.Steps[2].Skipped != "no mise configuration" {
		t.Errorf("no config: %+v", res.Steps)
	}
	if len(fr.calls) != 2 || !strings.HasPrefix(fr.calls[0].argv, "go get -tool ") || fr.calls[1].argv != "go mod tidy" || rel.hits != 0 {
		t.Errorf("calls %+v, %d downloads", fr.calls, rel.hits)
	}
}

func TestMissingProgram(t *testing.T) {
	archive := tarGz(t, map[string]string{"mise/bin/mise": fakeMise})
	in := newTest(t, serve(t, &release{archive: archive}), archive, &fakeRun{})
	in.run = newInstaller().run // the real runner, on a PATH without bundle
	in.environ = func(context.Context, string) ([]string, error) { return nil, errors.New("no env") }
	in.baseEnv = []string{"PATH=" + t.TempDir()}
	res := in.install(t.Context(), newProj(t, map[string]string{"Gemfile": ""}), Options{})
	last := res.Steps[len(res.Steps)-1]
	if last.Err == nil || !strings.HasPrefix(last.Err.Error(), "bundle is not installed") || !errors.Is(last.Err, exec.ErrNotFound) {
		t.Errorf("missing program: %v", last.Err)
	}
}

func TestRun(t *testing.T) {
	if _, err := Run(t.Context(), nil, Options{}); err == nil {
		t.Error("nil project")
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	shell := newProj(t, map[string]string{"run.sh": "#!/bin/sh\n"})
	res, err := Run(t.Context(), shell, Options{SkipMake: true})
	if err != nil || stepNames(res) != "mise,make,tools of mise.toml" || res.Failed() {
		t.Errorf("shell project: %v, %+v", err, res)
	}
	res, _ = Run(t.Context(), shell, Options{SkipMise: true})
	if err := res.Steps[1].Err; err == nil || !strings.Contains(err.Error(), "install it with") {
		t.Errorf("no make, no mise: %+v", res.Steps[1])
	}
}

func TestOversizedRead(t *testing.T) {
	if _, err := readCapped(io.LimitReader(zeros{}, maxDownload+10), "big"); err == nil || !strings.Contains(err.Error(), "larger than 300 MB") {
		t.Errorf("cap: %v", err)
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func stepNames(r *Result) string {
	var out []string
	for _, s := range r.Steps {
		out = append(out, s.Name)
	}
	return strings.Join(out, ",")
}

// checkInstalled runs the installed mise, which proves it is in place and executable.
func checkInstalled(t *testing.T, mise string) {
	t.Helper()
	got, err := exec.CommandContext(t.Context(), mise).Output()
	if err != nil || string(got) != "mise 2026.11.9\n" {
		t.Fatalf("installed mise: %q, %v", got, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(mise)); len(entries) != 1 {
		t.Errorf("temporary files left: %v", entries)
	}
}
