package ruby

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/stack"
)

func params(framework string) stack.Params {
	return stack.Params{Complexity: 15, FileLines: 400, DiffCoverage: 80, Facts: map[string]string{"test_framework": framework}}
}

func writeDefaults(t *testing.T, framework string) string {
	t.Helper()
	dir := t.TempDir()
	for _, c := range (Adapter{}).Configs(params(framework)) {
		if err := os.WriteFile(filepath.Join(dir, c.Path), []byte(c.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// edit replaces old with new in .rubocop.yml; an empty old appends new.
func edit(t *testing.T, dir, old, new string) {
	t.Helper()
	path := filepath.Join(dir, rubocopFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	switch {
	case old == "":
		s += "\n" + new
	case !strings.Contains(s, old):
		t.Fatalf(".rubocop.yml lacks %q", old)
	default:
		s = strings.Replace(s, old, new, 1)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findings(dir, framework string) map[string][]string {
	out := map[string][]string{}
	for _, v := range (Adapter{}).Verifiers(params(framework)) {
		for _, f := range v.Check(dir) {
			out[f.RuleID] = append(out[f.RuleID], f.Message)
		}
	}
	return out
}

func TestDefaultsPassTheirVerifiers(t *testing.T) {
	for _, fw := range []string{"minitest", "rspec"} {
		if got := findings(writeDefaults(t, fw), fw); len(got) > 0 {
			t.Fatalf("%s: default configuration reports %v", fw, got)
		}
	}
}

func TestTurningACopOffReports(t *testing.T) {
	cases := []struct {
		rule, old, new string
	}{
		{"code.swallowed-errors", "Lint/SuppressedException:\n", "Lint/SuppressedException:\n  Enabled: false\n"},
		{"code.swallowed-errors", "Style/RescueStandardError:\n", "Style/RescueStandardError:\n  Enabled: false\n"},
		{"code.debug-leftovers", "", "Lint:\n  Enabled: false\n"},
		{"code.debug-leftovers", "", "Lint/Debugger:\n  Enabled: false\n"},
		{"code.format", "", "Layout:\n  Enabled: false\n"},
		{"code.naming", "", "Naming:\n  Enabled: false\n"},
		{"code.complexity", "Metrics/CyclomaticComplexity:\n  Max: 15", "Metrics/CyclomaticComplexity:\n  Max: 40"},
		{"code.complexity", "", "Metrics:\n  Enabled: false\n"},
		{"code.dead", "", "Lint/UselessAssignment:\n  Enabled: false\n"},
		{"code.lint", "AllCops:\n", "AllCops:\n  DisabledByDefault: true\n"},
		{"tests.smells", "  - rubocop-minitest\n", ""},
		{"tests.smells", "Minitest/SkipWithoutReason:\n  Enabled: true", "Minitest/SkipWithoutReason:\n  Enabled: false"},
	}
	for _, c := range cases {
		t.Run(c.rule+"/"+c.new, func(t *testing.T) {
			dir := writeDefaults(t, "minitest")
			edit(t, dir, c.old, c.new)
			if got := findings(dir, "minitest"); len(got[c.rule]) == 0 {
				t.Fatalf("no %s finding; got %v", c.rule, got)
			}
		})
	}
}

func TestRSpecFocusOffReports(t *testing.T) {
	dir := writeDefaults(t, "rspec")
	edit(t, dir, "RSpec/Focus:\n  Enabled: true", "RSpec/Focus:\n  Enabled: false")
	if got := findings(dir, "rspec"); len(got["tests.smells"]) == 0 {
		t.Fatalf("no tests.smells finding; got %v", got)
	}
}

func TestSuppressionsMatchWithAndWithoutReason(t *testing.T) {
	cases := []struct {
		line, reason string
	}{
		{"def call # rubocop:disable Metrics/AbcSize -- one switch over the protocol", "one switch over the protocol"},
		{"# rubocop:disable Metrics/AbcSize, Metrics/MethodLength -- generated", "generated"},
		{"# rubocop:disable Metrics/AbcSize", ""},
		{"# rubocop:todo Lint/Debugger", ""},
	}
	var s stack.Suppression
	for _, x := range (Adapter{}).Suppressions() {
		if x.Kind == "rubocop:disable" {
			s = x
		}
	}
	for _, c := range cases {
		m := s.Pattern.FindStringSubmatch(c.line)
		if m == nil {
			t.Fatalf("no match for %q", c.line)
		}
		if got := strings.TrimSpace(m[s.Pattern.SubexpIndex("reason")]); got != c.reason {
			t.Errorf("%q: reason = %q, want %q", c.line, got, c.reason)
		}
	}
}

func TestCoverageRecipeMatchesTheHelper(t *testing.T) {
	m := (Adapter{}).Make(params("minitest"))
	joined := strings.Join(m.Coverage, "\n")
	for _, want := range []string{"grep -qs SimpleCov test/test_helper.rb", "COVERAGE=1 ", "diff-cover " + LcovReport} {
		if !strings.Contains(joined, want) {
			t.Errorf("coverage lacks %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(strings.Join(CoverageHelper("test"), "\n"), LcovReport) {
		t.Error("the helper does not write the report the recipe reads")
	}
}
