// Package python is the Python adapter: Ruff for lint and format, pyright strict, vulture, deptry, import-linter,
// pytest with random order, coverage with diff-cover, pip-audit, mutmut.
package python

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rojosinalma/pawl/internal/report"
	"github.com/rojosinalma/pawl/internal/stack"
	"github.com/rojosinalma/pawl/internal/stack/common"
)

// Pinned versions of the tools without a native pin file (Renovate moves them in mise.toml).
const (
	RuffVersion     = "0.17.5"
	PyrightVersion  = "1.1.414"
	VultureVersion  = "2.16"
	DeptryVersion   = "0.25.1"
	PipAuditVersion = "2.10.1"
	MutmutVersion   = "3.8.1"
)

type Adapter struct{}

func init() { stack.Register(Adapter{}) }

func (Adapter) Name() string  { return "python" }
func (Adapter) Title() string { return "Python" }

func (Adapter) Detect(files []string) bool {
	for _, f := range files {
		if f == "pyproject.toml" || f == "setup.py" || f == "requirements.txt" || strings.HasSuffix(f, ".py") {
			return true
		}
	}
	return false
}

func (Adapter) Facts(root string) map[string]string {
	facts := map[string]string{"runner": ""}
	if _, err := os.Stat(filepath.Join(root, "uv.lock")); err == nil {
		facts["runner"] = "uv run "
		facts["uv"] = "true"
	}
	if raw, err := os.ReadFile(filepath.Join(root, "pyproject.toml")); err == nil {
		s := string(raw)
		facts["has_pyproject"] = "true"
		facts["has_ruff_section"] = strconv.FormatBool(strings.Contains(s, "[tool.ruff"))
		facts["has_pytest_section"] = strconv.FormatBool(strings.Contains(s, "[tool.pytest"))
		if m := regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`).FindStringSubmatch(s); m != nil {
			facts["package"] = strings.ReplaceAll(m[1], "-", "_")
		}
	}
	for _, d := range []string{"src", "app", "lib"} {
		if st, err := os.Stat(filepath.Join(root, d)); err == nil && st.IsDir() {
			facts["source_dir"] = d
			break
		}
	}
	if facts["source_dir"] == "" {
		facts["source_dir"] = "."
	}
	return facts
}

func (Adapter) Tools() []stack.Tool {
	return []stack.Tool{
		{Name: "python", Check: []string{"python", "--version"}, Install: "the version in .python-version; mise use python", Rings: []string{"precommit", "check"}},
		{Name: "ruff", Version: RuffVersion, Check: []string{"ruff", "--version"}, Mise: "ruff", Install: "mise use ruff@" + RuffVersion, Rings: []string{"precommit", "check"}},
		{Name: "pyright", Version: PyrightVersion, Check: []string{"pyright", "--version"}, Mise: "npm:pyright", Install: "mise use npm:pyright@" + PyrightVersion, Rings: []string{"check"}},
		{Name: "vulture", Version: VultureVersion, Check: []string{"vulture", "--version"}, Mise: "pypi:vulture", Install: "mise use pypi:vulture@" + VultureVersion, Rings: []string{"check"}},
		{Name: "deptry", Version: DeptryVersion, Check: []string{"deptry", "--version"}, Mise: "pypi:deptry", Install: "mise use pypi:deptry@" + DeptryVersion, Rings: []string{"check"}},
		{Name: "pip-audit", Version: PipAuditVersion, Check: []string{"pip-audit", "--version"}, Mise: "pypi:pip-audit", Install: "mise use pypi:pip-audit@" + PipAuditVersion, Rings: []string{"nightly"}},
		{Name: "mutmut", Version: MutmutVersion, Check: []string{"mutmut", "version"}, Mise: "pypi:mutmut", Install: "mise use pypi:mutmut@" + MutmutVersion, Rings: []string{"nightly"}},
	}
}

func (Adapter) SourceGlobs() []string { return []string{"**/*.py", "**/*.pyi"} }
func (Adapter) TestGlobs() []string {
	return []string{"**/test_*.py", "**/*_test.py", "**/tests/**", "**/conftest.py"}
}

func (Adapter) Pins() []stack.Pin {
	return []stack.Pin{{
		What:  "Python version (.python-version)",
		Files: []string{".python-version", ".tool-versions", "mise.toml"},
		Locks: []string{"uv.lock", "poetry.lock", "pdm.lock", "requirements.lock", "requirements.txt"},
	}}
}

func (Adapter) Suppressions() []stack.Suppression {
	globs := []string{"**/*.py", "**/*.pyi"}
	return []stack.Suppression{
		{Kind: "noqa", Pattern: regexp.MustCompile(`#\s*noqa(?::\s*[\w, ]+)?(?:\s*(?:--|#)\s*(?P<reason>.+))?`), Globs: globs},
		{Kind: "type-ignore", Pattern: regexp.MustCompile(`#\s*type:\s*ignore(?:\[[\w, -]+\])?(?:\s*(?:--|#)\s*(?P<reason>.+))?`), Globs: globs},
		{Kind: "pyright-ignore", Pattern: regexp.MustCompile(`#\s*pyright:\s*ignore(?:\[[\w, ]+\])?(?:\s*(?:--|#)\s*(?P<reason>.+))?`), Globs: globs},
		{Kind: "nosec", Pattern: regexp.MustCompile(`#\s*nosec(?:\s+B\d+(?:\s*,\s*B\d+)*)?(?:\s*(?:--|#)\s*(?P<reason>.+))?`), Globs: globs},
	}
}

func (Adapter) Configs(p stack.Params) []stack.ConfigFile {
	out := []stack.ConfigFile{
		{Path: "pyrightconfig.json", Content: pyrightConfig},
		{Path: ".importlinter", Content: importLinterConfig},
	}
	if p.Facts["has_ruff_section"] != "true" {
		out = append(out, stack.ConfigFile{Path: "ruff.toml", Content: strings.ReplaceAll(ruffConfig, "{{complexity}}", strconv.Itoa(p.Complexity))})
	}
	if p.Facts["has_pytest_section"] != "true" {
		out = append(out, stack.ConfigFile{Path: "pytest.ini", Content: pytestConfig})
	}
	return out
}

// DevDependencies is the dependency group pawl appends to a pyproject.toml that lacks the test tooling.
const DevDependencies = `
# pawl: test tooling of this project, pinned here (the stack's native pin). Install with uv sync (or pip install -e .[dev]).
[dependency-groups]
dev = [
  "pytest==9.2.0",
  "pytest-randomly==5.1.0",
  "coverage==7.17.3",
]
`

func (Adapter) Rules(stack.Params) map[string]stack.Enforcement {
	lint := func(via ...string) stack.Enforcement { return stack.Enforcement{Step: "lint", Via: via} }
	sg := common.RuleIDs("python")
	return map[string]stack.Enforcement{
		"code.format":               {Step: "fmt-check", Via: []string{"ruff/format"}},
		"code.lint":                 lint("ruff"),
		"code.types":                lint("pyright/strict"),
		"code.complexity":           lint("ruff/C901"),
		"code.boundaries":           {Waived: stack.Reason("python", "no layer boundaries declared yet; declare contracts in .importlinter and set step = \"lint\"")},
		"code.dead":                 lint("vulture"),
		"code.dropped-results":      lint("ruff/RUF006", "ruff/B018"),
		"code.swallowed-errors":     lint("ruff/BLE001", "ruff/S110", "ruff/E722"),
		"code.debug-leftovers":      lint("ruff/T20", "ruff/ERA", "ruff/TD003"),
		"code.naming":               lint("ruff/N"),
		"escape.test-branch":        lint(sg["escape.test-branch"]...),
		"resilience.timeouts":       lint(append([]string{"ruff/S113"}, sg["resilience.timeouts"]...)...),
		"resilience.exhaustive":     lint("pyright/reportMatchNotExhaustive"),
		"resilience.config-owner":   lint(sg["resilience.config-owner"]...),
		"resilience.import-cycles":  {Waived: stack.Reason("python", "Ruff and import-linter do not detect cycles; pylint R0401 is the tool once adopted")},
		"resilience.bounded-growth": lint(sg["resilience.bounded-growth"]...),
		"tests.runner":              {Step: "test", Via: []string{"pytest-randomly"}},
		"tests.smells":              lint(append([]string{"ruff/PT"}, sg["tests.smells"]...)...),
		"tests.diff-coverage":       {Step: "coverage", Via: []string{"coverage", "diff-cover"}},
		"tests.mutation":            {Step: "mutation", Via: []string{"mutmut"}},
		"tests.mock-edges":          lint(sg["tests.mock-edges"]...),
		"drift.audit":               {Step: "audit", Via: []string{"pip-audit"}},
		"drift.unused-deps":         lint("deptry"),
		"drift.security":            lint("ruff/S"),
	}
}

func (Adapter) Verifiers(p stack.Params) []stack.Verifier {
	f, t := "ruff.toml", stack.TOML
	prefix := "lint"
	if p.Facts["has_ruff_section"] == "true" {
		f, prefix = "pyproject.toml", "tool.ruff.lint"
	}
	selected := func(rule, code string) stack.Verifier {
		return stack.V(rule, "ruff/"+code, stack.Exists(f), ruffSelects(f, t, prefix, code))
	}
	return []stack.Verifier{
		stack.V("code.lint", "ruff", stack.Exists(f)),
		stack.V("code.types", "pyright/strict", stack.Equal("pyrightconfig.json", stack.JSONC, "typeCheckingMode", "strict")),
		selected("code.complexity", "C901"),
		stack.V("code.complexity", "ruff/C901", stack.Equal(f, t, prefix+".mccabe.max-complexity", p.Complexity)),
		selected("code.dropped-results", "RUF"),
		selected("code.dropped-results", "B"),
		selected("code.swallowed-errors", "BLE"),
		selected("code.swallowed-errors", "S"),
		selected("code.swallowed-errors", "E"),
		selected("code.debug-leftovers", "T20"),
		selected("code.debug-leftovers", "ERA"),
		selected("code.debug-leftovers", "TD"),
		selected("code.naming", "N"),
		selected("tests.smells", "PT"),
		selected("drift.security", "S"),
		stack.V("resilience.exhaustive", "pyright/reportMatchNotExhaustive", stack.Equal("pyrightconfig.json", stack.JSONC, "typeCheckingMode", "strict")),
	}
}

// ruffSelects reports when the ruff selection lacks the code (as itself or as a prefix such as "S" for "S110") or
// ignores it.
func ruffSelects(file string, format stack.Format, prefix, code string) func(stack.Verify) []report.Finding {
	return func(v stack.Verify) []report.Finding {
		v.File, v.Format = file, format
		cfg, err := stack.LoadConfig(v.Root, file, format)
		if err != nil || cfg == nil {
			return nil // Exists() reports the missing file
		}
		selectedList := append(list(cfg, prefix+".select"), list(cfg, prefix+".extend-select")...)
		ok := false
		for _, s := range selectedList {
			if s == "ALL" || strings.HasPrefix(code, s) {
				ok = true
			}
		}
		if !ok {
			return []report.Finding{report.Errorf(v.RuleID, file, 0, "%s.select must include %q for %s", prefix, code, v.Via).WithFix("add it, or waive the rule in pawl.toml with a reason")}
		}
		for _, ig := range append(list(cfg, prefix+".ignore"), list(cfg, prefix+".extend-ignore")...) {
			if strings.HasPrefix(code, ig) || strings.HasPrefix(ig, code) {
				return []report.Finding{report.Errorf(v.RuleID, file, 0, "%s.ignore lists %q, which turns %s off", prefix, ig, v.Via).WithFix("remove it, or waive the rule in pawl.toml with a reason")}
			}
		}
		return nil
	}
}

func list(cfg *stack.Config, path string) []string {
	v, ok := cfg.Get(path)
	if !ok {
		return nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, strings.TrimSpace(strings.ToUpper(toString(i))))
	}
	return out
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (Adapter) Make(p stack.Params) stack.Make {
	run := p.Facts["runner"]
	src := p.Facts["source_dir"]
	audit := "pip-audit"
	if p.Facts["uv"] == "true" {
		audit = "uv export --no-hashes --format requirements-txt -o $(REPORTS)/requirements.txt && pip-audit -r $(REPORTS)/requirements.txt"
	}
	return stack.Make{
		Vars: []string{
			"PY_TEST_SCOPE ?= $(shell $(PAWL) affected python 2>/dev/null)",
			"STAGED_PY := $(filter %.py %.pyi,$(STAGED))",
		},
		Fmt:      []string{"ruff format .", "ruff check --fix-only ."},
		FmtCheck: []string{"ruff format --check ."},
		Lint: []string{
			"ruff check .",
			"pyright",
			"vulture " + src + " --min-confidence 80",
			"deptry .",
		},
		Test: []string{run + "pytest -p randomly $(PY_TEST_SCOPE)"},
		Precommit: []string{
			`@[ -z "$(STAGED_PY)" ] || ruff format --check $(STAGED_PY)`,
			`@[ -z "$(STAGED_PY)" ] || ruff check $(STAGED_PY)`,
		},
		Coverage: []string{
			"@mkdir -p $(REPORTS)",
			run + "coverage run -m pytest -p randomly",
			run + "coverage xml -o $(REPORTS)/python-coverage.xml",
			"diff-cover $(REPORTS)/python-coverage.xml --compare-branch=$(PAWL_BASE) --fail-under=$(DIFF_COVERAGE)",
		},
		Audit:    []string{"@mkdir -p $(REPORTS)", audit},
		Mutation: []string{run + "mutmut run"},
	}
}

const ruffConfig = `# Ruff configuration written by pawl (lint and format). Project-owned: change the selection here; pawl check
# verifies that the rule sets the catalog needs stay selected (or are waived in pawl.toml with a reason).
line-length = 100
indent-width = 4

[lint]
select = [
  "E", "W",   # pycodestyle
  "F",        # pyflakes
  "I",        # isort
  "UP",       # pyupgrade
  "B",        # bugbear (B018: useless expression, a dropped result)
  "S",        # bandit: security, S110 (try-except-pass), S113 (requests without timeout)
  "BLE",      # blind except
  "C90",      # mccabe complexity
  "N",        # pep8-naming
  "T20",      # print statements
  "ERA",      # commented-out code
  "TD",       # TODO format (TD003: needs an issue link)
  "PT",       # pytest style
  "RUF",      # Ruff's own (RUF006: dangling asyncio task)
]
ignore = ["TD002"]   # TODO author is not required; the link is

[lint.mccabe]
max-complexity = {{complexity}}

[lint.per-file-ignores]
"test_*.py" = ["S101", "T20"]
"*_test.py" = ["S101", "T20"]
"tests/**" = ["S101", "T20"]
"conftest.py" = ["S101"]

[format]
quote-style = "double"
docstring-code-format = true
`

const pyrightConfig = `{
  // pyright configuration written by pawl: the strictest level. Project-owned.
  "typeCheckingMode": "strict",
  "reportMatchNotExhaustive": "error",
  "exclude": ["**/node_modules", "**/__pycache__", ".venv", "build", "dist", ".pawl"]
}
`

const importLinterConfig = `# import-linter configuration written by pawl. Declare layer boundaries as contracts here and set
# [rules."code.boundaries"] step = "lint" in pawl.toml; the Makefile lint target then runs lint-imports.
[importlinter]
root_package = .

# [importlinter:contract:layers]
# name = Layers import one way
# type = layers
# layers =
#     mypackage.api
#     mypackage.domain
#     mypackage.storage
`

const pytestConfig = `# pytest configuration written by pawl: random order (pytest-randomly), no retries, strict markers.
[pytest]
addopts = -p randomly --strict-markers
testpaths = tests
`
