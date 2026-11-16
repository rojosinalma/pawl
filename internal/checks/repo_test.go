package checks

import (
	"testing"

	"github.com/rojosinalma/pawl/internal/report"
)

func TestHook(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(f *fixture)
		want  string
	}{
		{"missing", func(f *fixture) { f.remove(hookFile) }, "hook is missing"},
		{"not executable", func(f *fixture) { f.writeMode(hookFile, "#!/bin/sh\nmake precommit\n", 0o644) }, "not executable"},
		{"no ring", func(f *fixture) { f.writeMode(hookFile, "#!/bin/sh\nexit 0\n", 0o755) }, "runs neither `pawl run precommit` nor `make precommit`"},
		{"the old check", func(f *fixture) { f.writeMode(hookFile, "#!/bin/sh\npawl check\n", 0o755) }, "runs neither"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, true)
			tc.setup(f)
			requireFinding(t, f.check("check", "repo.hook"), "repo.hook", report.Error, tc.want)
		})
	}
}

func TestHookMayRunTheRingThroughPawlOrMake(t *testing.T) {
	for _, body := range []string{"#!/bin/sh\nexec \"${PAWL:-pawl}\" run precommit\n", "#!/bin/sh\nexec make precommit\n"} {
		f := newFixture(t, true)
		f.writeMode(hookFile, body, 0o755)
		requireClean(t, f.check("check", "repo.hook"))
	}
}

// The hooks path of a clone is the machine's business (`pawl doctor`); a fresh CI checkout must pass the check.
func TestHookIgnoresTheClonesHooksPath(t *testing.T) {
	f := newFixture(t, true)
	f.run("git", "config", "--unset", "core.hooksPath")
	requireClean(t, f.check("check", "repo.hook"))
}

func TestPins(t *testing.T) {
	for _, tc := range []struct {
		name  string
		git   bool
		setup func(f *fixture)
		want  string
	}{
		{"lockfile untracked", true, func(f *fixture) {
			f.run("git", "rm", "-q", "--cached", "go.sum")
		}, "no lockfile is tracked"},
		{"lockfile missing outside git", false, func(f *fixture) { f.remove("go.sum") }, "no lockfile is tracked"},
		{"version pin missing", false, func(f *fixture) { f.remove("go.mod") }, "no pin for the Go version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.git)
			tc.setup(f)
			requireFinding(t, f.check("check", "repo.pinned"), "repo.pinned", report.Error, tc.want)
		})
	}
}

func TestPinsSkipStackWithoutFiles(t *testing.T) {
	f := newFixture(t, false)
	f.remove("go.mod")
	f.remove("go.sum")
	f.remove("main.go")
	requireClean(t, f.check("check", "repo.pinned"))
}

func TestWorkflows(t *testing.T) {
	for _, tc := range []struct {
		name     string
		file     string
		workflow string
		sev      report.Severity
		want     string
		line     int
	}{
		{"unpinned action", ".github/workflows/check.yml",
			"name: check\njobs:\n  a:\n    steps:\n      - uses: actions/checkout@v4\n      - run: make check\n",
			report.Error, "`actions/checkout@v4` is not pinned", 5},
		{"third-party short sha", ".github/workflows/release.yaml",
			"jobs:\n  a:\n    steps:\n      - uses: goreleaser/goreleaser-action@0123abc\n",
			report.Error, "not pinned to a commit SHA", 4},
		{"reusable workflow", ".github/workflows/other.yml",
			"jobs:\n  a:\n    uses: acme/ci/.github/workflows/x.yml@main\n",
			report.Error, "not pinned to a commit SHA", 3},
		{"docker image without digest", ".github/workflows/other.yml",
			"jobs:\n  a:\n    steps:\n      - uses: docker://alpine:3\n",
			report.Error, "not pinned to a digest", 4},
		{"ring step outside make", ".github/workflows/nightly.yml",
			"jobs:\n  a:\n    steps:\n      - run: echo hi\n      - run: make nightly\n",
			report.Warning, "calls neither make nor pawl", 4},
		{"ring workflow by name without make", ".github/workflows/ci.yml",
			"name: Check\njobs:\n  a:\n    steps:\n      - run: pawl verify\n",
			report.Error, "runs no make target", 0},
		{"unparsable", ".github/workflows/broken.yml", "jobs: [\n", report.Error, "cannot parse", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, false)
			f.write(tc.file, tc.workflow)
			got := requireFinding(t, f.check("check", "repo.ci-shape"), "repo.ci-shape", tc.sev, tc.want)
			if got.File != tc.file || got.Line != tc.line {
				t.Errorf("location = %s:%d, want %s:%d", got.File, got.Line, tc.file, tc.line)
			}
		})
	}
}

func TestWorkflowsAccepted(t *testing.T) {
	f := newFixture(t, false)
	f.write(".github/workflows/other.yml", "jobs:\n  a:\n    steps:\n      - uses: ./.github/actions/setup\n"+
		"      - uses: docker://alpine@sha256:abc\n      - run: echo not a ring workflow\n")
	f.write(".github/workflows/nightly.yml", "jobs:\n  a:\n    steps:\n      - run: |\n          git config x y\n          make nightly\n      - run: ./bin/pawl budget\n")
	f.write(".github/workflows/empty.yml", "")
	requireClean(t, f.check("check", "repo.ci-shape"))
}
