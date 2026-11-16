// Package typescript is the TypeScript and JavaScript adapter (Bun first, Node accepted): Biome for lint and format,
// oxlint with tsgolint for the type-aware promise rules, tsc strict, knip, dependency-cruiser, bun audit, Stryker.
package typescript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rojosinalma/pawl/internal/stack"
	"github.com/rojosinalma/pawl/internal/stack/common"
)

// Pinned versions of the tools without a native pin file (Renovate moves them in mise.toml).
const (
	BiomeVersion             = "2.6.8"
	OxlintVersion            = "1.93.0"
	KnipVersion              = "6.45.1"
	DependencyCruiserVersion = "18.7.2"
	StrykerVersion           = "10.1.1"
)

type Adapter struct{}

func init() { stack.Register(Adapter{}) }

func (Adapter) Name() string  { return "typescript" }
func (Adapter) Title() string { return "TypeScript / JavaScript" }

func (Adapter) Detect(files []string) bool {
	for _, f := range files {
		if f == "package.json" || f == "tsconfig.json" {
			return true
		}
	}
	return false
}

func (Adapter) Facts(root string) map[string]string {
	facts := map[string]string{"runtime": "node"}
	if _, err := os.Stat(filepath.Join(root, "bun.lock")); err == nil {
		facts["runtime"] = "bun"
	} else if _, err := os.Stat(filepath.Join(root, "bun.lockb")); err == nil {
		facts["runtime"] = "bun"
	}
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err == nil {
		var pkg struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &pkg) == nil {
			facts["package"] = pkg.Name
		}
	}
	if _, err := os.Stat(filepath.Join(root, "tsconfig.json")); err == nil {
		facts["typescript"] = "true"
	}
	return facts
}

func (Adapter) Tools() []stack.Tool {
	return []stack.Tool{
		{Name: "bun", Check: []string{"bun", "--version"}, Install: "the version in .bun-version or package.json engines; https://bun.sh", Rings: []string{"precommit", "check"}},
		{Name: "biome", Version: BiomeVersion, Check: []string{"biome", "--version"}, Mise: "biome", Install: "mise use biome@" + BiomeVersion, Rings: []string{"precommit", "check"}},
		{Name: "oxlint", Version: OxlintVersion, Check: []string{"oxlint", "--version"}, Mise: "oxlint", Install: "mise use oxlint@" + OxlintVersion + " plus the npm package oxlint-tsgolint", Rings: []string{"check"}},
		{Name: "knip", Version: KnipVersion, Check: []string{"knip", "--version"}, Mise: "npm:knip", Install: "mise use npm:knip@" + KnipVersion, Rings: []string{"check"}},
		{Name: "depcruise", Version: DependencyCruiserVersion, Check: []string{"depcruise", "--version"}, Mise: "npm:dependency-cruiser", Install: "mise use npm:dependency-cruiser@" + DependencyCruiserVersion, Rings: []string{"check"}},
	}
}

func (Adapter) SourceGlobs() []string {
	return []string{"**/*.ts", "**/*.tsx", "**/*.mts", "**/*.cts", "**/*.js", "**/*.jsx", "**/*.mjs", "**/*.cjs"}
}

func (Adapter) TestGlobs() []string {
	return []string{"**/*.test.*", "**/*.spec.*", "**/__tests__/**", "**/test/**", "**/tests/**"}
}

func (Adapter) Pins() []stack.Pin {
	return []stack.Pin{{
		What:  "JavaScript runtime version (.bun-version, .node-version, .nvmrc or package.json engines)",
		Files: []string{".bun-version", ".node-version", ".nvmrc", ".tool-versions", "mise.toml"},
		Locks: []string{"bun.lock", "bun.lockb", "package-lock.json", "pnpm-lock.yaml", "yarn.lock"},
	}}
}

func (Adapter) Suppressions() []stack.Suppression {
	globs := []string{"**/*.ts", "**/*.tsx", "**/*.mts", "**/*.cts", "**/*.js", "**/*.jsx", "**/*.mjs", "**/*.cjs"}
	return []stack.Suppression{
		{Kind: "biome-ignore", Pattern: regexp.MustCompile(`biome-ignore\s+[\w/.-]+(?::\s*(?P<reason>.+))?`), Globs: globs},
		{Kind: "ts-ignore", Pattern: regexp.MustCompile(`@ts-ignore(?:\s*(?:--|:)?\s*(?P<reason>.+))?`), Globs: globs},
		{Kind: "ts-expect-error", Pattern: regexp.MustCompile(`@ts-expect-error(?:\s*(?:--|:)?\s*(?P<reason>.+))?`), Globs: globs},
		{Kind: "eslint-disable", Pattern: regexp.MustCompile(`eslint-disable(?:-next-line|-line)?(?:\s+[\w/@,-]+)?(?:\s*--\s*(?P<reason>.+))?`), Globs: globs},
		{Kind: "oxlint-disable", Pattern: regexp.MustCompile(`oxlint-disable(?:-next-line|-line)?(?:\s+[\w/@,-]+)?(?:\s*--\s*(?P<reason>.+))?`), Globs: globs},
		{Kind: "as-any", Pattern: regexp.MustCompile(`\bas\s+any\b(?:\s*//\s*(?P<reason>.+))?`), Globs: []string{"**/*.ts", "**/*.tsx", "**/*.mts", "**/*.cts"}},
	}
}

func (Adapter) Configs(p stack.Params) []stack.ConfigFile {
	biome := strings.ReplaceAll(biomeConfig, "{{complexity}}", strconv.Itoa(p.Complexity))
	return []stack.ConfigFile{
		{Path: "biome.jsonc", Content: biome},
		{Path: ".oxlintrc.json", Content: oxlintConfig},
		{Path: "knip.json", Content: knipConfig},
		{Path: ".dependency-cruiser.cjs", Content: depcruiseConfig},
	}
}

func (Adapter) Rules(stack.Params) map[string]stack.Enforcement {
	lint := func(via ...string) stack.Enforcement { return stack.Enforcement{Step: "lint", Via: via} }
	sg := common.RuleIDs("typescript")
	return map[string]stack.Enforcement{
		"code.format":               {Step: "fmt-check", Via: []string{"biome/format"}},
		"code.lint":                 lint("biome"),
		"code.types":                lint("tsc/strict"),
		"code.complexity":           lint("biome/noExcessiveCognitiveComplexity"),
		"code.boundaries":           {Waived: stack.Reason("typescript", "no layer boundaries declared yet; declare them in .dependency-cruiser.cjs (forbidden rules) and set step = \"lint\"")},
		"code.dead":                 lint("knip"),
		"code.dropped-results":      lint("oxlint/no-floating-promises", "oxlint/no-misused-promises"),
		"code.swallowed-errors":     lint("biome/noEmptyBlockStatements"),
		"code.debug-leftovers":      lint("biome/noConsole", "biome/noDebugger"),
		"code.naming":               lint("biome/useNamingConvention"),
		"escape.test-branch":        lint(sg["escape.test-branch"]...),
		"resilience.timeouts":       lint(sg["resilience.timeouts"]...),
		"resilience.exhaustive":     lint("biome/useExhaustiveSwitchCases"),
		"resilience.config-owner":   lint(sg["resilience.config-owner"]...),
		"resilience.import-cycles":  lint("depcruise/no-circular"),
		"resilience.bounded-growth": lint(sg["resilience.bounded-growth"]...),
		"tests.runner":              {Step: "test", Via: []string{"bun test"}},
		"tests.smells":              lint("biome/noFocusedTests", "biome/noSkippedTests"),
		"tests.diff-coverage":       {Step: "coverage", Via: []string{"diff-cover"}},
		"tests.mutation":            {Step: "mutation", Via: []string{"stryker"}},
		"tests.mock-edges":          lint(sg["tests.mock-edges"]...),
		"drift.audit":               {Step: "audit", Via: []string{"bun audit"}},
		"drift.unused-deps":         lint("knip"),
		"drift.security":            lint(append([]string{"biome/noGlobalEval"}, sg["drift.security"]...)...),
	}
}

func (Adapter) Verifiers(p stack.Params) []stack.Verifier {
	f, j := "biome.jsonc", stack.JSONC
	rule := func(id, group, name string) stack.Verifier {
		return stack.V(id, "biome/"+name, stack.NotEqual(f, j, "linter.rules."+group+"."+name, "off"), stack.NotEqual(f, j, "linter.enabled", false))
	}
	return []stack.Verifier{
		stack.V("code.format", "biome/format", stack.Exists(f), stack.NotEqual(f, j, "formatter.enabled", false)),
		stack.V("code.lint", "biome", stack.Exists(f), stack.NotEqual(f, j, "linter.enabled", false)),
		stack.V("code.types", "tsc/strict", stack.Equal("tsconfig.json", stack.JSONC, "compilerOptions.strict", true)),
		rule("code.complexity", "complexity", "noExcessiveCognitiveComplexity"),
		stack.V("code.complexity", "biome/noExcessiveCognitiveComplexity", stack.Equal(f, j, "linter.rules.complexity.noExcessiveCognitiveComplexity.options.maxAllowedComplexity", p.Complexity)),
		rule("code.swallowed-errors", "suspicious", "noEmptyBlockStatements"),
		rule("code.debug-leftovers", "suspicious", "noConsole"),
		rule("code.debug-leftovers", "suspicious", "noDebugger"),
		rule("code.naming", "style", "useNamingConvention"),
		rule("resilience.exhaustive", "nursery", "useExhaustiveSwitchCases"),
		rule("tests.smells", "suspicious", "noFocusedTests"),
		rule("tests.smells", "suspicious", "noSkippedTests"),
		rule("drift.security", "security", "noGlobalEval"),
		stack.V("code.dropped-results", "oxlint/no-floating-promises", stack.Equal(".oxlintrc.json", stack.JSONC, "rules.typescript/no-floating-promises", "error"), stack.Equal(".oxlintrc.json", stack.JSONC, "options.typeAware", true)),
		stack.V("code.dropped-results", "oxlint/no-misused-promises", stack.Equal(".oxlintrc.json", stack.JSONC, "rules.typescript/no-misused-promises", "error")),
		stack.V("code.dead", "knip", stack.Exists("knip.json")),
		stack.V("resilience.import-cycles", "depcruise/no-circular", stack.Exists(".dependency-cruiser.cjs"), stack.Matches(".dependency-cruiser.cjs", regexp.MustCompile(`name:\s*['"]no-circular['"]`), "the no-circular rule")),
	}
}

func (Adapter) Make(p stack.Params) stack.Make {
	runtime := p.Facts["runtime"]
	test := "bun test"
	audit := "bun audit"
	if runtime == "node" {
		test = "node --test"
		audit = "npm audit --audit-level=high"
	}
	m := stack.Make{
		Vars: []string{
			"STAGED_TS := $(filter %.ts %.tsx %.mts %.cts %.js %.jsx %.mjs %.cjs %.json %.jsonc,$(STAGED))",
		},
		Fmt:      []string{"biome format --write ."},
		FmtCheck: []string{"biome format ."},
		Lint: []string{
			"biome ci .",
			"knip",
			"depcruise --config .dependency-cruiser.cjs src",
		},
		Test: []string{test},
		Precommit: []string{
			`@[ -z "$(STAGED_TS)" ] || biome check $(STAGED_TS)`,
		},
		Audit:    []string{audit},
		Mutation: []string{"stryker run"},
	}
	if p.Facts["typescript"] == "true" {
		m.Lint = append([]string{"tsc --noEmit", "oxlint --type-aware ."}, m.Lint...)
	}
	if runtime == "bun" {
		m.Coverage = []string{
			"@mkdir -p $(REPORTS)",
			"bun test --coverage --coverage-reporter=lcov --coverage-dir=$(REPORTS)/bun",
			"diff-cover $(REPORTS)/bun/lcov.info --compare-branch=$(PAWL_BASE) --fail-under=$(DIFF_COVERAGE)",
		}
	} else {
		m.Coverage = []string{
			"@mkdir -p $(REPORTS)",
			"node --test --experimental-test-coverage --test-coverage-reporter=lcov --test-coverage-reporter-destination=$(REPORTS)/node.lcov",
			"diff-cover $(REPORTS)/node.lcov --compare-branch=$(PAWL_BASE) --fail-under=$(DIFF_COVERAGE)",
		}
	}
	return m
}

const biomeConfig = `{
  // Biome configuration written by pawl. Project-owned: change options and rules here; pawl check verifies that the
  // rules the catalog needs stay on (or are waived in pawl.toml with a reason).
  "$schema": "https://biomejs.dev/schemas/2.6.8/schema.json",
  "vcs": { "enabled": true, "clientKind": "git", "useIgnoreFile": true },
  "files": { "ignoreUnknown": true },
  "formatter": { "enabled": true, "indentStyle": "space", "indentWidth": 2, "lineWidth": 100 },
  "javascript": { "formatter": { "quoteStyle": "single", "semicolons": "always", "trailingCommas": "all" } },
  "assist": { "enabled": true, "actions": { "source": { "organizeImports": "on" } } },
  "linter": {
    "enabled": true,
    "rules": {
      "preset": "recommended",
      "complexity": {
        "noExcessiveCognitiveComplexity": { "level": "error", "options": { "maxAllowedComplexity": {{complexity}} } }
      },
      "correctness": { "noUnusedImports": "error", "noUnusedVariables": "error" },
      "style": {
        "noDefaultExport": "error",
        "useNamingConvention": { "level": "error", "options": { "strictCase": false } }
      },
      "suspicious": {
        "noConsole": "error",
        "noDebugger": "error",
        "noEmptyBlockStatements": "error",
        "noExplicitAny": "error",
        "noFocusedTests": "error",
        "noSkippedTests": "error"
      },
      "security": { "noGlobalEval": "error" },
      "nursery": { "useExhaustiveSwitchCases": "error" }
    }
  },
  "overrides": [
    {
      "includes": ["**/*.test.*", "**/*.spec.*", "**/__tests__/**", "**/test/**", "**/tests/**"],
      "linter": { "rules": { "suspicious": { "noConsole": "off" } } }
    },
    {
      "includes": ["**/*.config.*", "**/scripts/**", "**/bin/**"],
      "linter": { "rules": { "suspicious": { "noConsole": "off" }, "style": { "noDefaultExport": "off" } } }
    }
  ]
}
`

const oxlintConfig = `{
  "$schema": "./node_modules/oxlint/configuration_schema.json",
  // oxlint adds the type-aware promise rules Biome's inference does not cover yet. Needs the npm package
  // oxlint-tsgolint. Biome owns everything else.
  "options": { "typeAware": true },
  "categories": { "correctness": "off" },
  "rules": {
    "typescript/no-floating-promises": "error",
    "typescript/no-misused-promises": "error"
  }
}
`

const knipConfig = `{
  "$schema": "https://unpkg.com/knip@6/schema.json",
  "entry": ["src/index.ts", "src/main.ts", "index.ts", "bin/*.ts"],
  "project": ["src/**/*.ts", "**/*.ts"],
  "ignoreDependencies": ["oxlint-tsgolint"]
}
`

const depcruiseConfig = `/** dependency-cruiser configuration written by pawl. Declare layer boundaries as forbidden rules here. */
module.exports = {
  forbidden: [
    { name: 'no-circular', severity: 'error', from: {}, to: { circular: true } },
    { name: 'no-orphans', severity: 'warn', from: { orphan: true, pathNot: ['\\.d\\.ts$', '(^|/)index\\.ts$'] }, to: {} },
  ],
  options: {
    doNotFollow: { path: 'node_modules' },
    tsPreCompilationDeps: true,
    tsConfig: { fileName: 'tsconfig.json' },
    enhancedResolveOptions: { exportsFields: ['exports'], conditionNames: ['import', 'require', 'node', 'default'] },
  },
};
`
