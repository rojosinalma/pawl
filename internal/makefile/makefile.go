// Package makefile reads a project's Makefile for the standard targets and generates the targets a project lacks
// from the adapters' recipe fragments.
package makefile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rojosinalma/pawl/internal/stack"
)

// Standard are the targets every project has; pawl verifies them.
var Standard = []string{"check", "fmt", "fmt-check", "lint", "test", "precommit"}

// Extra are the targets pawl writes for the rings and the catalog beyond the five standard ones.
var Extra = []string{"coverage", "audit", "mutation", "nightly"}

// Makefile is a parsed Makefile.
type Makefile struct {
	Path    string
	Content string
	Targets map[string]Target
	Vars    map[string]string
}

// Target is one rule.
type Target struct {
	Name    string
	Deps    []string
	Recipe  []string
	Line    int
	Comment string
}

var (
	targetRe = regexp.MustCompile(`^([A-Za-z0-9_./-]+)\s*:(?:[^=]|$)(.*)$`)
	varRe    = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*[:?+]?=\s*(.*)$`)
)

// Read parses root/Makefile (or GNUmakefile, makefile). ok is false when none exists.
func Read(root string) (*Makefile, bool, error) {
	for _, name := range []string{"GNUmakefile", "makefile", "Makefile"} {
		path := filepath.Join(root, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		m := Parse(string(raw))
		m.Path = name
		return m, true, nil
	}
	return nil, false, nil
}

// Parse reads targets and variables from Makefile text. Recipes are the tab-indented lines after a rule.
func Parse(content string) *Makefile {
	m := &Makefile{Content: content, Targets: map[string]Target{}, Vars: map[string]string{}}
	var current *Target
	lines := strings.Split(content, "\n")
	lastComment := ""
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "\t"):
			if current != nil {
				current.Recipe = append(current.Recipe, strings.TrimPrefix(line, "\t"))
			}
			continue
		case strings.HasPrefix(strings.TrimSpace(line), "#"):
			lastComment = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
			continue
		case strings.TrimSpace(line) == "":
			lastComment = ""
			if current != nil {
				m.Targets[current.Name] = *current
				current = nil
			}
			continue
		}
		if current != nil {
			m.Targets[current.Name] = *current
			current = nil
		}
		if mv := varRe.FindStringSubmatch(line); mv != nil && !strings.Contains(mv[1], ":") {
			m.Vars[mv[1]] = strings.TrimSpace(mv[2])
			continue
		}
		if mt := targetRe.FindStringSubmatch(line); mt != nil {
			name := mt[1]
			if strings.HasPrefix(name, ".") {
				continue
			}
			rest := mt[2]
			if idx := strings.Index(rest, "##"); idx >= 0 {
				lastComment = strings.TrimSpace(rest[idx+2:])
				rest = rest[:idx]
			}
			if idx := strings.Index(rest, "#"); idx >= 0 {
				rest = rest[:idx]
			}
			t := Target{Name: name, Deps: strings.Fields(rest), Line: i + 1, Comment: lastComment}
			current = &t
			lastComment = ""
		}
	}
	if current != nil {
		m.Targets[current.Name] = *current
	}
	return m
}

// Has reports whether the target exists.
func (m *Makefile) Has(target string) bool {
	_, ok := m.Targets[target]
	return ok
}

// Missing returns the standard targets the Makefile lacks.
func (m *Makefile) Missing(targets []string) []string {
	var out []string
	for _, t := range targets {
		if !m.Has(t) {
			out = append(out, t)
		}
	}
	return out
}

// Reaches reports whether running `target` runs `wanted` (as a dependency, recursively, or in a recipe line).
func (m *Makefile) Reaches(target, wanted string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(name string) bool {
		if seen[name] {
			return false
		}
		seen[name] = true
		t, ok := m.Targets[name]
		if !ok {
			return false
		}
		for _, d := range t.Deps {
			if d == wanted || walk(d) {
				return true
			}
		}
		for _, r := range t.Recipe {
			if mentions(r, wanted) {
				return true
			}
		}
		return false
	}
	return walk(target)
}

// mentions reports whether the recipe line contains wanted as whole words: "lint" is in "$(MAKE) lint" but not in
// "golangci-lint run".
func mentions(line, wanted string) bool {
	for from := 0; ; {
		i := strings.Index(line[from:], wanted)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(wanted)
		if (start == 0 || !wordByte(line[start-1])) && (end == len(line) || !wordByte(line[end])) {
			return true
		}
		from = start + 1
	}
}

func wordByte(b byte) bool {
	return b == '-' || b == '_' || b == '.' || b == '/' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// DryRun returns the commands `make -n target` would run; it needs make on PATH.
func DryRun(root, target string) (string, error) {
	cmd := exec.Command("make", "-n", "--no-print-directory", target)
	cmd.Dir = root
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("make -n %s: %s", target, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// Generate renders the standard and extra targets from the active adapters.
func Generate(adapters []stack.Adapter, p stack.Params, diffCoverage int, base string) string {
	var frags []stack.Make
	for _, a := range adapters {
		frags = append(frags, a.Make(p))
	}
	var b strings.Builder
	b.WriteString("# Standard targets written by pawl (https://github.com/rojosinalma/pawl). Project-owned: edit freely.\n")
	b.WriteString("# `make check` is the merge gate; `make precommit` runs from .githooks/pre-commit on staged files.\n")
	b.WriteString(".DEFAULT_GOAL := check\n")
	b.WriteString("SHELL := /bin/sh\n")
	b.WriteString(".PHONY: " + strings.Join(append(append([]string{}, Standard...), Extra...), " ") + "\n\n")
	fmt.Fprintf(&b, "PAWL_BASE ?= %s\n", base)
	fmt.Fprintf(&b, "DIFF_COVERAGE ?= %d\n", diffCoverage)
	for _, f := range frags {
		for _, v := range f.Vars {
			b.WriteString(v + "\n")
		}
	}
	b.WriteString("\n")

	rule := func(name, comment string, deps []string, lines ...[]string) {
		fmt.Fprintf(&b, "%s:%s  ## %s\n", name, depString(deps), comment)
		n := 0
		for _, group := range lines {
			for _, l := range group {
				b.WriteString("\t" + l + "\n")
				n++
			}
		}
		if n == 0 {
			b.WriteString("\t@true\n")
		}
		b.WriteString("\n")
	}
	collect := func(pick func(stack.Make) []string) []string {
		var out []string
		for _, f := range frags {
			out = append(out, pick(f)...)
		}
		return out
	}

	rule("check", "the merge gate: lint, tests, then the catalog verification", []string{"lint", "test"}, []string{"$(PAWL) check"})
	rule("fmt", "rewrite files with the formatters", nil, collect(func(f stack.Make) []string { return f.Fmt }))
	rule("fmt-check", "fail when a file is not formatted", nil, collect(func(f stack.Make) []string { return f.FmtCheck }))
	rule("lint", "every linter, type checker and pattern rule on the whole tree", []string{"fmt-check"}, collect(func(f stack.Make) []string { return f.Lint }))
	rule("test", "the test suites; PAWL_FULL=1 runs everything, otherwise the affected scope", nil, collect(func(f stack.Make) []string { return f.Test }))
	rule("precommit", "staged files only, under ten seconds; run by the git hook", nil, collect(func(f stack.Make) []string { return f.Precommit }), []string{"$(PAWL) check --ring precommit"})
	rule("coverage", "tests with coverage; changed lines must reach DIFF_COVERAGE percent", nil, collect(func(f stack.Make) []string { return f.Coverage }))
	rule("audit", "dependency vulnerability audits", nil, collect(func(f stack.Make) []string { return f.Audit }))
	rule("mutation", "mutation testing on the change (nightly)", nil, collect(func(f stack.Make) []string { return f.Mutation }))
	rule("nightly", "everything, including the slow rings", []string{"check", "coverage", "audit", "mutation"}, []string{"$(PAWL) check --ring nightly", "$(PAWL) budget"})
	return b.String()
}

func depString(deps []string) string {
	if len(deps) == 0 {
		return ""
	}
	return " " + strings.Join(deps, " ")
}

// Merge appends the missing targets of generated to existing, keeping every existing line. Variables the generated
// text defines and the existing Makefile lacks are added before the first appended target.
func Merge(existing string, generated string) string {
	have := Parse(existing)
	gen := Parse(generated)
	var names []string
	for name := range gen.Targets {
		if !have.Has(name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return existing
	}
	order := append(append([]string{}, Standard...), Extra...)
	sort.Slice(names, func(i, j int) bool { return indexOf(order, names[i]) < indexOf(order, names[j]) })

	var b strings.Builder
	b.WriteString(strings.TrimRight(existing, "\n"))
	b.WriteString("\n\n# --- targets added by pawl init; project-owned ---\n")
	var vars []string
	for name, val := range gen.Vars {
		if _, ok := have.Vars[name]; !ok {
			vars = append(vars, name+" ?= "+val)
		}
	}
	sort.Strings(vars)
	for _, v := range vars {
		b.WriteString(v + "\n")
	}
	if len(vars) > 0 {
		b.WriteString("\n")
	}
	b.WriteString(".PHONY: " + strings.Join(names, " ") + "\n\n")
	for _, name := range names {
		t := gen.Targets[name]
		fmt.Fprintf(&b, "%s:%s  ## %s\n", name, depString(t.Deps), t.Comment)
		for _, r := range t.Recipe {
			b.WriteString("\t" + r + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return len(list)
}
