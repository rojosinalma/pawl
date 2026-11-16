// Package golang is the Go stack adapter: golangci-lint v2 (lint, formatters, security, complexity), the compiler
// for types and import cycles, go test with shuffling, govulncheck, gremlins.
package golang

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

// GolangciLintVersion is the pinned golangci-lint release.
const GolangciLintVersion = "2.15.0"

// Go module tool versions pinned by `go get -tool`.
const (
	GovulncheckModule = "golang.org/x/vuln/cmd/govulncheck@v1.7.0"
	Gcov2lcovModule   = "github.com/jandelgado/gcov2lcov@v1.1.1"
	GremlinsVersion   = "0.6.0"
)

type Adapter struct{}

func init() { stack.Register(Adapter{}) }

func (Adapter) Name() string  { return "go" }
func (Adapter) Title() string { return "Go" }

func (Adapter) Detect(files []string) bool {
	for _, f := range files {
		if f == "go.mod" || strings.HasSuffix(f, ".go") {
			return true
		}
	}
	return false
}

var modulePattern = regexp.MustCompile(`(?m)^module\s+(\S+)`)

func (Adapter) Facts(root string) map[string]string {
	facts := map[string]string{}
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err == nil {
		if m := modulePattern.FindSubmatch(raw); m != nil {
			facts["module"] = string(m[1])
		}
	}
	return facts
}

func (Adapter) Tools() []stack.Tool {
	return []stack.Tool{
		{Name: "go", Check: []string{"go", "version"}, Install: "the version in go.mod; mise use go, or https://go.dev/dl", Rings: []string{"precommit", "check"}},
		{Name: "golangci-lint", Version: GolangciLintVersion, Check: []string{"golangci-lint", "version", "--short"}, Mise: "golangci-lint", Install: "mise use golangci-lint@" + GolangciLintVersion, Rings: []string{"precommit", "check"}},
		{Name: "gremlins", Version: GremlinsVersion, Check: []string{"gremlins", "--version"}, Mise: "github:go-gremlins/gremlins", Install: "mise use github:go-gremlins/gremlins@" + GremlinsVersion, Rings: []string{"nightly"}},
	}
}

func (Adapter) SourceGlobs() []string { return []string{"**/*.go"} }
func (Adapter) TestGlobs() []string   { return []string{"**/*_test.go"} }

func (Adapter) Pins() []stack.Pin {
	return []stack.Pin{{What: "Go version (go.mod `go` directive)", Files: []string{"go.mod"}, Locks: []string{"go.sum"}}}
}

func (Adapter) Suppressions() []stack.Suppression {
	return []stack.Suppression{
		{Kind: "nolint", Pattern: regexp.MustCompile(`//\s*nolint(?::[\w,./-]+)?(?:\s*//\s*(?P<reason>.+))?`), Globs: []string{"**/*.go"}},
		{Kind: "nosec", Pattern: regexp.MustCompile(`#nosec(?:\s+G\d+(?:\s*,\s*G\d+)*)?(?:\s*(?:--|//)\s*(?P<reason>.+))?`), Globs: []string{"**/*.go"}},
		{Kind: "lint:ignore", Pattern: regexp.MustCompile(`//\s*lint:ignore\s+\S+(?:\s+(?P<reason>.+))?`), Globs: []string{"**/*.go"}},
	}
}

func (Adapter) Configs(p stack.Params) []stack.ConfigFile {
	cfg := strings.ReplaceAll(golangciConfig, "{{complexity}}", strconv.Itoa(p.Complexity))
	return []stack.ConfigFile{{Path: ".golangci.yml", Content: cfg}}
}

func (Adapter) Rules(stack.Params) map[string]stack.Enforcement {
	lint := func(via ...string) stack.Enforcement { return stack.Enforcement{Step: "lint", Via: via} }
	sg := common.RuleIDs("go")
	return map[string]stack.Enforcement{
		"code.format":               {Step: "fmt-check", Via: []string{"golangci-lint/gofmt", "golangci-lint/goimports"}},
		"code.lint":                 lint("golangci-lint"),
		"code.types":                lint("golangci-lint/typecheck"),
		"code.complexity":           lint("golangci-lint/gocognit"),
		"code.boundaries":           {Waived: stack.Reason("go", "no layer boundaries declared yet; declare them under linters.settings.depguard in .golangci.yml, enable depguard, and set step = \"lint\"")},
		"code.dead":                 lint("golangci-lint/unused"),
		"code.dropped-results":      lint("golangci-lint/errcheck"),
		"code.swallowed-errors":     lint("golangci-lint/nilerr", "golangci-lint/errcheck"),
		"code.debug-leftovers":      lint("golangci-lint/forbidigo"),
		"code.naming":               lint("golangci-lint/revive"),
		"escape.test-branch":        lint(sg["escape.test-branch"]...),
		"resilience.timeouts":       lint("golangci-lint/noctx", "golangci-lint/bodyclose"),
		"resilience.exhaustive":     lint("golangci-lint/exhaustive"),
		"resilience.config-owner":   lint(sg["resilience.config-owner"]...),
		"resilience.import-cycles":  lint("go/compiler"),
		"resilience.bounded-growth": lint(sg["resilience.bounded-growth"]...),
		"tests.runner":              {Step: "test", Via: []string{"go test -shuffle=on"}},
		"tests.smells":              lint(append([]string{"golangci-lint/testifylint"}, sg["tests.smells"]...)...),
		"tests.diff-coverage":       {Step: "coverage", Via: []string{"diff-cover"}},
		"tests.mutation":            {Step: "mutation", Via: []string{"gremlins"}},
		"tests.mock-edges":          {Waived: stack.Reason("go", "tests substitute interfaces, which are the declared edges; nothing to detect")},
		"drift.audit":               {Step: "audit", Via: []string{"govulncheck"}},
		"drift.unused-deps":         lint("go mod tidy -diff"),
		"drift.security":            lint("golangci-lint/gosec"),
	}
}

func (Adapter) Verifiers(p stack.Params) []stack.Verifier {
	f, y := ".golangci.yml", stack.YAML
	on := func(rule, linter string) stack.Verifier {
		return stack.V(rule, "golangci-lint/"+linter, linterOn(linter))
	}
	return []stack.Verifier{
		stack.V("code.format", "golangci-lint/gofmt", stack.Has(f, y, "formatters.enable", "gofmt")),
		stack.V("code.format", "golangci-lint/goimports", stack.Has(f, y, "formatters.enable", "goimports")),
		stack.V("code.lint", "golangci-lint", stack.Exists(f), stack.Equal(f, y, "version", "2")),
		on("code.complexity", "gocognit"),
		stack.V("code.complexity", "golangci-lint/gocognit", stack.Equal(f, y, "linters.settings.gocognit.min-complexity", p.Complexity)),
		on("code.dead", "unused"),
		on("code.dropped-results", "errcheck"),
		on("code.swallowed-errors", "nilerr"),
		on("code.debug-leftovers", "forbidigo"),
		stack.V("code.debug-leftovers", "golangci-lint/forbidigo", stack.Matches(f, regexp.MustCompile(`(?m)^\s*-\s*pattern:.*fmt`), "a forbidigo pattern for fmt.Print")),
		on("code.naming", "revive"),
		on("resilience.timeouts", "noctx"),
		on("resilience.timeouts", "bodyclose"),
		on("resilience.exhaustive", "exhaustive"),
		on("tests.smells", "testifylint"),
		on("drift.security", "gosec"),
	}
}

// standardLinters are the linters golangci-lint v2 runs with `linters.default: standard`.
var standardLinters = map[string]bool{"errcheck": true, "govet": true, "ineffassign": true, "staticcheck": true, "unused": true}

// linterOn reports when .golangci.yml does not run the linter: it is listed under linters.disable, or neither
// linters.enable nor linters.default (all, or standard for the standard set) turns it on.
func linterOn(linter string) func(stack.Verify) []report.Finding {
	const file = ".golangci.yml"
	return func(v stack.Verify) []report.Finding {
		cfg, err := stack.LoadConfig(v.Root, file, stack.YAML)
		if err != nil {
			return []report.Finding{report.Errorf(v.RuleID, file, 0, "cannot parse: %v", err)}
		}
		if cfg == nil {
			return stack.Exists(file)(v)
		}
		if cfg.Contains("linters.disable", linter) {
			return []report.Finding{report.Errorf(v.RuleID, file, 0, "linters.disable lists %q, which turns %s off", linter, v.Via).WithFix("remove it, or waive the rule in pawl.toml with a reason")}
		}
		def, _ := cfg.Get("linters.default")
		if def == nil {
			def = "standard"
		}
		if cfg.Contains("linters.enable", linter) || def == "all" || (def == "standard" && standardLinters[linter]) {
			return nil
		}
		return []report.Finding{report.Errorf(v.RuleID, file, 0, "%s does not run (linters.default is %v and linters.enable lacks it)", v.Via, def).WithFix("add it to linters.enable, or waive the rule in pawl.toml with a reason")}
	}
}

func (Adapter) Make(stack.Params) stack.Make {
	return stack.Make{
		Vars: []string{
			"GO_PKGS ?= ./...",
			"GO_TEST_SCOPE ?= $(shell $(PAWL) affected go 2>/dev/null || echo ./...)",
			"STAGED_GO := $(filter %.go,$(STAGED))",
		},
		Fmt:      []string{"golangci-lint fmt $(GO_PKGS)"},
		FmtCheck: []string{"golangci-lint fmt --diff $(GO_PKGS)"},
		Lint: []string{
			"golangci-lint run $(GO_PKGS)",
			"go mod tidy -diff",
		},
		Test: []string{"go test -shuffle=on -race -count=1 $(GO_TEST_SCOPE)"},
		Precommit: []string{
			`@[ -z "$(STAGED_GO)" ] || golangci-lint fmt --diff $(STAGED_GO)`,
			`@[ -z "$(STAGED_GO)" ] || golangci-lint run --fast-only $(addprefix ./,$(sort $(dir $(STAGED_GO))))`,
		},
		Coverage: []string{
			"@mkdir -p $(REPORTS)",
			"go test -shuffle=on -count=1 -coverprofile=$(REPORTS)/go-coverage.out $(GO_PKGS)",
			"go tool gcov2lcov -infile $(REPORTS)/go-coverage.out -outfile $(REPORTS)/go-coverage.lcov",
			"diff-cover $(REPORTS)/go-coverage.lcov --compare-branch=$(PAWL_BASE) --fail-under=$(DIFF_COVERAGE)",
		},
		Audit:    []string{"go tool govulncheck ./..."},
		Mutation: []string{"gremlins unleash --tags=''"},
	}
}

const golangciConfig = `# golangci-lint v2 configuration written by pawl. Project-owned: change thresholds and rules here; pawl check
# verifies that the rules the catalog needs stay on (or are waived in pawl.toml with a reason).
version: "2"

linters:
  default: standard
  enable:
    - bodyclose      # resilience.timeouts: response bodies are closed
    - errorlint      # errors are wrapped and compared with errors.Is/As
    - exhaustive     # resilience.exhaustive
    - forbidigo      # code.debug-leftovers: no fmt.Print* outside main packages
    - gocognit       # code.complexity
    - gosec          # drift.security
    - misspell
    - nilerr         # code.swallowed-errors: a nil error is not returned when err != nil
    - noctx          # resilience.timeouts: requests carry a context
    - revive         # code.naming
    - testifylint    # tests.smells
  settings:
    gocognit:
      min-complexity: {{complexity}}
    forbidigo:
      forbid:
        - pattern: ^fmt\.Print(f|ln)?$
          msg: use the logger or return the value; printing belongs to the entry point
      exclude-godoc-examples: true
    revive:
      rules:
        - name: var-naming
        - name: exported
          arguments: ["checkPrivateReceivers"]
        - name: receiver-naming
        - name: error-naming
        - name: error-strings
        - name: increment-decrement
        - name: indent-error-flow
    exhaustive:
      default-signifies-exhaustive: true
  exclusions:
    generated: lax
    presets:
      - std-error-handling
    rules:
      - path: _test\.go
        linters: [gosec, forbidigo]
      - path: (^cmd/|(^|/)main\.go$)
        linters: [forbidigo]

formatters:
  enable:
    - gofmt
    - goimports

issues:
  max-issues-per-linter: 0
  max-same-issues: 0
  uniq-by-line: false

run:
  timeout: 5m
`
