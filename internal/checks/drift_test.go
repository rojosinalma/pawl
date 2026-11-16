package checks

import (
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/report"
)

// onMain applies a change on main, commits it, and moves feature/work onto it.
func (f *fixture) onMain(msg string, change func()) {
	f.t.Helper()
	f.run("git", "checkout", "-q", "main")
	change()
	f.commit(msg)
	f.run("git", "checkout", "-q", "-B", "feature/work")
}

func TestPrinciples(t *testing.T) {
	f := newFixture(t, false)
	f.editManifest(func(m *manifest.Manifest) {
		m.Principles = []manifest.Principle{{Name: "money is exact", Suite: "test/principles/*_test.go"}, {Name: "no suite"}}
	})
	rep := f.check("check", "tests.principles")
	requireFinding(t, rep, "tests.principles", report.Error, `"money is exact": suite "test/principles/*_test.go" matches no file`)
	requireFinding(t, rep, "tests.principles", report.Error, `"no suite"`)

	f.write("test/principles/money_test.go", "package principles\n")
	f.editManifest(func(m *manifest.Manifest) { m.Principles = m.Principles[:1] })
	requireClean(t, f.check("check", "tests.principles"))
}

// withContract declares one contract and enforces drift.contracts, which the adapters waive by default.
func withContract(m *manifest.Manifest, version string) {
	m.Rules["drift.contracts"] = manifest.Rule{Step: "check", Via: []string{"pawl"}}
	m.Contracts = []manifest.Contract{{Name: "api", Path: "docs/api.yaml", Kind: "openapi", Version: version}}
}

func TestContracts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  bool
		version string
		want    bool
	}{
		{"changed without bump", true, "1.0.0", true},
		{"changed with bump", true, "1.1.0", false},
		{"unchanged", false, "1.0.0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, true)
			f.onMain("contract", func() {
				f.write("docs/api.yaml", "openapi: 3.1.0\n")
				f.editManifest(func(m *manifest.Manifest) { withContract(m, "1.0.0") })
			})
			if tc.change {
				f.write("docs/api.yaml", "openapi: 3.1.1\n")
			}
			f.editManifest(func(m *manifest.Manifest) { withContract(m, tc.version) })
			got := find(f.check("check", "drift.contracts"), "drift.contracts", "changed without a version bump")
			if tc.want != (len(got) > 0) {
				t.Fatalf("want finding = %v, got %v", tc.want, got)
			}
		})
	}
}

func TestContractsMissingPathAndNoBase(t *testing.T) {
	f := newFixture(t, false)
	f.editManifest(func(m *manifest.Manifest) { withContract(m, "1.0.0") })
	rep := f.check("check", "drift.contracts")
	requireFinding(t, rep, "drift.contracts", report.Error, "docs/api.yaml does not exist")
	if !skipped(rep, "drift.contracts") {
		t.Error("the bump check needs a base ref and should be skipped")
	}
}

func TestPRSize(t *testing.T) {
	for _, tc := range []struct {
		name     string
		branch   string
		workType string
		lines    int
		lockfile int
		want     string
	}{
		{"feature within budget", "feature/work", "", 300, 0, ""},
		{"bugfix branch over budget", "fix/thing", "", 300, 0, "over the bugfix budget of 200"},
		{"env overrides the branch", "feature/work", "hotfix", 60, 0, "over the hotfix budget of 50"},
		{"unprefixed branch is a feature", "work", "", 900, 0, "over the feature budget of 800"},
		{"lockfiles do not count", "hotfix/x", "", 10, 5000, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, true)
			t.Setenv(workTypeEnv, tc.workType)
			f.run("git", "checkout", "-q", "-B", tc.branch)
			f.write("notes.txt", strings.Repeat("line\n", tc.lines))
			f.write("go.sum", strings.Repeat("example.com/x v1.0.0 h1:abc=\n", tc.lockfile))
			f.commit("change")
			rep := f.check("check", "drift.pr-size")
			got := find(rep, "drift.pr-size", "budget")
			if tc.want == "" {
				if len(got) > 0 {
					t.Fatalf("unexpected: %v", got)
				}
				return
			}
			requireFinding(t, rep, "drift.pr-size", report.Error, tc.want)
		})
	}
}

func TestPRSizeSkips(t *testing.T) {
	f := newFixture(t, false)
	if !skipped(f.check("check", "drift.pr-size"), "drift.pr-size") {
		t.Error("no base ref should skip")
	}
	g := newFixture(t, true)
	t.Setenv(workTypeEnv, "chore")
	if !skipped(g.check("check", "drift.pr-size"), "drift.pr-size") {
		t.Error("a work type without a budget should skip")
	}
}

func TestCountExcluded(t *testing.T) {
	for file, want := range map[string]bool{
		".pawl/baseline.json":           true,
		"go.sum":                        true,
		"web/bun.lock":                  true,
		"vendor/x.lock":                 true,
		"old.txt => go.sum":             true,
		"src/{a.go => b.go}":            false,
		"main.go":                       false,
		".pawl/rules/test-branch.yml":   false,
		"docs/{old.lock => current.md}": false,
	} {
		if got := countExcluded(file); got != want {
			t.Errorf("countExcluded(%q) = %v, want %v", file, got, want)
		}
	}
}
