// Package affected computes the test scope of the change against the base: what `make test` passes to the stack's
// test runner in the pull request ring.
//
// The scope is one line of arguments for the stack's test runner. Three cases decide it:
//
//	stack        everything (full run)              nothing (no file of the stack changed)   affected
//	go           ./...                              -run ^$ ./...                            ./a ./cmd/x (reverse deps)
//	rust         --workspace                        --workspace --no-run                     -p a -p b (reverse deps)
//	python       "" (pytest discovers)              --collect-only -q                        test files
//	ruby         every test/**/*_test.rb            ""                                       test files
//	             (spec/**/*_spec.rb with rspec)
//	typescript   "" (the runner discovers)          ""                                       test files
//	shell        ""                                 ""                                       ""
//
// Everything runs when PAWL_FULL=1, outside a repository, without a base, when nothing changed at all, when a global
// file changed (pawl.toml, a Makefile, .pawl/, .github/, mise.toml, .tool-versions, a lockfile or package manifest,
// a tool configuration at the root), when the stack's own tooling cannot be queried, and, for the file-mapped
// stacks, when a source file changed but no test file matches it by name or a test helper changed.
package affected

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/rojosinalma/pawl/internal/project"
)

const cmdTimeout = 2 * time.Minute

// runner runs an outbound command and returns its standard output.
type runner interface {
	Output(dir, name string, args ...string) ([]byte, error)
}

type execRunner struct{ timeout time.Duration }

func (e execRunner) Output(dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// strategy is one stack's way from a change set to a scope.
type strategy struct {
	everything func(p *project.Project) string
	nothing    string
	owns       func(file string) bool
	// affected returns the scope for a change that touches the stack; ok=false means run everything.
	affected func(p *project.Project, run runner, changed []string) (scope string, ok bool)
}

func strategies() map[string]strategy {
	return map[string]strategy{
		"go":         goStrategy(),
		"rust":       rustStrategy(),
		"python":     mappedStrategy(pythonMapping()),
		"ruby":       mappedStrategy(rubyMapping()),
		"typescript": mappedStrategy(typescriptMapping()),
		"shell":      shellStrategy(),
	}
}

// Scope returns the arguments for the stack's test runner, as one line for a Makefile `$(shell ...)`:
// Go: the packages that depend on a changed package ("./internal/a ./cmd/x"); Rust: "-p crate" per affected
// workspace crate; Python, Ruby, TypeScript: the test files that changed or that match a changed source file by
// name. It returns the stack's "everything" scope when PAWL_FULL=1, when there is no base, when nothing changed, or
// when a configuration, lockfile, Makefile, workflow or pawl.toml changed. A change that touches no file of the stack
// returns the stack's "nothing" scope (see the package documentation).
func Scope(p *project.Project, stackName string) (string, error) {
	return scope(p, stackName, execRunner{timeout: cmdTimeout})
}

func scope(p *project.Project, stackName string, run runner) (string, error) {
	st, ok := strategies()[stackName]
	if !ok {
		return "", fmt.Errorf("unknown stack %q (known: %s)", stackName, strings.Join(project.StackNames(), ", "))
	}
	all := st.everything(p)
	if os.Getenv("PAWL_FULL") == "1" || p.Repo == nil {
		return all, nil
	}
	base := p.BaseRef()
	if base == "" {
		return all, nil
	}
	changed, ok := changes(p, base, run)
	if !ok || len(changed) == 0 {
		return all, nil
	}
	return decide(p, st, run, changed, all), nil
}

// decide applies the global-file rule, the "nothing" rule and the stack's own mapping.
func decide(p *project.Project, st strategy, run runner, changed []string, all string) string {
	ours := false
	for _, f := range changed {
		if isGlobal(f) {
			return all
		}
		ours = ours || st.owns(f)
	}
	if !ours {
		return st.nothing
	}
	if s, ok := st.affected(p, run, changed); ok {
		return s
	}
	return all
}

// changes lists the changed, deleted and untracked paths between base and the working tree; ok is false when git
// cannot tell, and the caller runs everything.
func changes(p *project.Project, base string, run runner) (files []string, ok bool) {
	changed, err := p.Repo.Changed(base)
	if err != nil {
		return nil, false
	}
	seen := map[string]bool{}
	for _, f := range changed {
		seen[f] = true
	}
	add := func(f string) {
		if f != "" && !seen[f] {
			seen[f] = true
			changed = append(changed, f)
		}
	}
	deleted, err := run.Output(p.Root, "git", "diff", "--name-only", "--diff-filter=D", "-z", base)
	if err != nil {
		return nil, false
	}
	for _, f := range strings.Split(string(deleted), "\x00") {
		add(f)
	}
	status, err := run.Output(p.Root, "git", "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, false
	}
	listed := toSet(p.Files)
	for _, rec := range strings.Split(string(status), "\x00") {
		if f, ok := strings.CutPrefix(rec, "?? "); ok && listed[f] {
			add(f)
		}
	}
	return changed, true
}

func toSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, s := range items {
		out[s] = true
	}
	return out
}

// isGlobal reports whether a change to the path can affect every test of every stack.
func isGlobal(file string) bool {
	if strings.HasPrefix(file, ".pawl/") || strings.HasPrefix(file, ".github/") {
		return true
	}
	switch path.Base(file) {
	case "pawl.toml", "Makefile", "GNUmakefile", "mise.toml", ".tool-versions",
		"go.sum", "go.mod", "Cargo.lock", "Cargo.toml", "bun.lock", "package.json", "package-lock.json",
		"uv.lock", "pyproject.toml", "Gemfile", "Gemfile.lock":
		return true
	}
	switch file {
	case ".golangci.yml", "biome.jsonc", "ruff.toml", ".rubocop.yml", "tsconfig.json", "pyrightconfig.json",
		"clippy.toml", "pytest.ini":
		return true
	}
	return false
}

func shellStrategy() strategy {
	return strategy{
		everything: func(*project.Project) string { return "" },
		owns:       func(string) bool { return false },
		affected:   func(*project.Project, runner, []string) (string, bool) { return "", true },
	}
}
