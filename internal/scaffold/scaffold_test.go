package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/stack/golang"

	"github.com/rojosinalma/pawl/internal/catalog"
	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/ratchet"
)

func goShellProject(t *testing.T, git bool) string {
	t.Helper()
	return newProject(t, git, map[string]string{
		"go.mod":         "module example.com/demo\n\ngo 1.25\n",
		"main.go":        "package main\n\nfunc main() {}\n",
		"scripts/run.sh": "#!/bin/sh\nset -eu\necho run\n",
	})
}

func TestInitGoShellEndToEnd(t *testing.T) {
	dir := goShellProject(t, true)
	res := mustInit(t, dir, Options{Version: "v1.2.3"})

	if strings.Join(res.Stacks, ",") != "go,shell" {
		t.Fatalf("stacks = %v", res.Stacks)
	}
	for _, f := range []string{hookPath, "mise.toml", ".github/workflows/check.yml", ".github/workflows/nightly.yml", ".github/workflows/review.yml", agentsFile, ".gitignore", ratchet.FileName, manifest.FileName} {
		if !hasItem(res.Created, f) {
			t.Errorf("Created lacks %s: %v", f, res.Created)
		}
	}

	for name, check := range map[string]func(*testing.T, string, *Result){
		"hook":      checkHook,
		"mise":      checkMise,
		"workflows": checkWorkflows,
		"agents":    checkAgents,
		"gitignore": checkGitignore,
		"baseline":  checkBaseline,
		"manifest":  checkManifest,
		"hints":     checkHints,
		"makefile":  checkMakefile,
	} {
		t.Run(name, func(t *testing.T) { check(t, dir, res) })
	}

	if _, err := Init(dir, Options{}); err == nil {
		t.Error("a second Init succeeded over an existing manifest")
	}
}

func checkHook(t *testing.T, dir string, _ *Result) {
	t.Helper()
	st, err := os.Stat(filepath.Join(dir, hookPath))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o111 == 0 {
		t.Errorf("hook mode %v is not executable", st.Mode())
	}
	assertContains(t, "hook", readFile(t, dir, hookPath), "set -eu", `pawl="${PAWL:-pawl}"`, `command -v "${pawl}"`, `exec "${pawl}" run precommit`, "exec make precommit", "git config pawl.toolbox")
	if got := runIn(t, dir, "git", "config", "core.hooksPath"); got != hooksDir {
		t.Errorf("core.hooksPath = %q", got)
	}
}

func checkMise(t *testing.T, dir string, _ *Result) {
	t.Helper()
	mise := readFile(t, dir, "mise.toml")
	assertContains(t, "mise.toml", mise, "[tools]", "# every stack", "# Go", "# Shell", "golangci-lint = ", `"github:go-gremlins/gremlins" = `,
		`"npm:jscpd" = `, `"aqua:rhysd/actionlint" = `, `node = "24.22.2"`, `python = "3.13.18"`, `uv = "0.13.8"`)
}

func checkWorkflows(t *testing.T, dir string, _ *Result) {
	t.Helper()
	check := readFile(t, dir, ".github/workflows/check.yml")
	assertContains(t, "check.yml", check, "merge_group", "branches:\n      - main\n", "persist-credentials: false", "fetch-depth: 0",
		"actions/checkout@"+actions["actions/checkout"].SHA+" # "+actions["actions/checkout"].Tag,
		"actions/setup-go@"+actions["actions/setup-go"].SHA, "go-version-file: go.mod",
		"jdx/mise-action@"+actions["jdx/mise-action"].SHA,
		"PAWL_RELEASE: https://github.com/rojosinalma/pawl/releases/download/v1.2.3", "PAWL_ASSET: pawl_1.2.3_linux_amd64.tar.gz",
		"name: Set up Pawl", "sha256sum -c asset.sha256", `PAWL_REQUIRED: "1"`,
		"run: make check", "run: make coverage", "PAWL_BASE: origin/${{ github.base_ref || 'main' }}",
		"if: failure()", "then pawl verify --format github; fi", "timeout-minutes: 30")
	assertLacks(t, "check.yml", check, "setup-ruby", "setup-uv", "setup-bun", "rustup", "pawl check", "rojosinalma/pawl@")
	nightly := readFile(t, dir, ".github/workflows/nightly.yml")
	assertContains(t, "nightly.yml", nightly, `cron: "17 3 * * *"`, "workflow_dispatch", "run: make nightly", `PAWL_FULL: "1"`,
		"issues: write", "GH_TOKEN: ${{ github.token }}", "TITLE: nightly failed", "gh issue create")
	review := readFile(t, dir, ".github/workflows/review.yml")
	assertContains(t, "review.yml", review, "workflow_dispatch", "Never a required check", "OpenCode", "Claude Code", "PR-Agent")
	assertLacks(t, "review.yml", review, "uses:", "  pull_request:")
	for _, name := range workflowFiles {
		checkWorkflow(t, name, readFile(t, dir, ".github/workflows/"+name))
	}
}

func checkMakefile(t *testing.T, dir string, _ *Result) {
	t.Helper()
	mk := readFile(t, dir, "Makefile")
	assertContains(t, "Makefile", mk, "\t@$(call pawl_run,verify)\n", "\t@$(call pawl_run,verify --ring precommit)\n",
		"\t@$(call pawl_run,verify --ring nightly)\n", "\t@$(call pawl_run,budget)\n", "\ndefine pawl_guard\n", "PAWL_REQUIRED=1")
	assertLacks(t, "Makefile", mk, "$(PAWL) check", "\t$(PAWL) verify")
}

func checkAgents(t *testing.T, dir string, _ *Result) {
	t.Helper()
	agents := readFile(t, dir, agentsFile)
	if !strings.HasPrefix(agents, "# demo\n\n"+agentsBegin) {
		t.Errorf("AGENTS.md starts wrong:\n%s", agents)
	}
	assertContains(t, "AGENTS.md", agents, "## Quality gate", "`make check` is the merge gate", "`pawl verify`", "`pawl run check` runs it", "`pawl rules show <id>`",
		"never disable a linter rule", "## Review checklist", "## Not enforced here", agentsEnd)
	assertLacks(t, "AGENTS.md", agents, "AGENTS.local.md")
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range cat.Rules {
		if r.IsReview() {
			assertContains(t, "AGENTS.md", agents, "- **"+strings.TrimSuffix(r.Title, ".")+".** ")
		}
	}
}

func checkGitignore(t *testing.T, dir string, _ *Result) {
	t.Helper()
	assertContains(t, ".gitignore", readFile(t, dir, ".gitignore"), "# pawl\n", ".pawl/reports/\n", "AGENTS.local.md\n")
}

func checkBaseline(t *testing.T, dir string, _ *Result) {
	t.Helper()
	b, ok, err := ratchet.Load(dir)
	if err != nil || !ok {
		t.Fatalf("baseline: ok=%v err=%v", ok, err)
	}
	if b.Version != 1 {
		t.Errorf("baseline version %d", b.Version)
	}
}

func checkManifest(t *testing.T, dir string, _ *Result) {
	t.Helper()
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	if problems := m.Validate(cat); len(problems) > 0 {
		t.Errorf("manifest problems: %v", problems)
	}
}

func checkHints(t *testing.T, _ string, res *Result) {
	t.Helper()
	if !hasHint(res, "run: go get -tool "+golang.GovulncheckModule+" "+golang.Gcov2lcovModule) {
		t.Errorf("hints = %v", res.Hints)
	}
}

func TestHookHandsTheRingToPawl(t *testing.T) {
	dir := goShellProject(t, true)
	mustInit(t, dir, Options{})
	fake := filepath.Join(dir, "fake-pawl")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"$@\" > \"$(dirname \"$0\")/called\"\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := execHook(t, dir, fake)
	if err := cmd.Run(); err == nil || cmd.ProcessState.ExitCode() != 3 {
		t.Errorf("the hook must exit with pawl's status 3, got %v", err)
	}
	if got := strings.TrimSpace(readFile(t, dir, "called")); got != "run precommit" {
		t.Errorf("the hook called pawl with %q", got)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	dir := goShellProject(t, true)
	before := listTree(t, dir)
	res := mustInit(t, dir, Options{DryRun: true})
	if after := listTree(t, dir); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Errorf("dry run changed the tree:\nbefore %v\nafter  %v", before, after)
	}
	if !hasItem(res.Created, hookPath) || !hasItem(res.Created, ratchet.FileName) || !hasItem(res.Created, agentsFile) {
		t.Errorf("dry run did not report the files: %v", res.Created)
	}
	cmd := gitCmd(t, dir, "config", "core.hooksPath")
	if out, err := cmd.Output(); err == nil {
		t.Errorf("dry run set core.hooksPath to %q", out)
	}
}

func TestOutsideGitRepository(t *testing.T) {
	dir := goShellProject(t, false)
	mustInit(t, dir, Options{})
	if _, err := os.Stat(filepath.Join(dir, hookPath)); err != nil {
		t.Error(err)
	}
}

func TestExistingMakefileGainsOnlyMissingTargets(t *testing.T) {
	dir := goShellProject(t, true)
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\techo mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := mustInit(t, dir, Options{})
	mk := readFile(t, dir, "Makefile")
	if !strings.HasPrefix(mk, "test:\n\techo mine\n") {
		t.Errorf("existing target changed:\n%s", mk)
	}
	assertContains(t, "Makefile", mk, "check:", "precommit:")
	if strings.Count(mk, "\ntest:") > 0 {
		t.Errorf("test target duplicated:\n%s", mk)
	}
	if !hasItem(res.Merged, "Makefile") {
		t.Errorf("Merged = %v", res.Merged)
	}
}

func TestExistingFilesAreExtended(t *testing.T) {
	dir := newProject(t, true, map[string]string{
		"go.mod":         "module example.com/demo\n\ngo 1.25\n\ntool (\n\tgolang.org/x/vuln/cmd/govulncheck\n\tgithub.com/jandelgado/gcov2lcov\n)\n",
		"main.go":        "package main\n",
		agentsFile:       "# Mine\n\nOur notes.",
		".gitignore":     "bin/\nAGENTS.local.md\n",
		".tool-versions": "golang 1.25.0\n",
	})
	res := mustInit(t, dir, Options{})
	agents := readFile(t, dir, agentsFile)
	if !strings.HasPrefix(agents, "# Mine\n\nOur notes.\n\n"+agentsBegin) {
		t.Errorf("AGENTS.md not appended:\n%s", agents)
	}
	if got := readFile(t, dir, ".gitignore"); got != "bin/\nAGENTS.local.md\n\n# pawl\n.pawl/reports/\n" {
		t.Errorf(".gitignore = %q", got)
	}
	for _, f := range []string{agentsFile, ".gitignore"} {
		if !hasItem(res.Merged, f) {
			t.Errorf("Merged lacks %s: %v", f, res.Merged)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "mise.toml")); err == nil {
		t.Error("mise.toml written next to .tool-versions")
	}
	if hasHint(res, "run: go get") {
		t.Errorf("go tools already declared, hints = %v", res.Hints)
	}
}

func TestSecondRunKeepsWhatExists(t *testing.T) {
	dir := goShellProject(t, true)
	mustInit(t, dir, Options{})
	if err := os.Remove(filepath.Join(dir, manifest.FileName)); err != nil {
		t.Fatal(err)
	}
	res := mustInit(t, dir, Options{})
	for _, f := range []string{hookPath, agentsFile, ".gitignore", ratchet.FileName, ".github/workflows/check.yml", "mise.toml"} {
		if !hasItem(res.Kept, f) {
			t.Errorf("Kept lacks %s: %v", f, res.Kept)
		}
	}
	if len(res.Merged) != 0 {
		t.Errorf("Merged = %v", res.Merged)
	}
}

func TestSyncAgentsReplacesBlockInPlace(t *testing.T) {
	dir := goShellProject(t, true)
	mustInit(t, dir, Options{})
	fresh := readFile(t, dir, agentsFile)
	block := fresh[strings.Index(fresh, agentsBegin):]
	edited := "# demo\n\nIntro.\n\n" + agentsBegin + "\nstale\n" + agentsEnd + "\n\n## Ours\n\nOutro.\n"
	if err := os.WriteFile(filepath.Join(dir, agentsFile), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := SyncAgents(p, false)
	if err != nil || !changed {
		t.Fatalf("SyncAgents = %v, %v", changed, err)
	}
	want := "# demo\n\nIntro.\n\n" + block + "\n## Ours\n\nOutro.\n"
	if got := readFile(t, dir, agentsFile); got != want {
		t.Errorf("AGENTS.md =\n%s\nwant\n%s", got, want)
	}
	if changed, err := SyncAgents(p, false); err != nil || changed {
		t.Errorf("second SyncAgents = %v, %v", changed, err)
	}

	if err := os.WriteFile(filepath.Join(dir, agentsFile), []byte(agentsBegin+"\nno end\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncAgents(p, false); err == nil {
		t.Error("a block without an end marker was accepted")
	}

	if err := os.Remove(filepath.Join(dir, agentsFile)); err != nil {
		t.Fatal(err)
	}
	if changed, err := SyncAgents(p, false); err != nil || !changed {
		t.Errorf("SyncAgents on a missing file = %v, %v", changed, err)
	}
}

func TestPawlDownload(t *testing.T) {
	latest := "https://github.com/rojosinalma/pawl/releases/latest/download/pawl_linux_amd64.tar.gz"
	for version, want := range map[string]string{
		"":               latest,
		"dev":            latest,
		"v1.2.3-dirty+x": latest,
		"1.2.3":          "https://github.com/rojosinalma/pawl/releases/download/v1.2.3/pawl_1.2.3_linux_amd64.tar.gz",
		"v0.4.0-rc.1":    "https://github.com/rojosinalma/pawl/releases/download/v0.4.0-rc.1/pawl_0.4.0-rc.1_linux_amd64.tar.gz",
	} {
		if release, asset := pawlDownload(version); release+"/"+asset != want {
			t.Errorf("pawlDownload(%q) = %s/%s, want %s", version, release, asset, want)
		}
	}
}

func TestPawlUses(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []struct{ version, commit, want string }{
		{"v1.2.3", sha, "rojosinalma/pawl@" + sha + " # v1.2.3"},
		{"1.2.3", sha, "rojosinalma/pawl@" + sha + " # v1.2.3"},
		{"dev", sha, ""},
		{"v0.4.0-rc.1", sha, ""},
		{"v1.2.3", "", ""},
		{"v1.2.3", "0123456", ""},
		{"v1.2.3", strings.ToUpper(sha), ""},
	} {
		if got := pawlUses(c.version, c.commit); got != c.want {
			t.Errorf("pawlUses(%q, %q) = %q, want %q", c.version, c.commit, got, c.want)
		}
	}
}

func TestUsesUnknownAction(t *testing.T) {
	if _, err := uses("someone/unpinned"); err == nil {
		t.Error("an unpinned action rendered")
	}
}
