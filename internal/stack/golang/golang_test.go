package golang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/stack"
)

var params = stack.Params{Complexity: 15, FileLines: 800, DiffCoverage: 80}

// writeConfigs writes the default configurations into a temporary root, applying edit to .golangci.yml.
func writeConfigs(t *testing.T, edit func(string) string) string {
	t.Helper()
	root := t.TempDir()
	for _, c := range (Adapter{}).Configs(params) {
		content := c.Content
		if c.Path == ".golangci.yml" && edit != nil {
			content = edit(content)
		}
		path := filepath.Join(root, c.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// failing returns "rule via" for every verifier that reports on root.
func failing(root string) []string {
	var out []string
	for _, v := range (Adapter{}).Verifiers(params) {
		if len(v.Check(root)) > 0 {
			out = append(out, v.RuleID+" "+v.Via)
		}
	}
	return out
}

func TestDefaultConfigPassesVerifiers(t *testing.T) {
	if got := failing(writeConfigs(t, nil)); len(got) > 0 {
		t.Fatalf("default config fails its own verifiers: %v", got)
	}
}

func TestVerifiersReportRuleTurnedOff(t *testing.T) {
	replace := func(old, new string) func(string) string {
		return func(s string) string {
			if !strings.Contains(s, old) {
				t.Fatalf("default config lacks %q", old)
			}
			return strings.Replace(s, old, new, 1)
		}
	}
	cases := []struct {
		name string
		edit func(string) string
		want string
	}{
		{"gocognit removed", replace("    - gocognit", ""), "code.complexity golangci-lint/gocognit"},
		{"threshold raised", replace("min-complexity: 15", "min-complexity: 40"), "code.complexity golangci-lint/gocognit"},
		{"errcheck disabled", replace("  enable:\n", "  disable: [errcheck]\n  enable:\n"), "code.dropped-results golangci-lint/errcheck"},
		{"default none", replace("default: standard", "default: none"), "code.dead golangci-lint/unused"},
		{"nilerr removed", replace("    - nilerr", ""), "code.swallowed-errors golangci-lint/nilerr"},
		{"forbidigo removed", replace("    - forbidigo", ""), "code.debug-leftovers golangci-lint/forbidigo"},
		{"forbid pattern removed", replace("        - pattern: ^fmt", "        - pattern: ^log"), "code.debug-leftovers golangci-lint/forbidigo"},
		{"goimports removed", replace("    - goimports", ""), "code.format golangci-lint/goimports"},
		{"version 1", replace(`version: "2"`, `version: "1"`), "code.lint golangci-lint"},
		{"gosec removed", replace("    - gosec", ""), "drift.security golangci-lint/gosec"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := failing(writeConfigs(t, c.edit))
			found := false
			for _, g := range got {
				found = found || g == c.want
			}
			if !found {
				t.Fatalf("want %q among failing verifiers, got %v", c.want, got)
			}
		})
	}
}

func TestDefaultAllKeepsLintersOn(t *testing.T) {
	root := writeConfigs(t, func(s string) string { return strings.Replace(s, "default: standard", "default: all", 1) })
	if got := failing(root); len(got) > 0 {
		t.Fatalf("linters.default: all fails verifiers: %v", got)
	}
}

func TestSuppressions(t *testing.T) {
	cases := []struct {
		line, kind, reason string
	}{
		{"x() //nolint:errcheck // the error is logged by the caller", "nolint", "the error is logged by the caller"},
		{"x() //nolint:errcheck,gosec", "nolint", ""},
		{"x() //nolint", "nolint", ""},
		{"x() // #nosec G104 -- the file is a fixed path", "nosec", "the file is a fixed path"},
		{"x() // #nosec", "nosec", ""},
		{"//lint:ignore SA1019 the old API is needed for Go 1.20", "lint:ignore", "the old API is needed for Go 1.20"},
		{"//lint:ignore SA1019", "lint:ignore", ""},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			kind, reason, ok := match(c.line)
			if !ok || kind != c.kind || reason != c.reason {
				t.Fatalf("got (%q, %q, %v), want (%q, %q)", kind, reason, ok, c.kind, c.reason)
			}
		})
	}
	if _, _, ok := match(`msg := "no directive here"`); ok {
		t.Fatal("plain code matched a suppression")
	}
}

// match returns the kind and reason of the first suppression that matches the line.
func match(line string) (kind, reason string, ok bool) {
	for _, s := range (Adapter{}).Suppressions() {
		m := s.Pattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if i := s.Pattern.SubexpIndex("reason"); i >= 0 {
			reason = strings.TrimSpace(m[i])
		}
		return s.Kind, reason, true
	}
	return "", "", false
}
