// Package makefile reads a project's Makefile for the standard targets and generates the targets a project lacks
// from the adapters' recipe fragments.
package makefile

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

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
	// Defines holds each `define NAME` block, from the define line to endef, by name.
	Defines map[string]string
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
	p := &parser{m: &Makefile{Content: content, Targets: map[string]Target{}, Vars: map[string]string{}, Defines: map[string]string{}}}
	for i, line := range strings.Split(content, "\n") {
		p.line(i+1, line)
	}
	p.flush()
	return p.m
}

// parser carries the state between lines: the rule whose recipe is being read and the comment above the next rule.
type parser struct {
	m           *Makefile
	current     *Target
	lastComment string
	define      string          // the name of the define block being read, if any
	block       strings.Builder // its text so far
}

func (p *parser) line(n int, line string) {
	if p.defineLine(line) {
		return
	}
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "\t"):
		if p.current != nil {
			p.current.Recipe = append(p.current.Recipe, strings.TrimPrefix(line, "\t"))
		}
		return
	case strings.HasPrefix(trimmed, "#"):
		p.lastComment = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		return
	case trimmed == "":
		p.lastComment = ""
		p.flush()
		return
	}
	p.flush()
	if mv := varRe.FindStringSubmatch(line); mv != nil && !strings.Contains(mv[1], ":") {
		p.m.Vars[mv[1]] = strings.TrimSpace(mv[2])
		return
	}
	if mt := targetRe.FindStringSubmatch(line); mt != nil {
		p.target(n, mt[1], mt[2])
	}
}

// defineLine reads the lines of a `define` block, whose body is make text and no rule; it reports whether the line
// belonged to one.
func (p *parser) defineLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if p.define == "" {
		name, ok := strings.CutPrefix(trimmed, "define ")
		if !ok {
			return false
		}
		p.flush()
		p.define = strings.Fields(name)[0]
		p.block.Reset()
	}
	p.block.WriteString(line + "\n")
	if trimmed == "endef" {
		p.m.Defines[p.define] = p.block.String()
		p.define = ""
	}
	return true
}

// target starts a rule; special targets such as .PHONY are not rules a project runs and are skipped.
func (p *parser) target(n int, name, rest string) {
	if strings.HasPrefix(name, ".") {
		return
	}
	if idx := strings.Index(rest, "##"); idx >= 0 {
		p.lastComment = strings.TrimSpace(rest[idx+2:])
		rest = rest[:idx]
	}
	if idx := strings.Index(rest, "#"); idx >= 0 {
		rest = rest[:idx]
	}
	p.current = &Target{Name: name, Deps: strings.Fields(rest), Line: n, Comment: p.lastComment}
	p.lastComment = ""
}

// flush stores the rule being read, if any.
func (p *parser) flush() {
	if p.current != nil {
		p.m.Targets[p.current.Name] = *p.current
		p.current = nil
	}
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

// Recipes returns the recipe lines of target and of every target it depends on, directly or not.
func (m *Makefile) Recipes(target string) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		t, ok := m.Targets[name]
		if !ok {
			return
		}
		out = append(out, t.Recipe...)
		for _, d := range t.Deps {
			walk(d)
		}
	}
	walk(target)
	return out
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

// dryRunTimeout bounds `make -n`, which only prints commands but can hang on a recursive or shell-heavy Makefile.
const dryRunTimeout = 60 * time.Second

// DryRun returns the commands `make -n target` would run; it needs make on PATH.
func DryRun(root, target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dryRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "-n", "--no-print-directory", target)
	cmd.Dir = root
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("make -n %s: %s", target, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// guards are the macros every call to pawl in a generated recipe goes through. They test for pawl when the recipe
// runs, not when make reads the file: a project may build pawl as a prerequisite of its rings.
const guards = `# Pawl is optional: every target works without it. Without pawl, $(call pawl_run,<args>) skips ` + "`pawl <args>`" + `
# with a warning and ` + "`make test`" + ` runs every test; PAWL_REQUIRED=1 (the CI workflows set it) makes that an error.
# $(call pawl_guard,<command run when pawl is installed>,<what happens instead, for the warning>)
define pawl_guard
if command -v $(firstword $(PAWL)) >/dev/null 2>&1; then $(1); elif [ "$(PAWL_REQUIRED)" = 1 ]; then echo "make $@: pawl is not installed and PAWL_REQUIRED=1" >&2; exit 1; else echo "make $@: warning: pawl is not installed; $(2)" >&2; fi
endef
pawl_run = $(call pawl_guard,$(PAWL) $(1),skipped: pawl $(1))
`

// testGuard is the first line of the test recipe: the scope variables already fall back to every test without pawl,
// and this line says so (or fails under PAWL_REQUIRED=1).
const testGuard = "@$(call pawl_guard,:,every test runs)"

// PawlRun returns the recipe line that runs `pawl <args>` through the guard macro.
func PawlRun(args string) string { return "@$(call pawl_run," + args + ")" }

// Generate renders the standard and extra targets from the active adapters.
func Generate(adapters []stack.Adapter, p stack.Params, diffCoverage int, base string) string {
	g := &generator{}
	for _, a := range adapters {
		g.frags = append(g.frags, a.Make(p))
	}
	lintLines := g.collect(func(f stack.Make) []string { return f.Lint })
	lintSteps := stepNames("lint", lintLines, g.stepNames())
	g.header(lintSteps, p.Exclude, diffCoverage, base)

	g.rule("check", "the merge gate: lint, tests, then the catalog verification", []string{"lint", "test"}, []string{PawlRun("verify")})
	g.rule("fmt", "rewrite files with the formatters", nil, g.collect(func(f stack.Make) []string { return f.Fmt }))
	g.rule("fmt-check", "fail when a file is not formatted", nil, g.collect(func(f stack.Make) []string { return f.FmtCheck }))
	// one target per linter, so `make -k lint` reports every tool's findings instead of stopping at the first
	g.rule("lint", "every linter, type checker and pattern rule on the whole tree; `make -k lint` reports every step", append([]string{"fmt-check"}, lintSteps...))
	for i, name := range lintSteps {
		g.rule(name, "one step of lint", nil, []string{lintLines[i]})
	}
	g.rule("test", "the test suites; PAWL_FULL=1 runs everything, otherwise the affected scope", nil, []string{testGuard}, g.collect(func(f stack.Make) []string { return f.Test }))
	g.rule("precommit", "staged files only, under ten seconds; run by the git hook", nil, g.collect(func(f stack.Make) []string { return f.Precommit }), []string{PawlRun("verify --ring precommit")})
	g.rule("coverage", "tests with coverage; changed lines must reach DIFF_COVERAGE percent", nil, g.collect(func(f stack.Make) []string { return f.Coverage }))
	g.rule("audit", "dependency vulnerability audits", nil, g.collect(func(f stack.Make) []string { return f.Audit }))
	g.rule("mutation", "mutation testing on the change (nightly)", nil, g.collect(func(f stack.Make) []string { return f.Mutation }))
	g.rule("nightly", "everything, including the slow rings", []string{"check", "coverage", "audit", "mutation"}, []string{PawlRun("verify --ring nightly"), PawlRun("budget")})
	return g.b.String()
}

// generator writes the Makefile text from the adapters' fragments.
type generator struct {
	b     strings.Builder
	frags []stack.Make
}

// collect joins one recipe part of every fragment, in adapter order.
func (g *generator) collect(pick func(stack.Make) []string) []string {
	var out []string
	for _, f := range g.frags {
		out = append(out, pick(f)...)
	}
	return out
}

// stepNames joins the step names the adapters give their lint lines.
func (g *generator) stepNames() map[string]string {
	named := map[string]string{}
	for _, f := range g.frags {
		for line, name := range f.StepNames {
			named[line] = name
		}
	}
	return named
}

// header writes the comment, the make settings, the phony list and the variables above the first rule.
func (g *generator) header(lintSteps, exclude []string, diffCoverage int, base string) {
	b := &g.b
	b.WriteString("# Standard targets written by pawl (https://github.com/rojosinalma/pawl). Project-owned: edit freely.\n")
	b.WriteString("# `make check` is the merge gate; `make precommit` runs from .githooks/pre-commit on staged files.\n")
	b.WriteString(".DEFAULT_GOAL := check\n")
	b.WriteString("SHELL := /bin/sh\n")
	b.WriteString(".PHONY: " + strings.Join(append(append(append([]string{}, Standard...), Extra...), lintSteps...), " ") + "\n\n")
	b.WriteString("# Paths the file-list tools skip, as an extended regular expression; keep it in step with project.exclude in pawl.toml.\n")
	fmt.Fprintf(b, "PAWL_EXCLUDE ?= %s\n", excludeRegex(exclude))
	fmt.Fprintf(b, "PAWL_BASE ?= %s\n", base)
	fmt.Fprintf(b, "DIFF_COVERAGE ?= %d\n", diffCoverage)
	for _, v := range g.collect(func(f stack.Make) []string { return f.Vars }) {
		b.WriteString(v + "\n")
	}
	b.WriteString("\n" + guards + "\n")
}

// rule writes one target; a target without recipe lines runs @true so make does not report it as having nothing to do.
func (g *generator) rule(name, comment string, deps []string, lines ...[]string) {
	fmt.Fprintf(&g.b, "%s:%s  ## %s\n", name, depString(deps), comment)
	n := 0
	for _, group := range lines {
		for _, l := range group {
			g.b.WriteString("\t" + l + "\n")
			n++
		}
	}
	if n == 0 {
		g.b.WriteString("\t@true\n")
	}
	g.b.WriteString("\n")
}

// excludeRegex turns the manifest's exclude prefixes into the regular expression of PAWL_EXCLUDE; without excludes
// it matches nothing a path can be.
func excludeRegex(prefixes []string) string {
	var parts []string
	for _, p := range prefixes {
		if p = strings.TrimSuffix(filepath.ToSlash(p), "/"); p != "" {
			parts = append(parts, regexp.QuoteMeta(p)+"/")
		}
	}
	if len(parts) == 0 {
		return "^$$"
	}
	return "^(" + strings.Join(parts, "|") + ")"
}

// stepNames names one sub-target per recipe line after the tool it runs ("lint-biome"), numbering repeats.
func stepNames(prefix string, lines []string, named map[string]string) []string {
	seen := map[string]int{}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		tool := named[l]
		if tool == "" {
			tool = toolName(l)
		}
		name := prefix + "-" + tool
		seen[name]++
		if n := seen[name]; n > 1 {
			name = fmt.Sprintf("%s-%d", name, n)
		}
		out = append(out, name)
	}
	return out
}

// launchers are command words that run another tool; the tool after them names the step.
var launchers = map[string]bool{"bundle": true, "exec": true, "uv": true, "run": true, "bun": true, "x": true, "npx": true, "env": true}

var (
	nameChars = regexp.MustCompile(`[^a-z0-9-]+`)
	toolStart = regexp.MustCompile(`^[A-Za-z_./]`) // not a variable, flag, quote or shell operator
)

// toolName returns the program a recipe line runs: the first word that is not a launcher, variable or assignment.
func toolName(line string) string {
	for _, w := range strings.Fields(strings.TrimLeft(line, "@-+")) {
		if launchers[w] || strings.Contains(w, "=") || !toolStart.MatchString(w) {
			continue
		}
		if name := strings.Trim(nameChars.ReplaceAllString(strings.ToLower(filepath.Base(w)), "-"), "-"); name != "" {
			return name
		}
	}
	return "step"
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
		if !have.Has(name) && !onlyFor(name, gen, have) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return existing
	}
	order := append(append([]string{}, Standard...), Extra...)
	sort.Strings(names)
	sort.SliceStable(names, func(i, j int) bool { return indexOf(order, names[i]) < indexOf(order, names[j]) })

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
	b.WriteString(missingDefines(have, gen))
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

// missingDefines returns the define blocks of gen that have lacks, in name order: the appended targets call them.
func missingDefines(have, gen *Makefile) string {
	var names []string
	for name := range gen.Defines {
		if _, ok := have.Defines[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(gen.Defines[name] + "\n")
	}
	return b.String()
}

// onlyFor reports whether name is a generated helper target (not a standard or extra one) that only targets the
// existing Makefile already has depend on, such as lint-biome when the project keeps its own lint.
func onlyFor(name string, gen, have *Makefile) bool {
	if indexOf(Standard, name) < len(Standard) || indexOf(Extra, name) < len(Extra) {
		return false
	}
	needed := false
	for _, t := range gen.Targets {
		for _, d := range t.Deps {
			if d == name {
				needed = true
				if !have.Has(t.Name) {
					return false
				}
			}
		}
	}
	return needed
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return len(list)
}
