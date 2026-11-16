package install

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	optMise   = "/opt/mise"
	makeWhere = optMise + " where make@" + MakeVersion
	makeInst  = optMise + " install make@" + MakeVersion
)

// fakeMakeInstall returns a directory laid out like mise's install of make, with an executable bin/make.
func fakeMakeInstall(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "bin", "make")
	writeFile(t, target, "#!/bin/sh\n")
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// newMakeTest returns an installer on a machine with mise at /opt/mise and without make; `mise where` prints where.
func newMakeTest(t *testing.T, where string) (*installer, *fakeRun) {
	t.Helper()
	archive := tarGz(t, map[string]string{"mise/bin/mise": fakeMise})
	fr := &fakeRun{out: map[string]string{makeWhere: where + "\n"}, fail: map[string]error{}}
	in := newTest(t, serve(t, &release{archive: archive}), archive, fr)
	in.misePath = func() string { return optMise }
	in.makePath = func() string { return "" }
	return in, fr
}

func TestInstallsMakeThroughDownloadedMise(t *testing.T) {
	archive := tarGz(t, map[string]string{"mise/bin/mise": fakeMise})
	rel := &release{archive: archive}
	install := fakeMakeInstall(t)
	in := newTest(t, serve(t, rel), archive, nil)
	mise := filepath.Join(in.binDir, "mise")
	fr := &fakeRun{out: map[string]string{mise + " where make@" + MakeVersion: install + "\n"}}
	in.run = fr.run
	in.makePath = func() string { return "" }
	p := newProj(t, map[string]string{"run.sh": "#!/bin/sh\n"})
	res := in.install(t.Context(), p, Options{})

	if stepNames(res) != "mise,make,tools of mise.toml" || res.Failed() || rel.hits != 1 {
		t.Fatalf("steps %+v, %d downloads", res.Steps, rel.hits)
	}
	got := make([]string, 0, len(fr.calls))
	for _, c := range fr.calls {
		if c.dir != p.Root || !slices.Equal(c.env, []string{"PATH=/usr/bin", "MISE_YES=1"}) {
			t.Errorf("%s ran in %s with %v", c.argv, c.dir, c.env)
		}
		got = append(got, c.argv)
	}
	want := []string{mise + " install make@4.4.1", mise + " where make@4.4.1"}
	if !slices.Equal(got, want) {
		t.Fatalf("calls %v", got)
	}
	link, target := filepath.Join(in.binDir, "make"), filepath.Join(install, "bin", "make")
	if dest, err := os.Readlink(link); err != nil || dest != target {
		t.Fatalf("link %s -> %q, %v", link, dest, err)
	}
	note := res.Steps[1].Note
	if !strings.HasPrefix(note, "installed "+link+" -> "+target+"; add "+in.binDir+" to PATH") {
		t.Errorf("note %q", note)
	}

	in.misePath = func() string { return mise }
	res = in.install(t.Context(), p, Options{})
	if res.Steps[1].Skipped != "already at "+link || !strings.HasPrefix(res.Steps[0].Skipped, "the project has no mise.toml") {
		t.Errorf("second run: %+v", res.Steps)
	}
}

func TestMakeNoteOnPath(t *testing.T) {
	in, _ := newMakeTest(t, fakeMakeInstall(t))
	in.pathVar = in.binDir
	res := in.install(t.Context(), newProj(t, nil), Options{})
	if s := res.Steps[1]; s.Err != nil || strings.Contains(s.Note, "add ") || !strings.HasPrefix(s.Note, "installed ") {
		t.Errorf("make step %+v", s)
	}
}

func TestMakeSkips(t *testing.T) {
	in, fr := newMakeTest(t, "")
	in.makePath = func() string { return "/usr/bin/make" }
	res := in.install(t.Context(), newProj(t, nil), Options{})
	if res.Steps[1].Skipped != "already at /usr/bin/make" {
		t.Errorf("on PATH: %+v", res.Steps[1])
	}

	in.makePath = func() string { return "" }
	local := filepath.Join(in.binDir, "make")
	writeFile(t, local, "")
	if err := os.Chmod(local, 0o755); err != nil {
		t.Fatal(err)
	}
	res = in.install(t.Context(), newProj(t, nil), Options{})
	if res.Steps[1].Skipped != "already at "+local || len(fr.calls) != 0 {
		t.Errorf("local bin: %+v, %v", res.Steps[1], fr.calls)
	}

	archive := tarGz(t, map[string]string{"mise/bin/mise": fakeMise})
	rel := &release{archive: archive}
	none := newTest(t, serve(t, rel), archive, &fakeRun{})
	none.makePath = func() string { return "" }
	res = none.install(t.Context(), newProj(t, nil), Options{SkipMake: true})
	if res.Steps[1].Skipped != "skipped on request" || !strings.HasPrefix(res.Steps[0].Skipped, "the project has no") || rel.hits != 0 {
		t.Errorf("SkipMake: %+v, %d downloads", res.Steps, rel.hits)
	}
}

func TestMakeFailures(t *testing.T) {
	cases := map[string]struct {
		where string
		edit  func(*installer, *fakeRun)
		want  string
	}{
		"no bin/make": {t.TempDir(), nil, "is not an executable file"},
		"empty where": {"", nil, "printed no directory"},
		"install fails": {"", func(_ *installer, fr *fakeRun) { fr.fail[makeInst] = errors.New("exit 1") },
			"install make: exit 1"},
		"where fails": {"", func(_ *installer, fr *fakeRun) { fr.fail[makeWhere] = errors.New("exit 2") },
			"find the installed make: exit 2"},
		"dir is file": {fakeMakeInstall(t), func(in *installer, _ *fakeRun) { writeFile(t, in.binDir, "x") }, "create "},
		"no home":     {"", func(in *installer, _ *fakeRun) { in.binDir = ""; t.Setenv("HOME", "") }, "no home directory"},
	}
	for name, c := range cases {
		in, fr := newMakeTest(t, c.where)
		if c.edit != nil {
			c.edit(in, fr)
		}
		res := in.install(t.Context(), newProj(t, nil), Options{})
		s := res.Steps[1]
		if s.Err == nil || !strings.Contains(s.Err.Error(), c.want) {
			t.Errorf("%s: %+v, want %q", name, s, c.want)
		}
		if name == "install fails" && (strings.Join(s.Command, " ") != makeInst || len(fr.calls) != 1) {
			t.Errorf("%s: command %v, calls %v", name, s.Command, fr.calls)
		}
	}
}

func TestMakeLinkNeverReplaces(t *testing.T) {
	in, _ := newMakeTest(t, fakeMakeInstall(t))
	link := filepath.Join(in.binDir, "make")
	writeFile(t, link, "mine")
	res := in.install(t.Context(), newProj(t, nil), Options{})
	if res.Steps[1].Skipped != "already at "+link || res.Steps[1].Err != nil {
		t.Errorf("make step %+v", res.Steps[1])
	}
	if body, err := os.ReadFile(link); err != nil || string(body) != "mine" {
		t.Errorf("replaced: %q, %v", body, err)
	}
}

func TestMakeWithoutMise(t *testing.T) {
	in, fr := newMakeTest(t, "")
	in.misePath = func() string { return "" }
	in.osRelease = func() string { return "ID=ubuntu\nID_LIKE=debian\n" }
	res := in.install(t.Context(), newProj(t, nil), Options{SkipMise: true})
	if err := res.Steps[1].Err; err == nil || !strings.Contains(err.Error(), "sudo apt-get install make") || len(fr.calls) != 0 {
		t.Errorf("skip mise: %v, %v", err, fr.calls)
	}

	in.baseURL = "http://127.0.0.1:1"
	res = in.install(t.Context(), newProj(t, nil), Options{})
	if res.Steps[0].Err == nil || res.Steps[1].Err == nil || !strings.Contains(res.Steps[1].Err.Error(), "no mise") {
		t.Errorf("mise failed: %+v", res.Steps)
	}
}

func TestMakeHint(t *testing.T) {
	cases := []struct{ goos, osRelease, want string }{
		{"darwin", "", "`xcode-select --install`"},
		{"linux", "ID=debian\n", "`sudo apt-get install make`"},
		{"linux", "NAME=\"Linux Mint\"\nID=linuxmint\nID_LIKE=\"ubuntu debian\"\n", "`sudo apt-get install make`"},
		{"linux", "ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n", "`sudo dnf install make`"},
		{"linux", "ID=fedora\n", "`sudo dnf install make`"},
		{"linux", "ID=alpine\n", "`apk add make`"},
		{"linux", "ID=manjaro\nID_LIKE=arch\n", "`sudo pacman -S make`"},
		{"linux", "ID=nixos\n", "Debian/Ubuntu `sudo apt-get install make`, Fedora/RHEL `sudo dnf install make`, " +
			"Alpine `apk add make`, Arch `sudo pacman -S make`"},
		{"freebsd", "", "the system package manager: "},
	}
	for _, c := range cases {
		if got := makeHint(c.goos, c.osRelease); !strings.Contains(got, c.want) {
			t.Errorf("%s %q: %q, want %q", c.goos, c.osRelease, got, c.want)
		}
	}
}
