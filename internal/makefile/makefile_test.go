package makefile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/stack"
)

const sample = `# project makefile
GO ?= go
NAME := app

.PHONY: build check

build: deps  ## compile
	$(GO) build ./...

deps:
	$(GO) mod download

# the gate
check: lint test
	pawl verify

lint: fmt-check
	golangci-lint run

fmt-check:
	gofmt -l .

test:
	$(GO) test ./...
`

func TestParseTargetsVarsAndRecipes(t *testing.T) {
	m := Parse(sample)
	if m.Vars["GO"] != "go" || m.Vars["NAME"] != "app" {
		t.Errorf("Vars = %v", m.Vars)
	}
	build, ok := m.Targets["build"]
	if !ok || len(build.Deps) != 1 || build.Deps[0] != "deps" || build.Comment != "compile" || len(build.Recipe) != 1 {
		t.Errorf("build = %+v", build)
	}
	if m.Has(".PHONY") {
		t.Error("special targets are not rules")
	}
	if m.Targets["check"].Comment != "the gate" {
		t.Errorf("comment above a target not kept: %+v", m.Targets["check"])
	}
	if got := m.Missing(Standard); len(got) != 2 || got[0] != "fmt" || got[1] != "precommit" {
		t.Errorf("Missing = %v", got)
	}
}

func TestReaches(t *testing.T) {
	m := Parse(sample)
	cases := []struct {
		from, to string
		want     bool
	}{
		{"check", "lint", true},
		{"check", "fmt-check", true},
		{"check", "pawl verify", true},
		{"check", "deps", false},
		{"lint", "lint", false},
		{"check", "golangci-lint run", true},
		{"build", "deps", true},
		{"nope", "lint", false},
	}
	for _, c := range cases {
		if got := m.Reaches(c.from, c.to); got != c.want {
			t.Errorf("Reaches(%s, %s) = %v", c.from, c.to, got)
		}
	}
}

func TestReadFindsTheMakefile(t *testing.T) {
	root := t.TempDir()
	if _, ok, err := Read(root); ok || err != nil {
		t.Fatalf("Read on an empty directory: ok=%v err=%v", ok, err)
	}
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	m, ok, err := Read(root)
	if err != nil || !ok || m.Path != "Makefile" || !m.Has("check") {
		t.Fatalf("Read: ok=%v err=%v m=%+v", ok, err, m)
	}
}

type fakeAdapter struct{ stack.Adapter }

func (fakeAdapter) Make(stack.Params) stack.Make {
	return stack.Make{
		Vars:      []string{"FAKE ?= fake"},
		Fmt:       []string{"$(FAKE) fmt"},
		FmtCheck:  []string{"$(FAKE) fmt --check"},
		Lint:      []string{"$(FAKE) lint"},
		Test:      []string{"$(FAKE) test"},
		Precommit: []string{"$(FAKE) lint $(STAGED)"},
	}
}

func TestGenerateHasEveryTargetAndParses(t *testing.T) {
	text := Generate([]stack.Adapter{fakeAdapter{}}, stack.Params{}, 80, "origin/main")
	m := Parse(text)
	for _, target := range append(append([]string{}, Standard...), Extra...) {
		if !m.Has(target) {
			t.Errorf("generated Makefile lacks %s", target)
		}
	}
	if !m.Reaches("check", "lint") || !m.Reaches("check", "$(PAWL) verify") || !m.Reaches("nightly", "coverage") {
		t.Error("generated dependency graph is wrong")
	}
	for target, want := range map[string]string{"check": "$(PAWL) verify", "precommit": "$(PAWL) verify --ring precommit", "nightly": "$(PAWL) verify --ring nightly"} {
		if got := m.Targets[target].Recipe; !slices.Contains(got, want) {
			t.Errorf("%s recipe %v lacks %q", target, got, want)
		}
	}
	if m.Vars["PAWL_BASE"] != "origin/main" || m.Vars["DIFF_COVERAGE"] != "80" || m.Vars["FAKE"] != "fake" {
		t.Errorf("Vars = %v", m.Vars)
	}
	if got := m.Targets["coverage"].Recipe; len(got) != 1 || got[0] != "@true" {
		t.Errorf("a target without recipes must be a no-op, got %v", got)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "    ") {
			t.Errorf("recipe indented with spaces: %q", line)
		}
	}
}

func TestMergeAddsOnlyMissingTargets(t *testing.T) {
	generated := Generate([]stack.Adapter{fakeAdapter{}}, stack.Params{}, 80, "origin/main")
	merged := Merge(sample, generated)
	if !strings.HasPrefix(merged, strings.TrimRight(sample, "\n")) {
		t.Fatal("Merge changed the existing content")
	}
	m := Parse(merged)
	for _, target := range append(append([]string{}, Standard...), Extra...) {
		if !m.Has(target) {
			t.Errorf("merged Makefile lacks %s", target)
		}
	}
	if got := m.Targets["lint"].Recipe; len(got) != 1 || got[0] != "golangci-lint run" {
		t.Errorf("existing lint target was replaced: %v", got)
	}
	if strings.Count(merged, "\ncheck:") != 1 {
		t.Error("check target duplicated")
	}
	if m.Vars["FAKE"] != "fake" || m.Vars["GO"] != "go" {
		t.Errorf("Vars = %v", m.Vars)
	}
	if again := Merge(merged, generated); again != merged {
		t.Error("Merge is not idempotent")
	}
}

func TestRecipesFollowsDependencies(t *testing.T) {
	m := Parse(sample)
	got := strings.Join(m.Recipes("check"), "\n")
	for _, want := range []string{"pawl verify", "golangci-lint run", "gofmt -l .", "$(GO) test ./..."} {
		if !strings.Contains(got, want) {
			t.Errorf("Recipes(check) lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "mod download") {
		t.Errorf("Recipes(check) includes an unrelated target:\n%s", got)
	}
	if len(m.Recipes("nope")) != 0 {
		t.Error("Recipes of a missing target is not empty")
	}
}
