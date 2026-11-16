package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	w.hint(rubyStep())
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
	step := pythonStep(p, existing)
	if step.Command[0] == "pip" {
		w.res.Hints = append(w.res.Hints, "run: "+strings.Join(step.Command, " ")+" (pip 25.1 or later)")
		return nil
	}
	w.hint(step)
	return nil
}

// addGo leaves go.mod alone (go mod tidy must resolve the tools) and hints the go get -tool line for the missing ones.
func addGo(_ *project.Project, w *writer) error {
	gomod, ok, err := w.read("go.mod")
	if err != nil || !ok {
		return err
	}
	if step, ok := goStep(gomod); ok {
		w.hint(step)
		w.hint(goTidyStep())
	}
	return nil
}

// addTypeScript hints the dev packages package.json lacks; a package.json that does not parse gets no hint, the
// stack's own tools report it.
func addTypeScript(p *project.Project, w *writer) error {
	raw, ok, err := w.read("package.json")
	if err != nil || !ok {
		return err
	}
	if missing, ok := missingPackages(raw, tsDevPackages); !ok || len(missing) == 0 {
		return nil
	}
	step, _ := typeScriptStep(p, raw)
	w.hint(step)
	return nil
}

func addRust(_ *project.Project, w *writer) error {
	w.hint(rustStep())
	return nil
}

// hint records the step as a command the project still has to run.
func (w *writer) hint(s SetupStep) {
	w.res.Hints = append(w.res.Hints, "run: "+strings.Join(s.Command, " "))
}

// SetupStep is a command the project's stack needs once after init or a fresh clone.
type SetupStep struct {
	Name    string   // "go tools", "gems", "python packages", "node packages", "rust components"
	Command []string // argv
}

// SetupSteps lists the stack-native commands the project needs, in adapter order: the Go tools go.mod lacks, the
// gems, the Python dev group, the node packages (added when package.json lacks pawl's, else installed from the
// lockfile) and the Rust components. A stack whose manifest file (go.mod, Gemfile, pyproject.toml, package.json) is
// missing has no step.
func SetupSteps(p *project.Project) []SetupStep {
	var out []SetupStep
	for _, a := range p.StackAdapters() {
		if step, ok := setupStep(p, a.Name()); ok {
			out = append(out, step)
			if step.Name == goToolsStep {
				out = append(out, goTidyStep())
			}
		}
	}
	return out
}

// setupStep is the step of one stack; ok is false when it has none.
func setupStep(p *project.Project, stackName string) (SetupStep, bool) {
	read := func(rel string) (string, bool) {
		raw, err := os.ReadFile(filepath.Join(p.Root, rel))
		return string(raw), err == nil
	}
	switch stackName {
	case "go":
		if gomod, ok := read("go.mod"); ok {
			return goStep(gomod)
		}
	case "ruby":
		if _, ok := read("Gemfile"); ok {
			return rubyStep(), true
		}
	case "python":
		if pyproject, ok := read("pyproject.toml"); ok {
			return pythonStep(p, pyproject), true
		}
	case "typescript":
		if raw, ok := read("package.json"); ok {
			return typeScriptStep(p, raw)
		}
	case "rust":
		return rustStep(), true
	}
	return SetupStep{}, false
}

// goStep adds the tools pawl's Go targets run when go.mod lacks them.
func goStep(gomod string) (SetupStep, bool) {
	var missing []string
	for _, mod := range []string{golang.GovulncheckModule, golang.Gcov2lcovModule} {
		pkg, _, _ := strings.Cut(mod, "@")
		if !strings.Contains(gomod, pkg) {
			missing = append(missing, mod)
		}
	}
	if len(missing) == 0 {
		return SetupStep{}, false
	}
	return SetupStep{Name: goToolsStep, Command: append([]string{"go", "get", "-tool"}, missing...)}, true
}

// goToolsStep names the step that adds the Go tools.
const goToolsStep = "go tools"

// goTidyStep follows the tools: `go get -tool` records the modules, `go mod tidy` completes go.sum, and the lint
// step `go mod tidy -diff` fails until it has run.
func goTidyStep() SetupStep {
	return SetupStep{Name: "go.sum", Command: []string{"go", "mod", "tidy"}}
}

func rubyStep() SetupStep {
	return SetupStep{Name: "gems", Command: []string{"bundle", "install"}}
}

// pythonStep installs the dev group with uv in a uv project (a uv.lock or a [tool.uv] section), else with pip.
func pythonStep(p *project.Project, pyproject string) SetupStep {
	if p.Params.Facts["uv"] == "true" || strings.Contains(pyproject, "[tool.uv") {
		return SetupStep{Name: "python packages", Command: []string{"uv", "sync"}}
	}
	return SetupStep{Name: "python packages", Command: []string{"pip", "install", "--group", "dev"}}
}

// typeScriptStep adds the dev packages package.json lacks, or installs the locked packages when it has them all;
// ok is false when package.json does not parse.
func typeScriptStep(p *project.Project, raw string) (SetupStep, bool) {
	missing, ok := missingPackages(raw, tsDevPackages)
	if !ok {
		return SetupStep{}, false
	}
	isBun := p.Params.Facts["runtime"] == "bun"
	var cmd []string
	switch {
	case len(missing) > 0 && isBun:
		cmd = append([]string{"bun", "add", "-d"}, missing...)
	case len(missing) > 0:
		cmd = append([]string{"npm", "install", "-D"}, missing...)
	case isBun:
		cmd = []string{"bun", "install", "--frozen-lockfile"}
	default:
		cmd = []string{"npm", "ci"}
	}
	return SetupStep{Name: "node packages", Command: cmd}, true
}

func rustStep() SetupStep {
	return SetupStep{Name: "rust components", Command: []string{"rustup", "component", "add", "clippy", "rustfmt", "llvm-tools-preview"}}
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
