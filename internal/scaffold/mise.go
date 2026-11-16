package scaffold

import (
	"fmt"
	"strings"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/stack"
)

// Runtime versions mise.toml pins for the npm: and pypi: tools when the project's own stack does not pin them.
const (
	NodeVersion   = "24.22.2"
	PythonVersion = "3.13.18"
	UvVersion     = "0.13.8"
)

const miseHeader = `# pawl: the tools of the rings (hook, check, nightly), pinned. ` + "`mise install`" + ` installs them.
# The stack's runtime stays in its native pin file (go.mod, .ruby-version, .python-version, rust-toolchain.toml).
[tools]
`

// writeMise writes mise.toml when the project has neither it nor .tool-versions.
func writeMise(p *project.Project, w *writer) error {
	if w.exists("mise.toml") {
		w.res.Kept = append(w.res.Kept, "mise.toml")
		return nil
	}
	if w.exists(".tool-versions") {
		return nil
	}
	return w.create("mise.toml", renderMise(p, w), 0o644)
}

// renderMise lists every pinned tool of the active adapters, grouped by adapter, plus the runtimes their
// npm: and pypi: backends need.
func renderMise(p *project.Project, w *writer) string {
	var b strings.Builder
	b.WriteString(miseHeader)
	var npm, pypi bool
	for _, a := range p.Adapters {
		lines := miseLines(a.Tools())
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "# %s\n", a.Title())
		for _, t := range a.Tools() {
			npm = npm || strings.HasPrefix(t.Mise, "npm:")
			pypi = pypi || strings.HasPrefix(t.Mise, "pypi:")
		}
		b.WriteString(strings.Join(lines, ""))
	}
	runtimes := runtimeLines(p, w, npm, pypi)
	if len(runtimes) > 0 {
		b.WriteString("# runtimes of the npm: and pypi: tools\n")
		b.WriteString(strings.Join(runtimes, ""))
	}
	return b.String()
}

func miseLines(tools []stack.Tool) []string {
	var out []string
	for _, t := range tools {
		if t.Mise == "" || t.Version == "" {
			continue
		}
		out = append(out, fmt.Sprintf("%s = %q\n", miseKey(t.Mise), t.Version))
	}
	return out
}

// miseKey quotes a tool name that is not a bare TOML key.
func miseKey(name string) string {
	if strings.ContainsAny(name, ":/@.") {
		return fmt.Sprintf("%q", name)
	}
	return name
}

func runtimeLines(p *project.Project, w *writer, npm, pypi bool) []string {
	var out []string
	if npm && !pinsRuntime(p, w, "typescript", ".node-version", ".nvmrc", ".bun-version") {
		out = append(out, fmt.Sprintf("node = %q\n", NodeVersion))
	}
	if pypi && !pinsRuntime(p, w, "python", ".python-version") {
		out = append(out, fmt.Sprintf("python = %q\n", PythonVersion), fmt.Sprintf("uv = %q\n", UvVersion))
	}
	return out
}

// pinsRuntime reports whether the stack is active and one of its pin files exists.
func pinsRuntime(p *project.Project, w *writer, stackName string, files ...string) bool {
	if !hasStack(p, stackName) {
		return false
	}
	for _, f := range files {
		if w.exists(f) {
			return true
		}
	}
	return false
}

func hasStack(p *project.Project, name string) bool {
	for _, a := range p.Adapters {
		if a.Name() == name {
			return true
		}
	}
	return false
}
