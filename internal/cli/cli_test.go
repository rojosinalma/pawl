package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/testrepo"
)

// goRepo is a small Go repository with one commit on main.
func goRepo(t *testing.T) string {
	t.Helper()
	return testrepo.New(t, map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.25\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
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

func TestBaselineThenVerifyPassOnAFreshInit(t *testing.T) {
	root := initRepo(t)
	testrepo.Git(t, root, "add", "-A")
	testrepo.Git(t, root, "commit", "-q", "--no-verify", "-m", "pawl init")
	out, stderr, code := pawl(t, root, "baseline")
	if code != 0 || !strings.Contains(out, "wrote .pawl/baseline.json") {
		t.Fatalf("baseline (%d): %s%s", code, out, stderr)
	}

	out, stderr, code = pawl(t, root, "verify", "--only", "repo.targets,repo.hook,escape.suppressions,code.file-size")
	if code != 0 {
		t.Fatalf("verify on a fresh init failed (%d):\n%s%s", code, out, stderr)
	}
	if !strings.Contains(out, "no findings") {
		t.Errorf("verify output:\n%s", out)
	}
}

func TestVerifyReportsFindingsAndFormats(t *testing.T) {
	root := goRepo(t)
	if _, stderr, code := pawl(t, root, "init"); code != 0 {
		t.Fatal(stderr)
	}
	if err := os.Remove(filepath.Join(root, ".githooks", "pre-commit")); err != nil {
		t.Fatal(err)
	}
	out, _, code := pawl(t, root, "verify", "--only", "repo.hook")
	if code != 1 || !strings.Contains(out, "repo.hook") || !strings.Contains(out, "fix:") {
		t.Errorf("verify with a missing hook (%d):\n%s", code, out)
	}
	out, _, code = pawl(t, root, "verify", "--only", "repo.hook", "--format", "json")
	if code != 1 || !strings.Contains(out, `"rule": "repo.hook"`) {
		t.Errorf("json (%d):\n%s", code, out)
	}
	out, _, code = pawl(t, root, "verify", "--only", "repo.hook", "--format", "github")
	if code != 1 || !strings.HasPrefix(out, "::error") {
		t.Errorf("github (%d):\n%s", code, out)
	}
	if _, stderr, code := pawl(t, root, "verify", "--format", "xml"); code != 1 || !strings.Contains(stderr, "unknown format") {
		t.Errorf("unknown format: %d %q", code, stderr)
	}
	if _, stderr, code := pawl(t, root, "verify", "--ring", "weekly"); code != 1 || stderr == "" {
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
	testrepo.Write(t, root, "main.go", suppressed)
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
	t.Setenv("MAKELEVEL", "")
	root := goRepo(t)
	for _, args := range [][]string{{"verify"}, {"check"}, {"run", "check"}, {"install"}, {"doctor"}, {"status"}, {"baseline"}, {"rules"}, {"affected", "go"}, {"budget"}, {"record", "precommit", "1.5"}} {
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

func TestResolveVersion(t *testing.T) {
	cases := []struct{ stamped, module, want string }{
		{"v1.2.3", "v9.9.9", "v1.2.3"},
		{"dev", "v0.1.0", "v0.1.0"},
		{"", "v0.1.0", "v0.1.0"},
		{"dev", "(devel)", "dev"},
		{"dev", "", "dev"},
	}
	for _, c := range cases {
		if got := ResolveVersion(c.stamped, c.module); got != c.want {
			t.Errorf("ResolveVersion(%q, %q) = %q, want %q", c.stamped, c.module, got, c.want)
		}
	}
}

func TestCheckInsideMakeVerifiesAndNamesTheOldRecipe(t *testing.T) {
	root := initRepo(t)
	if err := os.Remove(filepath.Join(root, ".githooks", "pre-commit")); err != nil {
		t.Fatal(err)
	}
	oldMakefile := strings.ReplaceAll(readFile(t, root, "Makefile"), "$(PAWL) verify", "$(PAWL) check")
	testrepo.Write(t, root, "Makefile", oldMakefile)
	t.Setenv("MAKELEVEL", "1")
	out, stderr, code := pawl(t, root, "check")
	if code != 1 || !strings.Contains(out, "repo.hook") {
		t.Errorf("check inside make does not run the verification (%d):\n%s", code, out)
	}
	if strings.TrimSpace(stderr) != `pawl: this Makefile calls "pawl check"; change the recipe to "pawl verify"` {
		t.Errorf("check inside make: stderr %q", stderr)
	}
}

// Inside another make (a project target that calls pawl) a current Makefile gets the gate, not the verification.
func TestCheckInsideMakeRunsTheGateOfACurrentMakefile(t *testing.T) {
	root := ringRepo(t)
	t.Setenv("MAKELEVEL", "1")
	out, stderr, code := pawl(t, root, "check", "--no-toolbox")
	if code != 1 || !strings.Contains(out, "pawl: check failed") || strings.Contains(stderr, "change the recipe") {
		t.Errorf("check inside make with a current Makefile: %d %q %q", code, out, stderr)
	}
}

// ringRepo is initRepo with a Makefile whose rings need no tool: precommit passes, check and nightly fail in one step.
func ringRepo(t *testing.T) string {
	t.Helper()
	root := initRepo(t)
	testrepo.Write(t, root, "Makefile", ".PHONY: precommit check nightly lint-a lint-b\n"+
		"precommit:\n\t@echo ring ran\n"+
		"check: lint-a lint-b\n"+
		"nightly: check\n"+
		"lint-a:\n\t@echo a is fine\n"+
		"lint-b:\n\t@echo b is broken; exit 3\n")
	return root
}

func TestRunReachesTheRunner(t *testing.T) {
	t.Setenv("MAKELEVEL", "")
	root := ringRepo(t)
	out, stderr, code := pawl(t, root, "run", "precommit", "--no-toolbox")
	if code != 0 || !strings.Contains(out, "pawl: precommit passed") || strings.Contains(out, "ring ran") {
		t.Errorf("a passing ring prints one line and exits 0: %d %q %q", code, out, stderr)
	}
	out, stderr, code = pawl(t, root, "run", "nightly", "--verbose", "--no-toolbox")
	if code != 1 || !strings.Contains(out, "a is fine") || !strings.Contains(out, "nightly failed") || stderr != "" && strings.Contains(stderr, "pawl:") {
		t.Errorf("a failing ring in verbose mode streams everything and exits 1 without an error line: %d %q %q", code, out, stderr)
	}
}

func TestCheckIsTheGate(t *testing.T) {
	t.Setenv("MAKELEVEL", "")
	root := ringRepo(t)
	out, stderr, code := pawl(t, root, "check", "--no-toolbox")
	if code != 1 || !strings.Contains(out, "pawl: check failed") || !strings.Contains(out, "--- lint-b") || !strings.Contains(out, "b is broken") {
		t.Errorf("check runs the check ring: %d %q %q", code, out, stderr)
	}
	if strings.Contains(out, "a is fine") {
		t.Errorf("the output of a passing step is shown:\n%s", out)
	}
}

// The machine of this test has no mise and no tools: PATH holds git, make and sh only, HOME is empty.
func TestInstallReachesTheInstaller(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"git", "make", "sh"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s is not installed", name)
		}
		if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	root := testrepo.New(t, map[string]string{"run.sh": "#!/usr/bin/env bash\nset -euo pipefail\necho hi\n"})
	if out, stderr, code := pawl(t, root, "init", "--stack", "shell"); code != 0 {
		t.Fatalf("init failed (%d): %s%s", code, out, stderr)
	}
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	out, stderr, code := pawl(t, root, "install", "--no-mise")
	if code != 1 {
		t.Errorf("install on a machine without the tools must exit 1: %d %q", code, stderr)
	}
	for _, want := range []string{"skipped mise: skipped on request", "skipped tools of mise.toml", "repo.pinned", "shellcheck"} {
		if !strings.Contains(out, want) {
			t.Errorf("install output lacks %q:\n%s", want, out)
		}
	}
}

func TestRunRejectsAMissingTargetAndBadArguments(t *testing.T) {
	root := initRepo(t)
	if _, stderr, code := pawl(t, root, "run", "weekly"); code != 1 || !strings.Contains(stderr, "no make target weekly") {
		t.Errorf("a missing target: %d %q", code, stderr)
	}
	for _, args := range [][]string{{"run"}, {"run", "check", "nightly"}} {
		_, stderr, code := pawl(t, root, args...)
		if code != 1 || !strings.Contains(stderr, "takes one make target") {
			t.Errorf("%v: %d %q", args, code, stderr)
		}
	}
}

func TestInitNamesTheNextCommands(t *testing.T) {
	out, stderr, code := pawl(t, goRepo(t), "init")
	if code != 0 || !strings.Contains(out, "next: `pawl install` (the pinned tools), `pawl run lint`, `pawl baseline` (records the debt of today), `pawl check`, then commit.") {
		t.Errorf("init (%d): %s%s", code, out, stderr)
	}
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
