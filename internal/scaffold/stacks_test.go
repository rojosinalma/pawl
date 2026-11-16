package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRubyAppendsToGemfile(t *testing.T) {
	dir := newProject(t, true, map[string]string{
		"Gemfile": "source 'https://rubygems.org'\n\ngem 'rack'",
		"app.rb":  "puts 1\n",
	})
	res := mustInit(t, dir, Options{})
	gemfile := readFile(t, dir, "Gemfile")
	if !strings.HasPrefix(gemfile, "source 'https://rubygems.org'\n\ngem 'rack'\n") {
		t.Errorf("Gemfile head changed:\n%s", gemfile)
	}
	assertContains(t, "Gemfile", gemfile, "group :development, :test do", "gem 'rubocop', ", "rubocop-minitest")
	if !hasItem(res.Merged, "Gemfile") || !hasHint(res, "run: bundle install") {
		t.Errorf("merged %v, hints %v", res.Merged, res.Hints)
	}
	check := readFile(t, dir, ".github/workflows/check.yml")
	assertContains(t, "check.yml", check, "ruby/setup-ruby@"+actions["ruby/setup-ruby"].SHA, "bundler-cache: true")
}

func TestRubyCreatesGemfile(t *testing.T) {
	dir := newProject(t, true, map[string]string{"app.rb": "puts 1\n", "spec/app_spec.rb": "\n"})
	res := mustInit(t, dir, Options{})
	gemfile := readFile(t, dir, "Gemfile")
	if !strings.HasPrefix(gemfile, "# frozen_string_literal: true\n\nsource 'https://rubygems.org'\n") {
		t.Errorf("Gemfile =\n%s", gemfile)
	}
	assertContains(t, "Gemfile", gemfile, "rubocop-rspec")
	if !hasItem(res.Created, "Gemfile") {
		t.Errorf("Created = %v", res.Created)
	}
}

func TestRubyWithRubocopIsLeftAlone(t *testing.T) {
	gemfile := "source 'https://rubygems.org'\ngem 'rubocop'\n"
	dir := newProject(t, true, map[string]string{"Gemfile": gemfile})
	res := mustInit(t, dir, Options{})
	if readFile(t, dir, "Gemfile") != gemfile || hasHint(res, "run: bundle") {
		t.Errorf("Gemfile touched; hints %v", res.Hints)
	}
}

func TestPythonWithUV(t *testing.T) {
	dir := newProject(t, true, map[string]string{
		"pyproject.toml":  "[project]\nname = \"demo-app\"\n",
		"uv.lock":         "version = 1\n",
		".python-version": "3.13\n",
		"demo/app.py":     "x = 1\n",
	})
	res := mustInit(t, dir, Options{})
	assertContains(t, "pyproject.toml", readFile(t, dir, "pyproject.toml"), "[project]\nname = \"demo-app\"\n", "[dependency-groups]", "pytest==")
	if !hasItem(res.Merged, "pyproject.toml") || !hasHint(res, "run: uv sync") {
		t.Errorf("merged %v, hints %v", res.Merged, res.Hints)
	}
	assertContains(t, "check.yml", readFile(t, dir, ".github/workflows/check.yml"), "astral-sh/setup-uv@"+actions["astral-sh/setup-uv"].SHA)
	mise := readFile(t, dir, "mise.toml")
	assertContains(t, "mise.toml", mise, "# Python", `"pypi:diff-cover"`, `node = "24.22.2"`)
	assertLacks(t, "mise.toml", mise, "python = ", "uv = ")
}

func TestPythonWithPytestIsLeftAlone(t *testing.T) {
	pyproject := "[project]\nname = \"demo\"\n[tool.pytest.ini_options]\n"
	dir := newProject(t, true, map[string]string{"pyproject.toml": pyproject})
	res := mustInit(t, dir, Options{})
	if readFile(t, dir, "pyproject.toml") != pyproject || hasHint(res, "run: uv") || hasHint(res, "run: pip") {
		t.Errorf("pyproject touched; hints %v", res.Hints)
	}
	assertContains(t, "mise.toml", readFile(t, dir, "mise.toml"), `python = "3.13.18"`, `uv = "0.13.8"`)
	assertLacks(t, "check.yml", readFile(t, dir, ".github/workflows/check.yml"), "setup-uv")
}

func TestPythonWithPipHint(t *testing.T) {
	dir := newProject(t, true, map[string]string{"pyproject.toml": "[project]\nname = \"demo\"\n"})
	res := mustInit(t, dir, Options{})
	if !hasHint(res, "run: pip install --group dev") {
		t.Errorf("hints %v", res.Hints)
	}
}

func TestTypeScriptWithBun(t *testing.T) {
	dir := newProject(t, true, map[string]string{
		"package.json":  `{"name": "demo", "devDependencies": {"typescript": "5.9.0"}}`,
		"bun.lock":      "{}\n",
		".bun-version":  "1.3.0\n",
		"tsconfig.json": "{}\n",
	})
	res := mustInit(t, dir, Options{})
	if !hasHint(res, "run: bun add -d oxlint-tsgolint @stryker-mutator/core") {
		t.Errorf("hints %v", res.Hints)
	}
	check := readFile(t, dir, ".github/workflows/check.yml")
	assertContains(t, "check.yml", check, "oven-sh/setup-bun@"+actions["oven-sh/setup-bun"].SHA, "bun-version-file: .bun-version", "run: bun install --frozen-lockfile")
	mise := readFile(t, dir, "mise.toml")
	assertContains(t, "mise.toml", mise, "# TypeScript", `"npm:knip"`, `python = "3.13.18"`)
	assertLacks(t, "mise.toml", mise, "node = ")
}

func TestTypeScriptWithNode(t *testing.T) {
	dir := newProject(t, true, map[string]string{
		"package.json": `{"name": "demo", "dependencies": {"typescript": "5"}, "devDependencies": {"oxlint-tsgolint": "1", "@stryker-mutator/core": "9"}}`,
	})
	res := mustInit(t, dir, Options{})
	if hasHint(res, "run: npm") || hasHint(res, "run: bun") {
		t.Errorf("hints %v", res.Hints)
	}
	check := readFile(t, dir, ".github/workflows/check.yml")
	assertLacks(t, "check.yml", check, "setup-bun", "bun install")
	assertContains(t, "mise.toml", readFile(t, dir, "mise.toml"), `node = "24.22.2"`)

	bad := newProject(t, true, map[string]string{"package.json": "{not json"})
	if res := mustInit(t, bad, Options{}); hasHint(res, "run: npm") {
		t.Errorf("hints %v", res.Hints)
	}
	partial := newProject(t, true, map[string]string{"package.json": `{"name": "demo"}`})
	if res := mustInit(t, partial, Options{}); !hasHint(res, "run: npm install -D typescript oxlint-tsgolint @stryker-mutator/core") {
		t.Errorf("hints %v", res.Hints)
	}
}

func TestRust(t *testing.T) {
	dir := newProject(t, true, map[string]string{
		"Cargo.toml":  "[package]\nname = \"demo-crate\"\n",
		"src/main.rs": "fn main() {}\n",
	})
	res := mustInit(t, dir, Options{})
	if !hasHint(res, "run: rustup component add clippy rustfmt llvm-tools-preview") {
		t.Errorf("hints %v", res.Hints)
	}
	assertContains(t, "check.yml", readFile(t, dir, ".github/workflows/check.yml"), "run: rustup toolchain install")
	assertContains(t, "mise.toml", readFile(t, dir, "mise.toml"), "# Rust", `"aqua:taiki-e/cargo-llvm-cov"`)
}

// TestEveryStackWorkflows renders the workflows with every setup step and checks their shape; with
// PAWL_WORKFLOW_OUT set it also copies them there for actionlint and zizmor.
func TestEveryStackWorkflows(t *testing.T) {
	dir := newProject(t, true, map[string]string{
		"go.mod":         "module example.com/demo\n\ngo 1.25\n",
		"Gemfile":        "source 'https://rubygems.org'\n",
		"pyproject.toml": "[project]\nname = \"demo\"\n",
		"uv.lock":        "version = 1\n",
		"package.json":   `{"name": "demo"}`,
		"bun.lock":       "{}\n",
		"Cargo.toml":     "[package]\nname = \"demo\"\n",
		"run.sh":         "#!/bin/sh\n",
	})
	res := mustInit(t, dir, Options{Version: "v0.1.0"})
	if strings.Join(res.Stacks, ",") != "go,typescript,ruby,python,shell,rust" {
		t.Fatalf("stacks = %v", res.Stacks)
	}
	for _, name := range workflowFiles {
		checkWorkflow(t, name, readFile(t, dir, ".github/workflows/"+name))
	}
	if out := os.Getenv("PAWL_WORKFLOW_OUT"); out != "" {
		copyWorkflowFiles(t, dir, out)
	}
	check := readFile(t, dir, ".github/workflows/check.yml")
	for name := range actions {
		assertContains(t, "check.yml", check, name+"@"+actions[name].SHA+" # "+actions[name].Tag)
	}
}

// copyWorkflowFiles copies the rendered workflows, and the files they reference, from dir to out.
func copyWorkflowFiles(t *testing.T, dir, out string) {
	t.Helper()
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range workflowFiles {
		rel := filepath.Join(".github/workflows", name)
		if err := writeFile(out, rel, readFile(t, dir, rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{hookPath, agentsFile, "mise.toml"} {
		if err := writeFile(out, f, readFile(t, dir, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAgentsWithoutFileContent(t *testing.T) {
	dir := goShellProject(t, true)
	if err := os.WriteFile(filepath.Join(dir, agentsFile), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, dir, Options{})
	if got := readFile(t, dir, agentsFile); !strings.HasPrefix(got, agentsBegin) {
		t.Errorf("empty AGENTS.md:\n%s", got)
	}
}
