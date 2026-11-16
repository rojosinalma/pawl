package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/catalog"
)

func complete(t *testing.T) (*Manifest, *catalog.Catalog) {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := Defaults()
	m.Stacks = []string{"go"}
	for _, r := range cat.Rules {
		if r.IsReview() {
			m.Rules[r.ID] = Rule{Waived: "no tool reaches this rule"}
		} else {
			m.Rules[r.ID] = Rule{Step: "lint", Via: []string{"tool/" + r.ID}, Gaps: []string{"shell: no tool exists"}}
		}
	}
	return &m, cat
}

func TestWriteLoadRoundTrip(t *testing.T) {
	m, cat := complete(t)
	m.Project = Project{ConfigModule: "internal/config", OwnModule: "example.com/app", TestDirs: []string{"test"}, Exclude: []string{"gen/"}}
	m.Principles = []Principle{{Name: "restart safe", Suite: "test/restart/*_test.go"}}
	m.Contracts = []Contract{{Name: "api", Path: "docs/openapi.yaml", Kind: "openapi", Version: "1.2.0"}}
	m.Quarantine = []QuarantinedTest{{Name: "TestFlaky", Issue: "https://example.com/issues/1", Since: "2026-01-01"}}
	root := t.TempDir()
	if err := Write(root, m, cat); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !Exists(root) {
		t.Fatal("Exists is false after Write")
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if problems := got.Validate(cat); len(problems) != 0 {
		t.Fatalf("round trip produced problems: %v", problems)
	}
	if got.Project.OwnModule != "example.com/app" || got.Project.ConfigModule != "internal/config" || len(got.Project.Exclude) != 1 {
		t.Errorf("project not preserved: %+v", got.Project)
	}
	if len(got.Principles) != 1 || len(got.Contracts) != 1 || len(got.Quarantine) != 1 {
		t.Errorf("lists not preserved: %+v %+v %+v", got.Principles, got.Contracts, got.Quarantine)
	}
	e := got.Rules["code.format"]
	if e.Step != "lint" || len(e.Via) != 1 || len(e.Gaps) != 1 {
		t.Errorf("rule entry not preserved: %+v", e)
	}
	if steps := got.Steps(); len(steps) != 1 || steps[0] != "lint" {
		t.Errorf("Steps = %v", steps)
	}
	if waived := got.Waived(cat); len(waived) != len(cat.ByClass(catalog.Review)) {
		t.Errorf("Waived = %d entries", len(waived))
	}
}

func TestValidateReportsEachProblem(t *testing.T) {
	m, cat := complete(t)
	delete(m.Rules, "code.format")
	m.Rules["code.lint"] = Rule{Step: "lint", Waived: "also waived here"}
	m.Rules["code.types"] = Rule{}
	m.Rules["code.dead"] = Rule{Waived: "no"}
	m.Rules["review.simplest"] = Rule{Step: "lint"}
	m.Rules["made.up"] = Rule{Step: "lint"}
	m.Rings["check"] = Ring{Target: "10m", Limit: "5m"}
	delete(m.Rings, "nightly")
	m.Budgets.FileLines = 0
	m.Quarantine = []QuarantinedTest{{Name: "TestFlaky"}}

	got := map[string]string{}
	for _, p := range m.Validate(cat) {
		got[p.RuleID+"|"+p.Message] = p.String()
	}
	wants := []string{
		"code.format|missing from the manifest",
		"code.lint|has both",
		"code.types|has neither",
		"code.dead|waiver needs a reason",
		"review.simplest|review rules cannot be enforced",
		"made.up|is not a catalog rule",
		"|rings.check",
		"|rings.nightly is missing",
		"|budgets:",
		"|quarantine entries need",
	}
	for _, want := range wants {
		found := false
		for key := range got {
			id, msg, _ := strings.Cut(key, "|")
			wantID, wantMsg, _ := strings.Cut(want, "|")
			if id == wantID && strings.HasPrefix(msg, wantMsg) {
				found = true
			}
		}
		if !found {
			t.Errorf("no problem matching %q in %v", want, got)
		}
	}
}

func TestLoadRejectsUnknownKeysAndVersions(t *testing.T) {
	root := t.TempDir()
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(root, FileName), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("version = 1\nsurprise = true\n")
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "unknown keys") {
		t.Errorf("unknown key: got %v", err)
	}
	write("version = 2\n")
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Errorf("version: got %v", err)
	}
	write("version = [")
	if _, err := Load(root); err == nil {
		t.Error("invalid TOML loaded")
	}
	if _, err := Require(t.TempDir()); err != ErrNoManifest {
		t.Errorf("Require without a manifest: %v", err)
	}
}

func TestRingDurations(t *testing.T) {
	target, limit, err := Ring{Target: "10s", Limit: "20s"}.Durations()
	if err != nil || target.Seconds() != 10 || limit.Seconds() != 20 {
		t.Errorf("Durations = %v %v %v", target, limit, err)
	}
	for _, r := range []Ring{{Target: "x", Limit: "1s"}, {Target: "1s", Limit: "x"}, {Target: "2s", Limit: "1s"}} {
		if _, _, err := r.Durations(); err == nil {
			t.Errorf("Durations(%+v) accepted", r)
		}
	}
}
