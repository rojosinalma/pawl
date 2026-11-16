// Package stacktest holds the helpers the adapter tests share: write an adapter's configurations into a temporary
// root, run its verifiers, and match its suppression patterns.
package stacktest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/stack"
	"github.com/rojosinalma/pawl/internal/testrepo"
)

// Params are the defaults a fresh pawl.toml carries.
var Params = stack.Params{Complexity: 15, FileLines: 800, DiffCoverage: 80, Facts: map[string]string{}}

// Edit changes the content of one configuration file before it is written.
type Edit func(t *testing.T, content string) string

// Replace returns an Edit that replaces the first occurrence of old; the test fails when the file lacks it.
func Replace(old, replacement string) Edit {
	return func(t *testing.T, content string) string {
		t.Helper()
		if !strings.Contains(content, old) {
			t.Fatalf("default config lacks %q", old)
		}
		return strings.Replace(content, old, replacement, 1)
	}
}

// Write writes the configurations into a temporary root, applying the edits by path, and returns the root.
func Write(t *testing.T, files []stack.ConfigFile, edits map[string]Edit) string {
	t.Helper()
	root := t.TempDir()
	for _, c := range files {
		content := c.Content
		if e := edits[c.Path]; e != nil {
			content = e(t, content)
		}
		testrepo.Write(t, root, c.Path, content)
	}
	return root
}

// EditFile replaces the first occurrence of old in root/rel with replacement; the test fails when the file lacks it.
func EditFile(t *testing.T, root, rel, old, replacement string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), old) {
		t.Fatalf("%s lacks %q", rel, old)
	}
	testrepo.Write(t, root, rel, strings.Replace(string(raw), old, replacement, 1))
}

// Findings runs the verifiers on root and returns their messages by rule id.
func Findings(root string, verifiers []stack.Verifier) map[string][]string {
	out := map[string][]string{}
	for _, v := range verifiers {
		for _, f := range v.Check(root) {
			out[f.RuleID] = append(out[f.RuleID], f.Message)
		}
	}
	return out
}

// Failing returns "<rule> <via>" for every verifier that reports on root.
func Failing(root string, verifiers []stack.Verifier) []string {
	var out []string
	for _, v := range verifiers {
		if len(v.Check(root)) > 0 {
			out = append(out, v.RuleID+" "+v.Via)
		}
	}
	return out
}

// Contains reports whether list holds s.
func Contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Match returns the kind and the trimmed reason of the first suppression that matches line.
func Match(suppressions []stack.Suppression, line string) (kind, reason string, ok bool) {
	for _, s := range suppressions {
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

// SuppressionCase is one line and the suppression it must match; Kind "" means no match.
type SuppressionCase struct {
	Line, Kind, Reason string
}

// CheckSuppressions runs the cases against the suppressions.
func CheckSuppressions(t *testing.T, suppressions []stack.Suppression, cases []SuppressionCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.Line, func(t *testing.T) {
			kind, reason, ok := Match(suppressions, c.Line)
			if c.Kind == "" {
				if ok {
					t.Fatalf("matched %q, want no suppression", kind)
				}
				return
			}
			if !ok || kind != c.Kind || reason != c.Reason {
				t.Fatalf("got (%q, %q, %v), want (%q, %q)", kind, reason, ok, c.Kind, c.Reason)
			}
		})
	}
}

// RuleOff is a configuration edit and the verifier ("<rule> <via>") that must report it.
type RuleOff struct {
	Name  string
	Edits map[string]Edit
	Want  string
}

// CheckRulesOff writes the configurations with each case's edits and expects the named verifier to report.
func CheckRulesOff(t *testing.T, files []stack.ConfigFile, verifiers []stack.Verifier, cases []RuleOff) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got := Failing(Write(t, files, c.Edits), verifiers)
			if !Contains(got, c.Want) {
				t.Fatalf("want %q among failing verifiers, got %v", c.Want, got)
			}
		})
	}
}

// CheckDefaults expects the configurations to pass every verifier.
func CheckDefaults(t *testing.T, files []stack.ConfigFile, verifiers []stack.Verifier) {
	t.Helper()
	if got := Failing(Write(t, files, nil), verifiers); len(got) > 0 {
		t.Fatalf("default config fails its own verifiers: %v", got)
	}
}
