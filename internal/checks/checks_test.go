package checks

import (
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/report"
)

func TestCleanProjectPasses(t *testing.T) {
	for _, tc := range []struct {
		name string
		git  bool
		ring string
	}{
		{"git check ring", true, ""},
		{"git explicit check ring", true, "check"},
		{"git precommit ring", true, "precommit"},
		{"plain directory", false, "check"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.git)
			rep := f.check(tc.ring)
			requireClean(t, rep)
			if len(rep.Checked) == 0 {
				t.Fatal("no rule recorded as checked")
			}
		})
	}
}

func TestCleanProjectRecordsRules(t *testing.T) {
	f := newFixture(t, true)
	rep := f.check("check")
	checked := strings.Join(rep.Checked, " ")
	for _, rule := range []string{"repo.targets", "repo.hook", "repo.pinned", "repo.ci-shape", "escape.suppressions",
		"code.file-size", "code.debug-leftovers", "drift.pr-size", "speed.affected", "speed.rings", "tests.runner"} {
		if !strings.Contains(checked, rule) {
			t.Errorf("%s not in Checked", rule)
		}
	}
	if !skipped(rep, "escape.duplication") {
		t.Errorf("duplication without a report should be skipped:\n%s", dump(rep))
	}
	seen := map[string]bool{}
	for _, id := range rep.Checked {
		if seen[id] {
			t.Errorf("%s recorded twice", id)
		}
		seen[id] = true
	}
}

func TestUnknownRing(t *testing.T) {
	f := newFixture(t, false)
	if _, err := Run(f.load(), Options{Ring: "weekly"}); err == nil {
		t.Fatal("expected an error for an unknown ring")
	}
}

func TestManifestProblems(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(m *manifest.Manifest)
		rule string
		want string
	}{
		{"missing rule", func(m *manifest.Manifest) { delete(m.Rules, "code.lint") }, "code.lint", "missing from the manifest"},
		{"empty rule", func(m *manifest.Manifest) { m.Rules["code.lint"] = manifest.Rule{} }, "code.lint", "neither a step nor a waiver"},
		{"short waiver", func(m *manifest.Manifest) { m.Rules["code.types"] = manifest.Rule{Waived: "no"} }, "code.types", "at least three words"},
		{"missing ring", func(m *manifest.Manifest) { delete(m.Rings, "nightly") }, "repo.targets", "rings.nightly"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, false)
			f.editManifest(tc.edit)
			rep := f.check("precommit")
			got := requireFinding(t, rep, tc.rule, report.Error, tc.want)
			if got.File != manifest.FileName {
				t.Errorf("file = %q, want %s", got.File, manifest.FileName)
			}
		})
	}
}

func TestManifestProblemLine(t *testing.T) {
	f := newFixture(t, false)
	f.editManifest(func(m *manifest.Manifest) { m.Rules["code.types"] = manifest.Rule{Waived: "no"} })
	got := requireFinding(t, f.check("check"), "code.types", report.Error, "three words")
	if got.Line == 0 {
		t.Error("the finding should point at the rule's table")
	}
}

func TestOnlyFiltersFindings(t *testing.T) {
	f := newFixture(t, false)
	f.remove(hookFile)
	f.editManifest(func(m *manifest.Manifest) { delete(m.Rules, "code.lint") })
	rep := f.check("check", "repo.hook")
	requireFinding(t, rep, "repo.hook", report.Error, "missing")
	for _, x := range rep.Findings {
		if x.RuleID != "repo.hook" {
			t.Errorf("finding of %s passed the Only filter", x.RuleID)
		}
	}
	if len(rep.Checked) != 1 || rep.Checked[0] != "repo.hook" {
		t.Errorf("Checked = %v, want [repo.hook]", rep.Checked)
	}
}

func TestWaivedRuleDoesNotRun(t *testing.T) {
	f := newFixture(t, false)
	f.remove(hookFile)
	f.editManifest(func(m *manifest.Manifest) {
		m.Rules["repo.hook"] = manifest.Rule{Waived: "this repository has no local commits at all"}
	})
	if got := find(f.check("check"), "repo.hook", ""); len(got) > 0 {
		t.Fatalf("waived rule reported: %v", got)
	}
}
