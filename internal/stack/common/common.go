// Package common is the adapter every project gets: the cross-stack tools (ast-grep, jscpd, typos, gitleaks,
// diff-cover), the ast-grep rule pack, and the catalog rules pawl itself checks.
package common

import (
	"bytes"
	"embed"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/rojosinalma/pawl/internal/stack"
)

//go:embed rules/*.yml.tmpl
var ruleTemplates embed.FS

// Versions pinned for the cross-stack tools; Renovate moves them through mise.toml in the project.
const (
	AstGrepVersion   = "0.51.2"
	JscpdVersion     = "5.4.3"
	TyposVersion     = "1.52.2"
	GitleaksVersion  = "8.30.1"
	DiffCoverVersion = "11.0.2"
)

// Adapter is the common adapter.
type Adapter struct{}

func init() { stack.Register(Adapter{}) }

func (Adapter) Name() string                      { return "common" }
func (Adapter) Title() string                     { return "every stack" }
func (Adapter) Detect([]string) bool              { return true }
func (Adapter) Facts(string) map[string]string    { return nil }
func (Adapter) SourceGlobs() []string             { return nil }
func (Adapter) TestGlobs() []string               { return nil }
func (Adapter) Pins() []stack.Pin                 { return nil }
func (Adapter) Suppressions() []stack.Suppression { return suppressions }

var suppressions = []stack.Suppression{
	{Kind: "ast-grep-ignore", Pattern: regexp.MustCompile(`ast-grep-ignore(?::\s*[\w,-]+)?(?:\s*--\s*(?P<reason>.+))?`), Globs: []string{"**/*"}},
	{Kind: "typos-ignore", Pattern: regexp.MustCompile(`typos:\s*ignore(?:\s*--\s*(?P<reason>.+))?`), Globs: []string{"**/*"}},
	{Kind: "gitleaks-allow", Pattern: regexp.MustCompile(`gitleaks:allow(?:\s*(?:--|#|//)\s*(?P<reason>.+))?`), Globs: []string{"**/*"}},
}

func (Adapter) Tools() []stack.Tool {
	return []stack.Tool{
		{Name: "ast-grep", Version: AstGrepVersion, Check: []string{"ast-grep", "--version"}, Mise: "ast-grep", Install: "mise use ast-grep@" + AstGrepVersion + " (or cargo install ast-grep)", Rings: []string{"check"}},
		{Name: "jscpd", Version: JscpdVersion, Check: []string{"jscpd", "--version"}, Mise: "npm:jscpd", Install: "mise use npm:jscpd@" + JscpdVersion, Rings: []string{"check"}},
		{Name: "typos", Version: TyposVersion, Check: []string{"typos", "--version"}, Mise: "typos", Install: "mise use typos@" + TyposVersion, Rings: []string{"check"}},
		{Name: "gitleaks", Version: GitleaksVersion, Check: []string{"gitleaks", "version"}, Mise: "gitleaks", Install: "mise use gitleaks@" + GitleaksVersion, Rings: []string{"precommit", "check"}},
		{Name: "diff-cover", Version: DiffCoverVersion, Check: []string{"diff-cover", "--version"}, Mise: "pypi:diff-cover", Install: "mise use pypi:diff-cover@" + DiffCoverVersion, Rings: []string{"check"}},
	}
}

type ruleParams struct {
	ConfigModule       string
	ConfigModuleOrHint string
	OwnModule          string
	TestDirs           []string
}

func (Adapter) Configs(p stack.Params) []stack.ConfigFile {
	rp := ruleParams{ConfigModule: strings.TrimSuffix(p.ConfigModule, "/"), OwnModule: regexp.QuoteMeta(p.OwnModule), TestDirs: p.TestDirs}
	rp.ConfigModuleOrHint = rp.ConfigModule
	if rp.ConfigModuleOrHint == "" {
		rp.ConfigModuleOrHint = "set project.config_module in pawl.toml"
	}
	out := []stack.ConfigFile{
		{Path: ".pawl/sgconfig.yml", Content: "# ast-grep project configuration for the pawl rule pack. `ast-grep scan -c .pawl/sgconfig.yml`.\nruleDirs:\n  - rules\n"},
		{Path: ".jscpd.json", Content: jscpdConfig},
		{Path: ".typos.toml", Content: typosConfig},
		{Path: ".gitleaks.toml", Content: gitleaksConfig},
	}
	for _, f := range RulePack(rp) {
		out = append(out, f)
	}
	return out
}

// RulePack renders the embedded ast-grep rules for the project.
func RulePack(rp ruleParams) []stack.ConfigFile {
	entries, err := ruleTemplates.ReadDir("rules")
	if err != nil {
		panic(err)
	}
	var out []stack.ConfigFile
	for _, e := range entries {
		raw, err := ruleTemplates.ReadFile("rules/" + e.Name())
		if err != nil {
			panic(err)
		}
		t, err := template.New(e.Name()).Parse(string(raw))
		if err != nil {
			panic(fmt.Sprintf("rule template %s: %v", e.Name(), err))
		}
		var b bytes.Buffer
		if err := t.Execute(&b, rp); err != nil {
			panic(fmt.Sprintf("rule template %s: %v", e.Name(), err))
		}
		name := strings.TrimSuffix(e.Name(), ".tmpl")
		out = append(out, stack.ConfigFile{Path: filepath.ToSlash(filepath.Join(".pawl", "rules", name)), Content: strings.TrimRight(b.String(), "\n") + "\n"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// RuleIDs lists the ast-grep rule ids the pack contains for a stack name, by catalog rule.
func RuleIDs(stackName string) map[string][]string {
	suffix := map[string]string{"go": "go", "typescript": "ts", "python": "py", "ruby": "rb", "rust": "rs"}[stackName]
	if suffix == "" {
		return nil
	}
	out := map[string][]string{}
	add := func(rule, id string) { out[rule] = append(out[rule], "ast-grep/"+id) }
	add("escape.test-branch", "test-branch-"+suffix)
	add("resilience.config-owner", "config-owner-"+suffix)
	add("resilience.bounded-growth", "bounded-growth-"+suffix)
	switch stackName {
	case "go":
		add("tests.smells", "skip-without-reason-go")
	case "rust":
		add("tests.smells", "skip-without-reason-rs")
		add("resilience.timeouts", "reqwest-without-timeout-rs")
	case "python":
		add("tests.smells", "skip-without-reason-py")
		add("resilience.timeouts", "urlopen-without-timeout-py")
		add("tests.mock-edges", "mock-own-module-py")
	case "typescript":
		add("resilience.timeouts", "fetch-without-timeout-ts")
		add("tests.mock-edges", "mock-own-module-ts")
		add("drift.security", "dynamic-code-ts")
	case "ruby":
		add("resilience.timeouts", "http-without-timeout-rb")
		add("tests.mock-edges", "mock-own-module-rb")
		add("drift.security", "dynamic-code-rb")
	}
	return out
}

func (Adapter) Rules(stack.Params) map[string]stack.Enforcement {
	pawl := stack.Enforcement{Step: "check", Via: []string{"pawl"}}
	return map[string]stack.Enforcement{
		"code.file-size":      pawl,
		"escape.suppressions": pawl,
		"escape.duplication":  {Step: "check", Via: []string{"jscpd"}},
		"repo.private":        {Step: "lint", Via: []string{"gitleaks", "pawl"}},
		"repo.pinned":         pawl,
		"repo.hook":           pawl,
		"repo.targets":        pawl,
		"repo.ci-shape":       pawl,
		"drift.pr-size":       pawl,
		"speed.rings":         pawl,
		"speed.affected":      pawl,
		"tests.principles":    pawl,
		"code.naming":          {Step: "lint", Via: []string{"typos"}},
	}
}

func (Adapter) Verifiers(stack.Params) []stack.Verifier {
	return []stack.Verifier{
		stack.V("escape.duplication", "jscpd", stack.Exists(".jscpd.json")),
		stack.V("repo.private", "gitleaks", stack.Exists(".gitleaks.toml")),
		stack.V("code.naming", "typos", stack.Exists(".typos.toml")),
		stack.V("escape.test-branch", "ast-grep", stack.Exists(".pawl/sgconfig.yml"), stack.Exists(".pawl/rules/test-branch.yml")),
		stack.V("resilience.config-owner", "ast-grep", stack.Exists(".pawl/rules/config-owner.yml")),
		stack.V("resilience.bounded-growth", "ast-grep", stack.Exists(".pawl/rules/bounded-growth.yml")),
		stack.V("tests.mock-edges", "ast-grep", stack.Exists(".pawl/rules/mock-edges.yml")),
	}
}

func (Adapter) Make(stack.Params) stack.Make {
	return stack.Make{
		Vars: []string{
			"PAWL ?= pawl",
			"STAGED := $(shell git diff --cached --name-only --diff-filter=ACMR)",
			"REPORTS := .pawl/reports",
		},
		Lint: []string{
			"ast-grep scan -c .pawl/sgconfig.yml",
			"typos",
			"gitleaks git . --redact --no-banner --exit-code 1",
		},
		Precommit: []string{
			`@[ -z "$(STAGED)" ] || git diff --cached | gitleaks stdin --redact --no-banner --exit-code 1`,
		},
	}
}

const jscpdConfig = `{
  "$schema": "https://raw.githubusercontent.com/kucherenko/jscpd/master/packages/jscpd/schema.json",
  "minLines": 8,
  "minTokens": 60,
  "ignore": ["**/node_modules/**", "**/vendor/**", "**/dist/**", "**/.pawl/**", "**/*.lock", "**/*.min.*", "**/testdata/**", "**/fixtures/**"],
  "gitignore": true,
  "absolute": false
}
`

const typosConfig = `# typos: identifier and comment spell check. Add project words under [default.extend-words].
[files]
extend-exclude = ["*.lock", "*.min.*", "vendor/", "node_modules/", ".pawl/reports/", "testdata/", "fixtures/"]

[default]
extend-ignore-re = ["[0-9a-f]{7,40}"]

[default.extend-words]
`

const gitleaksConfig = `# gitleaks: secret detection. Extends the built-in rules; add project allowlists below.
title = "project gitleaks configuration"

[extend]
useDefault = true

[[allowlists]]
description = "lockfiles and vendored dependencies carry hashes, not secrets"
paths = [
  '''(^|/)(package-lock\.json|bun\.lock|bun\.lockb|yarn\.lock|pnpm-lock\.yaml|Gemfile\.lock|uv\.lock|poetry\.lock|go\.sum|Cargo\.lock|mise\.lock)$''',
  '''(^|/)(vendor|node_modules)/''',
]
`
