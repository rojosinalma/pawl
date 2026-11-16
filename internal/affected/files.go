package affected

import (
	"path"
	"sort"
	"strings"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/runner"
	"github.com/rojosinalma/pawl/internal/stack"
)

// mapping maps source files to test files by name for stacks without a dependency graph pawl can query.
type mapping struct {
	stack      string
	exts       []string                        // source extensions whose stem names tests
	isTest     func(base string) bool          // a test file the runner takes as an argument
	candidates func(stem string) []string      // test file names for a source stem
	helper     func(file string) bool          // a change that affects tests broadly (conftest, test helpers)
	everything func(p *project.Project) string // the full scope
	nothing    string
}

func mappedStrategy(m mapping) strategy {
	return strategy{
		everything: m.everything,
		nothing:    m.nothing,
		owns:       func(f string) bool { return ownedBy(m.stack, f) },
		affected:   m.affected,
	}
}

func ownedBy(name, file string) bool {
	a, ok := stack.Get(name)
	return ok && stack.Match(file, a.SourceGlobs())
}

func (m mapping) affected(p *project.Project, _ runner.Runner, changed []string) (string, bool) {
	byName := m.testIndex(p.Files)
	listed := toSet(p.Files)
	tests := map[string]bool{}
	sources := false
	for _, f := range changed {
		if !ownedBy(m.stack, f) {
			continue
		}
		switch base := path.Base(f); {
		case m.isTest(base):
			tests[f] = tests[f] || listed[f]
		case m.helper(f):
			return "", false
		default:
			sources = true
			for _, c := range m.candidates(m.stem(base)) {
				addAll(tests, byName[c])
			}
		}
	}
	out := keys(tests)
	switch {
	case len(out) == 0 && sources:
		return "", false
	case len(out) == 0:
		return m.nothing, true
	}
	return strings.Join(out, " "), true
}

// testIndex maps a test file's base name to its paths.
func (m mapping) testIndex(files []string) map[string][]string {
	out := map[string][]string{}
	for _, f := range files {
		if b := path.Base(f); m.isTest(b) {
			out[b] = append(out[b], f)
		}
	}
	return out
}

func addAll(set map[string]bool, items []string) {
	for _, s := range items {
		set[s] = true
	}
}

// keys returns the members of the set whose value is true, sorted.
func keys(set map[string]bool) []string {
	var out []string
	for k, ok := range set {
		if ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// stem strips a known source extension; a file with another extension has no stem (no test matches it).
func (m mapping) stem(base string) string {
	for _, ext := range m.exts {
		if s, ok := strings.CutSuffix(base, ext); ok {
			return s
		}
	}
	return ""
}

func underDir(file string, dirs ...string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(file, d+"/") || strings.Contains(file, "/"+d+"/") {
			return true
		}
	}
	return false
}

func pythonMapping() mapping {
	return mapping{
		stack: "python",
		exts:  []string{".py"},
		isTest: func(b string) bool {
			return strings.HasSuffix(b, ".py") && (strings.HasPrefix(b, "test_") || strings.HasSuffix(b, "_test.py"))
		},
		candidates: func(s string) []string { return nonEmpty(s, "test_"+s+".py", s+"_test.py") },
		helper: func(f string) bool {
			return path.Base(f) == "conftest.py" || underDir(f, "tests", "test")
		},
		everything: func(*project.Project) string { return "" },
		nothing:    "--collect-only -q",
	}
}

func rubyMapping() mapping {
	return mapping{
		stack:      "ruby",
		exts:       []string{".rb"},
		isTest:     func(b string) bool { return strings.HasSuffix(b, "_test.rb") || strings.HasSuffix(b, "_spec.rb") },
		candidates: func(s string) []string { return nonEmpty(s, s+"_test.rb", s+"_spec.rb") },
		helper:     func(f string) bool { return underDir(f, "test", "spec") },
		everything: rubyEverything,
		nothing:    "",
	}
}

// rubyEverything lists every test file: the specs under spec/ with rspec, the tests under test/ otherwise.
func rubyEverything(p *project.Project) string {
	glob, suffix := "test/**", "_test.rb"
	if p.Params.Facts["test_framework"] == "rspec" {
		glob, suffix = "spec/**", "_spec.rb"
	}
	var out []string
	for _, f := range stack.Filter(p.Files, []string{glob}) {
		if strings.HasSuffix(f, suffix) {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func typescriptMapping() mapping {
	return mapping{
		stack:  "typescript",
		exts:   []string{".tsx", ".ts", ".mts", ".cts", ".jsx", ".js", ".mjs", ".cjs"},
		isTest: func(b string) bool { return strings.Contains(b, ".test.") || strings.Contains(b, ".spec.") },
		candidates: func(s string) []string {
			var out []string
			for _, kind := range []string{".test", ".spec"} {
				for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
					out = append(out, s+kind+ext)
				}
			}
			return nonEmpty(s, out...)
		},
		helper:     func(f string) bool { return underDir(f, "test", "tests", "__tests__") },
		everything: func(*project.Project) string { return "" },
		nothing:    "",
	}
}

// nonEmpty returns the names, or none when the stem is empty.
func nonEmpty(stem string, names ...string) []string {
	if stem == "" {
		return nil
	}
	return names
}
