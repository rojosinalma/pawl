package scaffold

import (
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/stack/golang"
)

// stepLines detects the project in files and returns its setup steps as "name: command" lines.
func stepLines(t *testing.T, files map[string]string, stacks ...string) []string {
	t.Helper()
	p, err := project.Detect(newProject(t, false, files), stacks)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range SetupSteps(p) {
		out = append(out, s.Name+": "+strings.Join(s.Command, " "))
	}
	return out
}

func TestSetupSteps(t *testing.T) {
	tools := "tool (\n\t" + strings.Split(golang.GovulncheckModule, "@")[0] + "\n\t" + strings.Split(golang.Gcov2lcovModule, "@")[0] + "\n)\n"
	full := `{"devDependencies": {"typescript": "5", "oxlint-tsgolint": "1", "@stryker-mutator/core": "9"}}`
	cases := []struct {
		name   string
		files  map[string]string
		stacks []string
		want   string
	}{
		{"go without tools", map[string]string{"go.mod": "module x\n"}, nil,
			"go tools: go get -tool " + golang.GovulncheckModule + " " + golang.Gcov2lcovModule},
		{"go with tools", map[string]string{"go.mod": "module x\n" + tools}, nil, ""},
		{"go without go.mod", map[string]string{"main.go": "package main\n"}, []string{"go"}, ""},
		{"ruby", map[string]string{"Gemfile": "source 'x'\n"}, nil, "gems: bundle install"},
		{"ruby without Gemfile", map[string]string{"a.rb": "\n"}, []string{"ruby"}, ""},
		{"python with uv.lock", map[string]string{"pyproject.toml": "[project]\n", "uv.lock": ""}, nil, "python packages: uv sync"},
		{"python uv-managed", map[string]string{"pyproject.toml": "[project]\n[tool.uv]\n"}, nil, "python packages: uv sync"},
		{"python with pip", map[string]string{"pyproject.toml": "[project]\n"}, nil, "python packages: pip install --group dev"},
		{"python without pyproject", map[string]string{"a.py": "\n"}, nil, ""},
		{"bun adds", map[string]string{"package.json": `{"devDependencies": {"typescript": "5"}}`, "bun.lock": "{}"}, nil,
			"node packages: bun add -d oxlint-tsgolint @stryker-mutator/core"},
		{"bun installs", map[string]string{"package.json": full, "bun.lock": "{}"}, nil, "node packages: bun install --frozen-lockfile"},
		{"npm adds", map[string]string{"package.json": `{}`}, nil, "node packages: npm install -D typescript oxlint-tsgolint @stryker-mutator/core"},
		{"npm installs", map[string]string{"package.json": full}, nil, "node packages: npm ci"},
		{"bad package.json", map[string]string{"package.json": "{"}, nil, ""},
		{"rust", map[string]string{"Cargo.toml": "[package]\n"}, nil, "rust components: rustup component add clippy rustfmt llvm-tools-preview"},
		{"shell has none", map[string]string{"run.sh": "#!/bin/sh\n"}, nil, ""},
	}
	for _, c := range cases {
		if got := strings.Join(stepLines(t, c.files, c.stacks...), "; "); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSetupStepsFollowAdapterOrder(t *testing.T) {
	got := stepLines(t, map[string]string{"Cargo.toml": "[package]\n", "Gemfile": "\n", "go.mod": "module x\n"})
	if len(got) != 3 || !strings.HasPrefix(got[0], "go tools") || !strings.HasPrefix(got[1], "gems") || !strings.HasPrefix(got[2], "rust") {
		t.Errorf("order: %v", got)
	}
}

// TestHintsComeFromSetupSteps: init hints the same command a later `pawl install` runs.
func TestHintsComeFromSetupSteps(t *testing.T) {
	dir := newProject(t, true, map[string]string{"go.mod": "module example.com/demo\n\ngo 1.25\n", "Cargo.toml": "[package]\nname = \"d\"\n"})
	res := mustInit(t, dir, Options{})
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range SetupSteps(p) {
		if !hasHint(res, "run: "+strings.Join(s.Command, " ")) {
			t.Errorf("no hint for %v in %v", s.Command, res.Hints)
		}
	}
}
