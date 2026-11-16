package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// goRepo is a small Go repository with one commit on main.
func goRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/app\n\ngo 1.25\n")
	write(t, root, "main.go", "package main\n\nfunc main() {}\n")
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "first")
	return root
}

func pawl(t *testing.T, root string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Execute("test", append([]string{"-C", root}, args...), &out, &errOut)
	return out.String(), errOut.String(), code
}

// initRepo is goRepo after a successful `pawl init`.
func initRepo(t *testing.T) string {
	t.Helper()
	root := goRepo(t)
	if out, stderr, code := pawl(t, root, "init"); code != 0 {
		t.Fatalf("init failed (%d): %s%s", code, out, stderr)
	}
	return root
}

func TestInitReportsWhatItWroteAndRunsOnce(t *testing.T) {
	root := goRepo(t)
	out, stderr, code := pawl(t, root, "init")
	if code != 0 {
		t.Fatalf("init failed (%d): %s%s", code, out, stderr)
	}
	for _, want := range []string{"stacks: go", "created  pawl.toml", "created  Makefile", "rules are waived by default", "still to run:"} {
		if !strings.Contains(out, want) {
			t.Errorf("init output lacks %q:\n%s", want, out)
		}
	}
	if _, stderr, code := pawl(t, root, "init"); code != 1 || !strings.Contains(stderr, "pawl.toml exists") {
		t.Errorf("second init: code %d, stderr %q", code, stderr)
	}
}

func TestRulesListsShowsAndSyncs(t *testing.T) {
	root := initRepo(t)
	out, _, code := pawl(t, root, "rules")
	if code != 0 || !strings.Contains(out, "HYGIENE") || !strings.Contains(out, "code.format") || !strings.Contains(out, "make fmt-check") {
		t.Errorf("rules (%d):\n%s", code, out)
	}
	out, _, code = pawl(t, root, "rules", "--waived")
	if code != 0 || strings.Contains(out, "review.simplest") || !strings.Contains(out, "code.boundaries") {
		t.Errorf("rules --waived (%d):\n%s", code, out)
	}
	out, _, code = pawl(t, root, "rules", "show", "review.errors")
	if code != 0 || !strings.Contains(out, "source: ") || !strings.Contains(out, "here: waived") {
		t.Errorf("rules show (%d):\n%s", code, out)
	}
	if _, stderr, code := pawl(t, root, "rules", "show", "no.rule"); code != 1 || !strings.Contains(stderr, "no rule") {
		t.Errorf("rules show unknown: %d %q", code, stderr)
	}
	if out, _, code := pawl(t, root, "rules", "sync"); code != 0 || !strings.Contains(out, "up to date") {
		t.Errorf("rules sync (%d): %s", code, out)
	}
}

func TestBaselineThenCheckPassOnAFreshInit(t *testing.T) {
	root := initRepo(t)
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "--no-verify", "-m", "pawl init")
	out, stderr, code := pawl(t, root, "baseline")
	if code != 0 || !strings.Contains(out, "wrote .pawl/baseline.json") {
		t.Fatalf("baseline (%d): %s%s", code, out, stderr)
	}

	out, stderr, code = pawl(t, root, "check", "--only", "repo.targets,repo.hook,escape.suppressions,code.file-size")
	if code != 0 {
		t.Fatalf("check on a fresh init failed (%d):\n%s%s", code, out, stderr)
	}
	if !strings.Contains(out, "no findings") {
		t.Errorf("check output:\n%s", out)
	}
}

func TestCheckReportsFindingsAndFormats(t *testing.T) {
	root := goRepo(t)
	if _, stderr, code := pawl(t, root, "init"); code != 0 {
		t.Fatal(stderr)
	}
	if err := os.Remove(filepath.Join(root, ".githooks", "pre-commit")); err != nil {
		t.Fatal(err)
	}
	out, _, code := pawl(t, root, "check", "--only", "repo.hook")
	if code != 1 || !strings.Contains(out, "repo.hook") || !strings.Contains(out, "fix:") {
		t.Errorf("check with a missing hook (%d):\n%s", code, out)
	}
	out, _, code = pawl(t, root, "check", "--only", "repo.hook", "--format", "json")
	if code != 1 || !strings.Contains(out, `"rule": "repo.hook"`) {
		t.Errorf("json (%d):\n%s", code, out)
	}
	out, _, code = pawl(t, root, "check", "--only", "repo.hook", "--format", "github")
	if code != 1 || !strings.HasPrefix(out, "::error") {
		t.Errorf("github (%d):\n%s", code, out)
	}
	if _, stderr, code := pawl(t, root, "check", "--format", "xml"); code != 1 || !strings.Contains(stderr, "unknown format") {
		t.Errorf("unknown format: %d %q", code, stderr)
	}
	if _, stderr, code := pawl(t, root, "check", "--ring", "weekly"); code != 1 || stderr == "" {
		t.Errorf("unknown ring: %d %q", code, stderr)
	}
}

func TestBaselineOnlyShrinksWithoutAReason(t *testing.T) {
	root := goRepo(t)
	if _, stderr, code := pawl(t, root, "init"); code != 0 {
		t.Fatal(stderr)
	}
	if _, stderr, code := pawl(t, root, "baseline"); code != 0 {
		t.Fatal(stderr)
	}
	suppressed := "package main\n\nfunc main() {} //" + "nolint:unused // kept for the example\n"
	write(t, root, "main.go", suppressed)
	_, stderr, code := pawl(t, root, "baseline")
	if code != 1 || !strings.Contains(stderr, "escape.suppressions rose from 0 to 1") || !strings.Contains(stderr, "--grow") {
		t.Fatalf("baseline with a regression: %d %q", code, stderr)
	}
	out, stderr, code := pawl(t, root, "baseline", "--grow", "imported an example file")
	if code != 0 || !strings.Contains(out, "escape.suppressions") {
		t.Fatalf("baseline --grow: %d %s%s", code, out, stderr)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".pawl", "baseline.json"))
	if err != nil || !strings.Contains(string(raw), "imported an example file") {
		t.Errorf("growth reason not recorded: %v %s", err, raw)
	}
}

func TestCommandsNeedAManifest(t *testing.T) {
	root := goRepo(t)
	for _, args := range [][]string{{"check"}, {"doctor"}, {"status"}, {"baseline"}, {"rules"}, {"affected", "go"}, {"budget"}, {"record", "precommit", "1.5"}} {
		_, stderr, code := pawl(t, root, args...)
		if code != 1 || !strings.Contains(stderr, "pawl init") {
			t.Errorf("%v without a manifest: %d %q", args, code, stderr)
		}
	}
}

func TestAffectedPrintsTheScope(t *testing.T) {
	root := initRepo(t)
	t.Setenv("PAWL_FULL", "1")
	if out, stderr, code := pawl(t, root, "affected", "go"); code != 0 || strings.TrimSpace(out) != "./..." {
		t.Errorf("affected go: %d %q %q", code, out, stderr)
	}
	if _, stderr, code := pawl(t, root, "affected", "cobol"); code != 1 || stderr == "" {
		t.Errorf("affected cobol: %d %q", code, stderr)
	}
}

func TestRecordThenBudgetOverTheLimit(t *testing.T) {
	root := initRepo(t)
	t.Setenv("PAWL_FULL", "1")
	for _, secs := range []string{"25", "26", "27"} {
		if _, stderr, code := pawl(t, root, "record", "precommit", secs); code != 0 {
			t.Fatalf("record: %d %q", code, stderr)
		}
	}
	if _, stderr, code := pawl(t, root, "record", "precommit", "soon"); code != 1 || !strings.Contains(stderr, "seconds") {
		t.Errorf("record with a bad duration: %d %q", code, stderr)
	}
	out, _, code := pawl(t, root, "budget")
	if code != 1 || !strings.Contains(out, "speed.rings") || !strings.Contains(out, "limit") {
		t.Errorf("budget over the limit (%d):\n%s", code, out)
	}
}

func TestStatusAndVersion(t *testing.T) {
	root := initRepo(t)
	t.Setenv("PAWL_FULL", "1")
	if out, stderr, code := pawl(t, root, "status"); code != 0 || out == "" {
		t.Errorf("status without a remote: %d %q %q", code, out, stderr)
	}
	if out, _, code := pawl(t, root, "--version"); code != 0 || !strings.Contains(out, "test") {
		t.Errorf("--version: %d %q", code, out)
	}
}
