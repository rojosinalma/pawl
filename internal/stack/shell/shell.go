// Package shell is the adapter for shell scripts, Dockerfiles and GitHub Actions workflows: ShellCheck, shfmt,
// hadolint, actionlint, zizmor, bats.
package shell

import (
	"regexp"

	"github.com/rojosinalma/pawl/internal/stack"
)

// Pinned versions (Renovate moves them in mise.toml).
const (
	ShellcheckVersion = "0.11.0"
	ShfmtVersion      = "3.14.1"
	HadolintVersion   = "2.15.1"
	ActionlintVersion = "1.7.12"
	ZizmorVersion     = "1.32.0"
	BatsVersion       = "1.14.0"
)

type Adapter struct{}

func init() { stack.Register(Adapter{}) }

func (Adapter) Name() string  { return "shell" }
func (Adapter) Title() string { return "Shell, Docker and GitHub Actions" }

var scriptGlobs = []string{"**/*.sh", "**/*.bash", "bin/*", "scripts/*", ".githooks/*"}
var dockerGlobs = []string{"**/Dockerfile", "**/Dockerfile.*", "**/*.dockerfile", "**/Containerfile"}
var workflowGlobs = []string{".github/workflows/*.yml", ".github/workflows/*.yaml"}

func (Adapter) Detect(files []string) bool {
	return stack.HasAny(files, []string{"**/*.sh", "**/*.bash"}) || stack.HasAny(files, dockerGlobs) || stack.HasAny(files, workflowGlobs)
}

func (Adapter) Facts(string) map[string]string { return nil }

func (Adapter) Tools() []stack.Tool {
	return []stack.Tool{
		{Name: "shellcheck", Version: ShellcheckVersion, Check: []string{"shellcheck", "--version"}, Mise: "shellcheck", Install: "mise use shellcheck@" + ShellcheckVersion, Rings: []string{"precommit", "check"}},
		{Name: "shfmt", Version: ShfmtVersion, Check: []string{"shfmt", "--version"}, Mise: "shfmt", Install: "mise use shfmt@" + ShfmtVersion, Rings: []string{"precommit", "check"}},
		{Name: "hadolint", Version: HadolintVersion, Check: []string{"hadolint", "--version"}, Mise: "hadolint", Install: "mise use hadolint@" + HadolintVersion, Rings: []string{"check"}},
		{Name: "actionlint", Version: ActionlintVersion, Check: []string{"actionlint", "-version"}, Mise: "aqua:rhysd/actionlint", Install: "mise use aqua:rhysd/actionlint@" + ActionlintVersion, Rings: []string{"check"}},
		{Name: "bats", Version: BatsVersion, Check: []string{"bats", "--version"}, Mise: "bats", Install: "mise use bats@" + BatsVersion, Rings: []string{"check"}},
		{Name: "zizmor", Version: ZizmorVersion, Check: []string{"zizmor", "--version"}, Mise: "zizmor", Install: "mise use zizmor@" + ZizmorVersion, Rings: []string{"check"}},
	}
}

func (Adapter) SourceGlobs() []string {
	return append(append([]string{}, scriptGlobs...), dockerGlobs...)
}
func (Adapter) TestGlobs() []string { return []string{"**/*.bats", "test/**", "tests/**"} }

func (Adapter) Pins() []stack.Pin {
	return []stack.Pin{{What: "tool versions for shell, Docker and Actions linters (mise.toml)", Files: []string{"mise.toml", ".tool-versions"}}}
}

func (Adapter) Suppressions() []stack.Suppression {
	return []stack.Suppression{
		{Kind: "shellcheck-disable", Pattern: regexp.MustCompile(`shellcheck\s+disable=[\w,]+(?:\s*(?:--|#)\s*(?P<reason>.+))?`), Globs: scriptGlobs},
		{Kind: "hadolint-ignore", Pattern: regexp.MustCompile(`hadolint\s+ignore=[\w,]+(?:\s*(?:--|#)\s*(?P<reason>.+))?`), Globs: dockerGlobs},
		{Kind: "zizmor-ignore", Pattern: regexp.MustCompile(`zizmor:\s*ignore\[[\w-]+\](?:\s*(?:--|#)\s*(?P<reason>.+))?`), Globs: workflowGlobs},
	}
}

func (Adapter) Configs(stack.Params) []stack.ConfigFile {
	return []stack.ConfigFile{
		{Path: ".shellcheckrc", Content: shellcheckrc},
		{Path: ".editorconfig", Content: editorconfig},
		{Path: ".hadolint.yaml", Content: hadolintConfig},
		{Path: ".github/actionlint.yaml", Content: actionlintConfig},
		{Path: ".github/zizmor.yml", Content: zizmorConfig},
	}
}

func (Adapter) Rules(stack.Params) map[string]stack.Enforcement {
	lint := func(via ...string) stack.Enforcement { return stack.Enforcement{Step: "lint", Via: via} }
	waive := func(what string) stack.Enforcement { return stack.Enforcement{Waived: stack.Reason("shell", what)} }
	return map[string]stack.Enforcement{
		"code.format":               {Step: "fmt-check", Via: []string{"shfmt"}},
		"code.lint":                 lint("shellcheck", "hadolint", "actionlint", "zizmor"),
		"code.types":                waive("shell has no type checker; ShellCheck covers quoting and expansion"),
		"code.complexity":           waive("no complexity tool for shell; a script over 100 lines is rewritten in a structured language"),
		"code.boundaries":           waive("shell scripts have no import graph"),
		"code.dead":                 lint("shellcheck/SC2034"),
		"code.dropped-results":      lint("ast-grep/shell-strict-mode"),
		"code.swallowed-errors":     waive("shell has no exceptions; strict mode (code.dropped-results) stops on the first failure"),
		"code.debug-leftovers":      waive("echo is output in shell; no tool separates debugging from output"),
		"code.naming":               waive("no naming lint for shell; ShellCheck covers unassigned and unused variables"),
		"escape.test-branch":        waive("shell scripts rarely have a test environment; declare one with ast-grep if needed"),
		"resilience.timeouts":       waive("wrap long network calls in `timeout`; no static check"),
		"resilience.exhaustive":     waive("case statements have no closed set; add `*) exit 1 ;;`"),
		"resilience.config-owner":   waive("scripts read the environment by nature"),
		"resilience.import-cycles":  waive("shell scripts have no import graph"),
		"resilience.bounded-growth": waive("shell scripts hold no long-lived state"),
		"tests.runner":              {Step: "test", Via: []string{"bats"}},
		"tests.smells":              waive("no shell tests declared"),
		"tests.diff-coverage":       waive("no coverage tool for shell"),
		"tests.mutation":            waive("no mutation tool for shell"),
		"tests.mock-edges":          waive("no shell tests declared"),
		"drift.audit":               waive("shell scripts have no dependency manifest"),
		"drift.unused-deps":         waive("shell scripts have no dependency manifest"),
		"drift.security":            lint("shellcheck", "zizmor", "hadolint", "docker-user"),
	}
}

func (Adapter) Verifiers(stack.Params) []stack.Verifier {
	return []stack.Verifier{
		stack.V("code.lint", "shellcheck", stack.Exists(".shellcheckrc")),
		stack.V("code.lint", "hadolint", stack.Exists(".hadolint.yaml"), stack.NotEqual(".hadolint.yaml", stack.YAML, "no-fail", true),
			stack.NotEqual(".hadolint.yaml", stack.YAML, "failure-threshold", "error", "none")),
		stack.V("drift.security", "zizmor/unpinned-uses", stack.NotEqual(".github/zizmor.yml", stack.YAML, "rules.unpinned-uses.disable", true)),
		stack.V("drift.security", "zizmor/template-injection", stack.NotEqual(".github/zizmor.yml", stack.YAML, "rules.template-injection.disable", true)),
		stack.V("code.format", "shfmt", stack.Exists(".editorconfig")),
		stack.V("code.dead", "shellcheck/SC2034", stack.NotMatches(".shellcheckrc", regexp.MustCompile(`(?m)^\s*disable=.*SC2034`), "a disable line for SC2034 (unused variables)")),
	}
}

func (Adapter) Make(stack.Params) stack.Make {
	find := `$(shell git ls-files -co --exclude-standard | grep -vE '$(PAWL_EXCLUDE)' | xargs -r shfmt -f 2>/dev/null)`
	return stack.Make{
		Vars: []string{
			"SH_FILES := " + find,
			"DOCKERFILES := $(shell git ls-files -co --exclude-standard | grep -vE '$(PAWL_EXCLUDE)' | grep -E '(^|/)(Dockerfile(\\..*)?|Containerfile|.*\\.dockerfile)$$')",
			"WORKFLOWS := $(wildcard .github/workflows/*.yml .github/workflows/*.yaml)",
			"BATS_FILES := $(filter %.bats,$(SH_FILES))",
			"STAGED_SH := $(filter $(SH_FILES),$(STAGED))",
		},
		Fmt:      []string{`@[ -z "$(SH_FILES)" ] || shfmt -w $(SH_FILES)`},
		FmtCheck: []string{`@[ -z "$(SH_FILES)" ] || shfmt -d $(SH_FILES)`},
		Lint: []string{
			`@[ -z "$(SH_FILES)" ] || shellcheck $(SH_FILES)`,
			`@[ -z "$(DOCKERFILES)" ] || hadolint $(DOCKERFILES)`,
			`@[ -z "$(WORKFLOWS)" ] || actionlint $(WORKFLOWS)`,
			`@[ -z "$(WORKFLOWS)" ] || zizmor --no-progress --min-severity low .github/workflows`,
			dockerUser,
		},
		StepNames: map[string]string{dockerUser: "docker-user"},
		Test:      []string{`@[ -z "$(BATS_FILES)" ] || bats $(BATS_FILES)`},
		Precommit: []string{
			`@[ -z "$(STAGED_SH)" ] || shfmt -d $(STAGED_SH)`,
			`@[ -z "$(STAGED_SH)" ] || shellcheck $(STAGED_SH)`,
		},
	}
}

// dockerUser fails for a Dockerfile without a USER instruction (hadolint only checks the last USER is not root).
const dockerUser = `@s=0; for f in $(DOCKERFILES); do grep -qiE '^[[:space:]]*USER[[:space:]]' "$$f" ` +
	`|| { echo "$$f: no USER instruction, the container runs as root (docker-user)"; s=1; }; done; exit $$s`

const shellcheckrc = `# ShellCheck configuration written by pawl. The optional checks follow the Google Shell Style Guide.
external-sources=true
enable=require-double-brackets
enable=require-variable-braces
enable=deprecate-which
enable=avoid-nullary-conditions
enable=check-unassigned-uppercase
enable=add-default-case
`

const editorconfig = `# EditorConfig written by pawl. shfmt reads its options from here (indent, case indent, binary operators).
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
trim_trailing_whitespace = true
indent_style = space
indent_size = 2

[*.{sh,bash}]
shell_variant = bash
binary_next_line = true
switch_case_indent = true
space_redirects = false

[*.py]
indent_size = 4

[*.go]
indent_style = tab

[Makefile]
indent_style = tab

[*.md]
trim_trailing_whitespace = false
`

const hadolintConfig = `# hadolint configuration written by pawl. Severity per rule: override.error / warning / info / style.
failure-threshold: warning
ignored: []
override:
  error: []
  warning: []
  info: []
  style: []
`

const actionlintConfig = `# actionlint configuration written by pawl. Add self-hosted runner labels under self-hosted-runner.labels.
self-hosted-runner:
  labels: []
config-variables: null
`

const zizmorConfig = `# zizmor configuration written by pawl. Per rule: rules.<id>.disable, rules.<id>.ignore, rules.<id>.config.
rules: {}
`
