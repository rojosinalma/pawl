package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/stack"
)

func params() stack.Params {
	return stack.Params{Complexity: 15, FileLines: 400, DiffCoverage: 80, Facts: map[string]string{"runtime": "bun", "typescript": "true"}}
}

const tsconfig = `{
  // the project's own file; init does not write it
  "compilerOptions": { "strict": true, "noEmit": true },
}
`

// writeDefaults writes what init writes plus a strict tsconfig.json and returns the directory.
func writeDefaults(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, c := range (Adapter{}).Configs(params()) {
		write(t, dir, c.Path, c.Content)
	}
	write(t, dir, "tsconfig.json", tsconfig)
	return dir
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// edit replaces old with new in dir/rel and fails when old is absent.
func edit(t *testing.T, dir, rel, old, new string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), old) {
		t.Fatalf("%s lacks %q", rel, old)
	}
	write(t, dir, rel, strings.Replace(string(raw), old, new, 1))
}

// findings runs every verifier on dir and returns the rule ids that reported.
func findings(dir string) map[string][]string {
	out := map[string][]string{}
	for _, v := range (Adapter{}).Verifiers(params()) {
		for _, f := range v.Check(dir) {
			out[f.RuleID] = append(out[f.RuleID], f.Message)
		}
	}
	return out
}

func TestDefaultsPassTheirVerifiers(t *testing.T) {
	if got := findings(writeDefaults(t)); len(got) > 0 {
		t.Fatalf("default configuration reports %v", got)
	}
}

func TestTurningARuleOffReports(t *testing.T) {
	cases := []struct {
		rule, file, old, new string
	}{
		{"code.debug-leftovers", "biome.jsonc", `"noConsole": "error"`, `"noConsole": "off"`},
		{"code.swallowed-errors", "biome.jsonc", `"noEmptyBlockStatements": "error"`, `"noEmptyBlockStatements": "off"`},
		{"resilience.import-cycles", "biome.jsonc", `"noImportCycles": "error"`, `"noImportCycles": "off"`},
		{"tests.smells", "biome.jsonc", `"noFocusedTests": "error"`, `"noFocusedTests": "off"`},
		{"tests.smells", "biome.jsonc", `"noSkippedTests": "error",`, ``},
		{"code.complexity", "biome.jsonc", `"level": "error",
          "options": { "maxAllowedComplexity"`, `"level": "off",
          "options": { "maxAllowedComplexity"`},
		{"code.complexity", "biome.jsonc", `"maxAllowedComplexity": 15`, `"maxAllowedComplexity": 40`},
		{"code.lint", "biome.jsonc", `"linter": {
    "enabled": true`, `"linter": {
    "enabled": false`},
		{"code.format", "biome.jsonc", `"formatter": { "enabled": true`, `"formatter": { "enabled": false`},
		{"code.dropped-results", ".oxlintrc.json", `"typeAware": true`, `"typeAware": false`},
		{"code.dropped-results", ".oxlintrc.json", `"typescript/no-floating-promises": "error"`, `"typescript/no-floating-promises": "warn"`},
		{"code.types", "tsconfig.json", `"strict": true`, `"strict": false`},
	}
	for _, c := range cases {
		t.Run(c.rule+"/"+c.new, func(t *testing.T) {
			dir := writeDefaults(t)
			edit(t, dir, c.file, c.old, c.new)
			if got := findings(dir); len(got[c.rule]) == 0 {
				t.Fatalf("no %s finding after %s; got %v", c.rule, c.new, got)
			}
		})
	}
}

func TestMissingConfigReports(t *testing.T) {
	dir := writeDefaults(t)
	if err := os.Remove(filepath.Join(dir, "biome.jsonc")); err != nil {
		t.Fatal(err)
	}
	if got := findings(dir); len(got["code.lint"]) == 0 {
		t.Fatalf("no code.lint finding without biome.jsonc; got %v", got)
	}
}

func TestSuppressionsMatchWithAndWithoutReason(t *testing.T) {
	cases := []struct {
		kind, line string
		reason     string // "" means the directive has no reason
	}{
		{"biome-ignore", "// biome-ignore lint/suspicious/noConsole: the CLI prints its result", "the CLI prints its result"},
		{"biome-ignore", "// biome-ignore lint/suspicious/noConsole", ""},
		{"biome-ignore", "// biome-ignore-all lint/style/useNamingConvention: generated names", "generated names"},
		{"ts-expect-error", "// @ts-expect-error the library types lag behind", "the library types lag behind"},
		{"ts-expect-error", "// @ts-expect-error", ""},
		{"ts-ignore", "// @ts-ignore", ""},
		{"oxlint-disable", "// oxlint-disable-next-line typescript/no-floating-promises -- fire and forget by design", "fire and forget by design"},
		{"eslint-disable", "/* eslint-disable no-console */", ""},
		{"as-any", "const x = y as any;", ""},
		{"as-any", "const x = y as any; // the decoder returns unknown shapes", "the decoder returns unknown shapes"},
	}
	byKind := map[string]stack.Suppression{}
	for _, s := range (Adapter{}).Suppressions() {
		byKind[s.Kind] = s
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			s, ok := byKind[c.kind]
			if !ok {
				t.Fatalf("no suppression kind %q", c.kind)
			}
			m := s.Pattern.FindStringSubmatch(c.line)
			if m == nil {
				t.Fatalf("%s does not match %q", c.kind, c.line)
			}
			if got := strings.TrimSpace(m[s.Pattern.SubexpIndex("reason")]); got != c.reason {
				t.Fatalf("reason = %q, want %q", got, c.reason)
			}
		})
	}
}

func TestMakeUsesTheProjectTypeScript(t *testing.T) {
	m := (Adapter{}).Make(params())
	joined := strings.Join(m.Lint, "\n")
	for _, want := range []string{"node_modules/.bin/tsc --noEmit", "oxlint .", "biome ci", "knip"} {
		if !strings.Contains(joined, want) {
			t.Errorf("lint lacks %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(strings.Join(m.Coverage, "\n"), "$(REPORTS)/bun/lcov.info") {
		t.Errorf("coverage does not read bun's lcov report: %v", m.Coverage)
	}
}
