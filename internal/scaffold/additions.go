package scaffold

import (
	"encoding/json"
	"strings"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/stack/golang"
	"github.com/rojosinalma/pawl/internal/stack/python"
	"github.com/rojosinalma/pawl/internal/stack/ruby"
)

// tsDevPackages are the npm packages the TypeScript make targets need in package.json.
var tsDevPackages = []string{"typescript", "oxlint-tsgolint", "@stryker-mutator/core"}

// writeAdditions adds the stack's own dependency declarations (Gemfile group, pyproject dependency group) and
// records the commands the project still has to run as hints.
func writeAdditions(p *project.Project, w *writer) error {
	additions := map[string]func(*project.Project, *writer) error{
		"ruby":       addRuby,
		"python":     addPython,
		"go":         addGo,
		"typescript": addTypeScript,
		"rust":       addRust,
	}
	for _, a := range p.StackAdapters() {
		if add, ok := additions[a.Name()]; ok {
			if err := add(p, w); err != nil {
				return err
			}
		}
	}
	return nil
}

func addRuby(p *project.Project, w *writer) error {
	if w.exists("Gemfile") && p.Params.Facts["has_rubocop"] == "true" {
		return nil
	}
	header := "# frozen_string_literal: true\n\nsource 'https://rubygems.org'\n"
	if err := w.appendTo("Gemfile", header, ruby.GemfileAdditions(p.Params.Facts["test_framework"])); err != nil {
		return err
	}
	w.res.Hints = append(w.res.Hints, "run: bundle install")
	return nil
}

func addPython(p *project.Project, w *writer) error {
	existing, ok, err := w.read("pyproject.toml")
	if err != nil || !ok {
		return err
	}
	if strings.Contains(existing, "[dependency-groups]") || strings.Contains(existing, "pytest") {
		return nil
	}
	if err := w.appendTo("pyproject.toml", "", python.DevDependencies); err != nil {
		return err
	}
	hint := "run: pip install --group dev (pip 25.1 or later)"
	if p.Params.Facts["uv"] == "true" {
		hint = "run: uv sync"
	}
	w.res.Hints = append(w.res.Hints, hint)
	return nil
}

// addGo leaves go.mod alone (go mod tidy must resolve the tools) and hints the go get -tool line for the missing ones.
func addGo(_ *project.Project, w *writer) error {
	gomod, ok, err := w.read("go.mod")
	if err != nil || !ok {
		return err
	}
	var missing []string
	for _, mod := range []string{golang.GovulncheckModule, golang.Gcov2lcovModule} {
		pkg, _, _ := strings.Cut(mod, "@")
		if !strings.Contains(gomod, pkg) {
			missing = append(missing, mod)
		}
	}
	if len(missing) > 0 {
		w.res.Hints = append(w.res.Hints, "run: go get -tool "+strings.Join(missing, " "))
	}
	return nil
}

func addTypeScript(p *project.Project, w *writer) error {
	raw, ok, err := w.read("package.json")
	if err != nil || !ok {
		return err
	}
	// a package.json that does not parse gets no hint; the stack's own tools report it
	missing, ok := missingPackages(raw, tsDevPackages)
	if !ok || len(missing) == 0 {
		return nil
	}
	install := "bun add -d "
	if p.Params.Facts["runtime"] != "bun" {
		install = "npm install -D "
	}
	w.res.Hints = append(w.res.Hints, "run: "+install+strings.Join(missing, " "))
	return nil
}

func addRust(_ *project.Project, w *writer) error {
	w.res.Hints = append(w.res.Hints, "run: rustup component add clippy rustfmt llvm-tools-preview")
	return nil
}

// missingPackages lists the names package.json declares in neither dependencies nor devDependencies; ok is false
// when it does not parse.
func missingPackages(raw string, names []string) ([]string, bool) {
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal([]byte(raw), &pkg) != nil {
		return nil, false
	}
	var missing []string
	for _, name := range names {
		_, dep := pkg.Dependencies[name]
		_, dev := pkg.DevDependencies[name]
		if !dep && !dev {
			missing = append(missing, name)
		}
	}
	return missing, true
}
