// Package stack defines the adapter contract: how one language stack enforces the catalog with its own tools.
package stack

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rojosinalma/pawl/internal/report"
)

// Params are the project facts an adapter needs to render configurations and make targets.
type Params struct {
	ConfigModule string
	OwnModule    string
	FileLines    int
	Complexity   int
	DiffCoverage int
	TestDirs     []string
	Exclude      []string
	// Facts the adapter detected (module path, package name) for its own use.
	Facts map[string]string
}

// Tool is a program the stack's enforcement runs.
type Tool struct {
	Name    string
	Version string   // the version pawl pins in the files it writes; "" when the stack's own pin applies
	Check   []string // prints the version; empty when the tool has no version flag
	Install string   // how to get it
	Mise    string   // the mise tool name pawl writes into mise.toml; "" when the stack's own pin applies
	Rings   []string // which rings need it; nightly-only tools are optional for the hook and the check ring
}

// ConfigFile is a tool configuration pawl writes when the project has none.
type ConfigFile struct {
	Path    string
	Content string
	Mode    os.FileMode
}

// Enforcement is the adapter's default for one catalog rule: a make step with the tools behind it, or a waiver.
type Enforcement struct {
	Step   string
	Via    []string
	Waived string
}

// Verifier checks that a tool's configuration still enforces a rule; it reports when the enforcement was turned off.
type Verifier struct {
	RuleID string
	Via    string
	Check  func(root string) []report.Finding
}

// Suppression is a directive that silences a tool; pawl counts them and requires a reason.
type Suppression struct {
	Kind    string
	Pattern *regexp.Regexp // the directive; the named group "reason" holds the text after it when present
	Globs   []string       // files it can appear in
}

// Pin is where the stack records its toolchain version and dependency lock.
type Pin struct {
	What  string
	Files []string // one of these must exist
	Locks []string // one of these must be tracked; empty when the stack has no lockfile
}

// Make is the stack's contribution to each standard target: recipe lines.
type Make struct {
	Vars      []string // variable definitions, one per line
	Fmt       []string
	FmtCheck  []string
	Lint      []string
	Test      []string
	Precommit []string
	Coverage  []string
	Audit     []string
	Mutation  []string
}

// Adapter is one stack.
type Adapter interface {
	Name() string
	Title() string
	// Detect reports whether the files (relative to the root) belong to this stack.
	Detect(files []string) bool
	// Facts reads project facts (module path, package name) for Params.Facts.
	Facts(root string) map[string]string
	Tools() []Tool
	Configs(p Params) []ConfigFile
	Rules(p Params) map[string]Enforcement
	Verifiers(p Params) []Verifier
	Suppressions() []Suppression
	Pins() []Pin
	Make(p Params) Make
	// SourceGlobs are the files pawl's text checks (file size, suppressions) read for this stack.
	SourceGlobs() []string
	// TestGlobs are the files that are tests; production-only rules skip them.
	TestGlobs() []string
}

var registry []Adapter

// Register adds an adapter; adapters register themselves in init.
func Register(a Adapter) { registry = append(registry, a) }

// All returns the registered adapters in a fixed order.
func All() []Adapter {
	out := append([]Adapter(nil), registry...)
	sort.Slice(out, func(i, j int) bool { return order(out[i].Name()) < order(out[j].Name()) })
	return out
}

func order(name string) int {
	for i, n := range []string{"go", "typescript", "ruby", "python", "shell", "rust"} {
		if n == name {
			return i
		}
	}
	return 99
}

// Get returns an adapter by name.
func Get(name string) (Adapter, bool) {
	for _, a := range registry {
		if a.Name() == name {
			return a, true
		}
	}
	return nil, false
}

// Detect returns the names of the stacks whose files are present.
func Detect(files []string) []string {
	var out []string
	for _, a := range All() {
		if a.Detect(files) {
			out = append(out, a.Name())
		}
	}
	return out
}

// Match reports whether the path matches one of the globs (`**/` prefixes match any depth).
func Match(path string, globs []string) bool {
	path = filepath.ToSlash(path)
	base := filepath.Base(path)
	for _, g := range globs {
		if strings.HasPrefix(g, "**/") {
			rest := strings.TrimPrefix(g, "**/")
			if !strings.Contains(rest, "/") {
				if ok, _ := filepath.Match(rest, base); ok {
					return true
				}
				continue
			}
			// a directory pattern such as **/test/**: match any path segment sequence
			if matchSegments(path, rest) {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(g, path); ok {
			return true
		}
		if strings.HasSuffix(g, "/**") && strings.HasPrefix(path, strings.TrimSuffix(g, "/**")+"/") {
			return true
		}
	}
	return false
}

func matchSegments(path, pattern string) bool {
	segs := strings.Split(path, "/")
	for i := range segs {
		sub := strings.Join(segs[i:], "/")
		if strings.HasSuffix(pattern, "/**") {
			dir := strings.TrimSuffix(pattern, "/**")
			if strings.HasPrefix(sub, dir+"/") {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(pattern, sub); ok {
			return true
		}
	}
	return false
}

// Filter returns the paths matching any of the globs.
func Filter(files, globs []string) []string {
	var out []string
	for _, f := range files {
		if Match(f, globs) {
			out = append(out, f)
		}
	}
	return out
}

// HasAny reports whether any file matches the globs.
func HasAny(files, globs []string) bool {
	for _, f := range files {
		if Match(f, globs) {
			return true
		}
	}
	return false
}

// Excluded reports whether path starts with one of the prefixes.
func Excluded(path string, prefixes []string) bool {
	path = filepath.ToSlash(path)
	for _, p := range prefixes {
		p = strings.TrimSuffix(filepath.ToSlash(p), "/")
		if p != "" && (path == p || strings.HasPrefix(path, p+"/")) {
			return true
		}
	}
	return false
}

// Reason builds a consistent waiver text for a rule the stack has no tool for.
func Reason(stack, what string) string {
	return stack + ": " + what
}
