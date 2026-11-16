package checks

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/rojosinalma/pawl/internal/makefile"
	"github.com/rojosinalma/pawl/internal/report"
)

var (
	// pawlVerifyRe finds `pawl verify` in a recipe, called directly or through the PAWL variable.
	pawlVerifyRe = regexp.MustCompile(`(\$\(PAWL\)|\$\{PAWL\}|\bpawl)\s+verify\b`)
	// pawlOldCheckRe finds the v0.1 call `pawl check`, which named the verification and now names the gate.
	pawlOldCheckRe = regexp.MustCompile(`(\$\(PAWL\)|\$\{PAWL\}|\bpawl)\s+check\b`)
	// pawlAffectedRe finds `pawl affected` in a recipe or a variable the recipe expands.
	pawlAffectedRe = regexp.MustCompile(`(\$\(PAWL\)|\$\{PAWL\}|\bpawl)\s+affected\b`)
	// makeVarRe finds variable references in make syntax.
	makeVarRe = regexp.MustCompile(`\$[({]([A-Za-z_][A-Za-z0-9_]*)[)}]`)
)

// maxVarDepth bounds the variable expansion so a recursive definition cannot loop.
const maxVarDepth = 8

// checkTargets verifies repo.targets: the standard targets, `check` reaching `pawl verify`, and every step named by
// an enforced rule being a target reachable from `check` or `nightly`.
func (r *run) checkTargets() error {
	const rule = "repo.targets"
	if !r.active(rule) {
		return nil
	}
	r.mark(rule)
	mk, ok, err := makefile.Read(r.p.Root)
	if err != nil {
		return fmt.Errorf("makefile: %w", err)
	}
	if !ok {
		r.add(report.Errorf(rule, "Makefile", 0, "no Makefile: the standard targets (%s) are the entry point", strings.Join(makefile.Standard, ", ")).
			WithFix("run `pawl init`; it writes the missing targets"))
		return nil
	}
	for _, t := range mk.Missing(makefile.Standard) {
		r.add(report.Errorf(rule, mk.Path, 0, "target `%s` is missing", t).
			WithFix("add the target, or run `pawl init`; it adds missing targets and keeps existing ones"))
	}
	old := r.checkOldCalls(mk)
	if mk.Has("check") && !old["check"] && !recipeMatches(mk, "check", pawlVerifyRe, false) {
		r.add(report.Errorf(rule, mk.Path, mk.Targets["check"].Line, "target `check` does not run `pawl verify`").
			WithFix("add the recipe line `$(PAWL) verify` to the check target"))
	}
	r.checkSteps(mk)
	return nil
}

// checkOldCalls reports the ring targets whose own recipe still calls `pawl check` for the verification and returns
// them. Inside make `pawl check` runs the verification with a note, so such a project works until the recipe is fixed.
func (r *run) checkOldCalls(mk *makefile.Makefile) map[string]bool {
	old := map[string]bool{}
	for _, name := range []string{"check", "precommit", "nightly"} {
		t, ok := mk.Targets[name]
		if !ok || !recipeLinesMatch(mk, t.Recipe, pawlOldCheckRe, false) {
			continue
		}
		old[name] = true
		r.add(report.Errorf("repo.targets", mk.Path, t.Line, "target `%s` calls `pawl check`, which is now the gate; the verification is `pawl verify`", name).
			WithFix(`replace "check" with "verify" in the recipe`))
	}
	return old
}

// checkSteps reports every step of an enforced rule that is no target or is not reached by check or nightly.
func (r *run) checkSteps(mk *makefile.Makefile) {
	byStep := map[string][]string{}
	for id, e := range r.p.Manifest.Rules {
		if e.Enforced() {
			byStep[e.Step] = append(byStep[e.Step], id)
		}
	}
	for _, step := range r.p.Manifest.Steps() {
		rules := byStep[step]
		sort.Strings(rules)
		names := strings.Join(rules, ", ")
		switch {
		case !mk.Has(step):
			r.add(report.Errorf("repo.targets", mk.Path, 0, "step `%s` (%s) is not a make target", step, names).
				WithFix(fmt.Sprintf("add a `%s` target, or point the rules at an existing target in pawl.toml", step)))
		case step == "check" || step == "nightly":
		case !mk.Reaches("check", step) && !mk.Reaches("nightly", step):
			r.add(report.Errorf("repo.targets", mk.Path, mk.Targets[step].Line, "step `%s` (%s) is not reached by `make check` or `make nightly`", step, names).
				WithFix(fmt.Sprintf("add `%s` to the prerequisites of check or nightly", step)))
		}
	}
}

// recipeMatches reports whether the target's recipe, its prerequisites' recipes, and (with vars) the variables
// those recipes reference match the expression.
func recipeMatches(mk *makefile.Makefile, target string, re *regexp.Regexp, vars bool) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(name string) bool {
		if seen[name] {
			return false
		}
		seen[name] = true
		t, ok := mk.Targets[name]
		if !ok {
			return false
		}
		if recipeLinesMatch(mk, t.Recipe, re, vars) {
			return true
		}
		for _, d := range t.Deps {
			if walk(d) {
				return true
			}
		}
		return false
	}
	return walk(target)
}

// recipeLinesMatch reports whether one recipe line, or (with vars) a variable it references, matches the expression.
func recipeLinesMatch(mk *makefile.Makefile, recipe []string, re *regexp.Regexp, vars bool) bool {
	for _, line := range recipe {
		if re.MatchString(line) || (vars && varsMatch(mk, line, re, 0)) {
			return true
		}
	}
	return false
}

// varsMatch expands the variables a line references, recursively up to maxVarDepth, and matches their values.
func varsMatch(mk *makefile.Makefile, line string, re *regexp.Regexp, depth int) bool {
	if depth >= maxVarDepth {
		return false
	}
	for _, m := range makeVarRe.FindAllStringSubmatch(line, -1) {
		val, ok := mk.Vars[m[1]]
		if !ok {
			continue
		}
		if re.MatchString(val) || varsMatch(mk, val, re, depth+1) {
			return true
		}
	}
	return false
}

// namesTool reports whether the manifest's via list names the verifier's tool, either exactly ("golangci-lint/errcheck")
// or as the tool of one of its rules ("ast-grep" for "ast-grep/config-owner-go").
func namesTool(via []string, tool string) bool {
	for _, v := range via {
		if v == tool || strings.HasPrefix(v, tool+"/") {
			return true
		}
	}
	return false
}

// checkVerifiers runs every adapter verifier whose rule is enforced and whose tool the manifest names in via; a
// verifier for a tool the project replaced is skipped silently.
func (r *run) checkVerifiers() error {
	for _, a := range r.p.Adapters {
		for _, v := range a.Verifiers(r.p.Params) {
			e := r.p.Manifest.Rules[v.RuleID]
			if !r.want(v.RuleID) || !e.Enforced() || !namesTool(e.Via, v.Via) {
				continue
			}
			r.mark(v.RuleID)
			r.add(v.Check(r.p.Root)...)
		}
	}
	return nil
}

// checkSpeed verifies speed.affected (the test target scopes its run with `pawl affected`) and records speed.rings,
// whose three rings the manifest validation already checks.
func (r *run) checkSpeed() error {
	if r.active("speed.rings") {
		r.mark("speed.rings")
	}
	const rule = "speed.affected"
	if !r.active(rule) || len(r.p.StackAdapters()) == 0 || !r.enforced("tests.runner") {
		return nil
	}
	r.mark(rule)
	mk, ok, err := makefile.Read(r.p.Root)
	if err != nil {
		return fmt.Errorf("makefile: %w", err)
	}
	if !ok || !mk.Has("test") {
		return nil // repo.targets reports the missing Makefile or target
	}
	if !recipeMatches(mk, "test", pawlAffectedRe, true) {
		r.add(report.Warnf(rule, mk.Path, mk.Targets["test"].Line, "target `test` runs every test; the check ring runs the affected scope").
			WithFix("scope the test command with a variable such as `TEST_SCOPE ?= $(shell $(PAWL) affected <stack>)`"))
	}
	return nil
}
