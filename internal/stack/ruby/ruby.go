// Package ruby is the Ruby adapter: RuboCop with performance and minitest/rspec plugins, SimpleCov for coverage,
// bundler-audit, mutant. Tools are gems in the project's Gemfile, the stack's native pin.
package ruby

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rojosinalma/pawl/internal/report"
	"github.com/rojosinalma/pawl/internal/stack"
	"github.com/rojosinalma/pawl/internal/stack/common"
)

// Pinned gem versions pawl writes into a Gemfile that lacks them.
const (
	RubocopVersion            = "1.92.2"
	RubocopPerformanceVersion = "1.27.0"
	RubocopMinitestVersion    = "0.41.0"
	RubocopRspecVersion       = "3.10.2"
	BundlerAuditVersion       = "0.9.3"
	SimplecovVersion          = "1.3.2"
	SimplecovLcovVersion      = "0.9.0"
)

// Adapter is the stack.Adapter of this stack.
type Adapter struct{}

func init() { stack.Register(Adapter{}) }

// Name implements stack.Adapter.
func (Adapter) Name() string { return "ruby" }

// Title implements stack.Adapter.
func (Adapter) Title() string { return "Ruby" }

// Detect implements stack.Adapter.
func (Adapter) Detect(files []string) bool {
	for _, f := range files {
		if f == "Gemfile" || f == "Rakefile" || strings.HasSuffix(f, ".rb") || strings.HasSuffix(f, ".gemspec") {
			return true
		}
	}
	return false
}

// Facts implements stack.Adapter.
func (Adapter) Facts(root string) map[string]string {
	facts := map[string]string{"test_framework": "minitest"}
	if _, err := os.Stat(filepath.Join(root, "spec")); err == nil {
		facts["test_framework"] = "rspec"
	}
	if raw, err := os.ReadFile(filepath.Join(root, "Gemfile")); err == nil {
		g := string(raw)
		facts["has_rubocop"] = strconv.FormatBool(strings.Contains(g, `"rubocop"`) || strings.Contains(g, `'rubocop'`))
		facts["has_rails"] = strconv.FormatBool(strings.Contains(g, `"rails"`) || strings.Contains(g, `'rails'`))
	}
	return facts
}

// Tools implements stack.Adapter.
func (Adapter) Tools() []stack.Tool {
	return []stack.Tool{
		{Name: "ruby", Check: []string{"ruby", "--version"}, Install: "the version in .ruby-version; mise use ruby", Rings: []string{"precommit", "check"}},
		{Name: "bundle", Check: []string{"bundle", "--version"}, Install: "gem install bundler", Rings: []string{"precommit", "check"}},
		{Name: "rubocop", Version: RubocopVersion, Check: []string{"bundle", "exec", "rubocop", "--version"}, Install: "bundle install (the Gemfile pins it)", Rings: []string{"precommit", "check"}},
		{Name: "bundler-audit", Version: BundlerAuditVersion, Check: []string{"bundle", "exec", "bundler-audit", "version"}, Install: "bundle install (the Gemfile pins it)", Rings: []string{"nightly"}},
	}
}

// SourceGlobs implements stack.Adapter.
func (Adapter) SourceGlobs() []string {
	return []string{"**/*.rb", "**/*.rake", "Gemfile", "Rakefile", "**/*.gemspec"}
}

// TestGlobs implements stack.Adapter.
func (Adapter) TestGlobs() []string {
	return []string{"**/*_test.rb", "**/*_spec.rb", "test/**", "spec/**"}
}

// Pins implements stack.Adapter.
func (Adapter) Pins() []stack.Pin {
	return []stack.Pin{{What: "Ruby version (.ruby-version or the Gemfile ruby directive)", Files: []string{".ruby-version", ".tool-versions", "mise.toml"}, Locks: []string{"Gemfile.lock"}}}
}

// Suppressions implements stack.Adapter.
func (Adapter) Suppressions() []stack.Suppression {
	globs := []string{"**/*.rb", "**/*.rake", "Gemfile", "Rakefile"}
	return []stack.Suppression{
		{Kind: "rubocop:disable", Pattern: regexp.MustCompile(`rubocop:(?:disable|todo)\s+[\w/]+(?:\s*,\s*[\w/]+)*(?:\s+--\s+(?P<reason>.+))?`), Globs: globs},
		{Kind: "steep:ignore", Pattern: regexp.MustCompile(`steep:ignore(?:\s+(?P<reason>.+))?`), Globs: globs},
	}
}

// Configs implements stack.Adapter.
func (Adapter) Configs(p stack.Params) []stack.ConfigFile {
	framework := p.Facts["test_framework"]
	plugin, testCops := "rubocop-minitest", minitestCops
	if framework == "rspec" {
		plugin, testCops = "rubocop-rspec", rspecCops
	}
	cfg := strings.NewReplacer("{{complexity}}", strconv.Itoa(p.Complexity), "{{plugin}}", plugin, "{{testcops}}", testCops).Replace(rubocopConfig)
	out := []stack.ConfigFile{{Path: ".rubocop.yml", Content: cfg}}
	if framework == "rspec" {
		out = append(out, stack.ConfigFile{Path: ".rspec", Content: "--require spec_helper\n--order random\n--color\n"})
	}
	return out
}

// GemfileAdditions returns the development and test group pawl appends to a Gemfile that lacks RuboCop. The gems
// are in alphabetical order, which RuboCop's Bundler/OrderedGems asks for.
func GemfileAdditions(framework string) string {
	gems := map[string]string{
		"bundler-audit":       BundlerAuditVersion,
		"rubocop":             RubocopVersion,
		"rubocop-performance": RubocopPerformanceVersion,
		"simplecov":           SimplecovVersion,
		"simplecov-lcov":      SimplecovLcovVersion,
	}
	if framework == "rspec" {
		gems["rubocop-rspec"] = RubocopRspecVersion
	} else {
		gems["rubocop-minitest"] = RubocopMinitestVersion
	}
	names := make([]string, 0, len(gems))
	for name := range gems {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("\n# pawl: the quality tools of this project, pinned here (the stack's native pin)\n")
	b.WriteString("group :development, :test do\n")
	for _, name := range names {
		b.WriteString("  gem '" + name + "', '" + gems[name] + "', require: false\n")
	}
	b.WriteString("end\n")
	return b.String()
}

// Rules implements stack.Adapter.
func (Adapter) Rules(p stack.Params) map[string]stack.Enforcement {
	lint := func(via ...string) stack.Enforcement { return stack.Enforcement{Step: "lint", Via: via} }
	sg := common.RuleIDs("ruby")
	smells := []string{"rubocop/Minitest/SkipWithoutReason", "rubocop/Minitest/NoAssertions"}
	if p.Facts["test_framework"] == "rspec" {
		smells = []string{"rubocop/RSpec/Focus", "rubocop/RSpec/Pending", "rubocop/RSpec/NoExpectationExample"}
	}
	return map[string]stack.Enforcement{
		"code.format":               {Step: "fmt-check", Via: []string{"rubocop/Layout"}},
		"code.lint":                 lint("rubocop"),
		"code.types":                {Waived: stack.Reason("ruby", "no type checker in use; Steep or Sorbet is an opt-in: add it to the Gemfile and set step = \"lint\"")},
		"code.complexity":           lint("rubocop/Metrics/CyclomaticComplexity", "rubocop/Metrics/PerceivedComplexity"),
		"code.boundaries":           {Waived: stack.Reason("ruby", "no layer boundaries declared yet; packwerk is the tool once packages are declared")},
		"code.dead":                 lint("rubocop/Lint/UselessAssignment", "rubocop/Lint/UnusedMethodArgument"),
		"code.dropped-results":      {Waived: stack.Reason("ruby", "exceptions propagate by default and there is no result type; code.swallowed-errors covers the handlers")},
		"code.swallowed-errors":     lint("rubocop/Lint/SuppressedException", "rubocop/Style/RescueStandardError"),
		"code.debug-leftovers":      lint("rubocop/Lint/Debugger"),
		"code.naming":               lint("rubocop/Naming"),
		"escape.test-branch":        lint(sg["escape.test-branch"]...),
		"resilience.timeouts":       lint(sg["resilience.timeouts"]...),
		"resilience.exhaustive":     {Waived: stack.Reason("ruby", "case over a closed set has no static check; end the case with `else raise`")},
		"resilience.config-owner":   lint(sg["resilience.config-owner"]...),
		"resilience.import-cycles":  {Waived: stack.Reason("ruby", "requires are resolved at run time, not a static graph")},
		"resilience.bounded-growth": lint(sg["resilience.bounded-growth"]...),
		"tests.runner":              {Step: "test", Via: []string{p.Facts["test_framework"] + " random order"}},
		"tests.smells":              lint(smells...),
		"tests.diff-coverage":       {Step: "coverage", Via: []string{"simplecov", "diff-cover"}},
		"tests.mutation":            {Step: "mutation", Via: []string{"mutant"}},
		"tests.mock-edges":          lint(sg["tests.mock-edges"]...),
		"drift.audit":               {Step: "audit", Via: []string{"bundler-audit"}},
		"drift.unused-deps":         {Waived: stack.Reason("ruby", "no standard tool reports unused gems")},
		"drift.security":            lint(sg["drift.security"]...),
	}
}

// Verifiers implements stack.Adapter.
func (Adapter) Verifiers(p stack.Params) []stack.Verifier {
	f, y := rubocopFile, stack.YAML
	cop := func(rule, name string) stack.Verifier { return stack.V(rule, "rubocop/"+name, copOn(name)) }
	v := []stack.Verifier{
		stack.V("code.lint", "rubocop", stack.Exists(f), stack.NotEqual(f, y, "AllCops.DisabledByDefault", true)),
		cop("code.format", "Layout"),
		cop("code.complexity", "Metrics/CyclomaticComplexity"),
		stack.V("code.complexity", "rubocop/Metrics/CyclomaticComplexity", copSetting("Metrics/CyclomaticComplexity", "Max", p.Complexity)),
		cop("code.complexity", "Metrics/PerceivedComplexity"),
		cop("code.dead", "Lint/UselessAssignment"),
		cop("code.dead", "Lint/UnusedMethodArgument"),
		cop("code.swallowed-errors", "Lint/SuppressedException"),
		cop("code.swallowed-errors", "Style/RescueStandardError"),
		cop("code.debug-leftovers", "Lint/Debugger"),
		cop("code.naming", "Naming"),
	}
	if p.Facts["test_framework"] == "rspec" {
		v = append(v, stack.V("tests.smells", "rubocop/RSpec/Focus", stack.Has(f, y, "plugins", "rubocop-rspec"), copOn("RSpec/Focus")))
	} else {
		v = append(v, stack.V("tests.smells", "rubocop/Minitest/SkipWithoutReason", stack.Has(f, y, "plugins", "rubocop-minitest"), copOn("Minitest/SkipWithoutReason")))
	}
	return v
}

const rubocopFile = ".rubocop.yml"

// loadRubocop parses .rubocop.yml; a missing or broken file is a finding.
func loadRubocop(v stack.Verify) (*stack.Config, []report.Finding) {
	cfg, err := stack.LoadConfig(v.Root, rubocopFile, stack.YAML)
	if err != nil {
		return nil, []report.Finding{report.Errorf(v.RuleID, rubocopFile, 0, "cannot parse: %v", err)}
	}
	if cfg == nil {
		return nil, []report.Finding{report.Errorf(v.RuleID, rubocopFile, 0, "missing; %s needs it for %s", v.Via, v.RuleID).WithFix("pawl init writes a default, or waive the rule in pawl.toml")}
	}
	return cfg, nil
}

// copOn reports when the cop ("Lint/Debugger") or its department ("Lint", RuboCop's department-level
// "Enabled: false") is turned off. A department name alone checks the department.
func copOn(name string) func(stack.Verify) []report.Finding {
	return func(v stack.Verify) []report.Finding {
		cfg, bad := loadRubocop(v)
		if bad != nil {
			return bad
		}
		keys := []string{name}
		if dept, _, ok := strings.Cut(name, "/"); ok {
			keys = append(keys, dept)
		}
		for _, k := range keys {
			if val, ok := cfg.GetKey(k, "Enabled"); ok && fmt.Sprint(val) == "false" {
				return []report.Finding{report.Errorf(v.RuleID, rubocopFile, 0, "%s: Enabled: false turns %s off", k, v.Via).WithFix("re-enable it, or waive the rule in pawl.toml with a reason")}
			}
		}
		return nil
	}
}

// copSetting reports when the cop's setting is missing or differs from want.
func copSetting(name, key string, want any) func(stack.Verify) []report.Finding {
	return func(v stack.Verify) []report.Finding {
		cfg, bad := loadRubocop(v)
		if bad != nil {
			return bad
		}
		if val, ok := cfg.GetKey(name, key); !ok || fmt.Sprint(val) != fmt.Sprint(want) {
			return []report.Finding{report.Errorf(v.RuleID, rubocopFile, 0, "%s: %s must be %v for %s (found %v)", name, key, want, v.Via, val).WithFix("set it, or waive the rule in pawl.toml with a reason")}
		}
		return nil
	}
}

// Make implements stack.Adapter.
func (Adapter) Make(p stack.Params) stack.Make {
	test := `bundle exec ruby -Ilib:test -e 'ARGV.each { |f| require File.expand_path(f) }' $(RUBY_TEST_SCOPE)`
	scope := "RUBY_TEST_SCOPE ?= $(shell $(PAWL) affected ruby 2>/dev/null || find test -name '*_test.rb')"
	mutation := "bundle exec mutant run --integration minitest"
	helper := "test/test_helper.rb"
	if p.Facts["test_framework"] == "rspec" {
		test = "bundle exec rspec --order random $(RUBY_TEST_SCOPE)"
		// without pawl (or when it fails) the scope is empty, and rspec without arguments runs every spec
		scope = "RUBY_TEST_SCOPE ?= $(shell $(PAWL) affected ruby 2>/dev/null || true)"
		mutation = "bundle exec mutant run --integration rspec"
		helper = "spec/spec_helper.rb"
	}
	return stack.Make{
		Vars: []string{
			scope,
			"STAGED_RB := $(filter %.rb %.rake Gemfile Rakefile %.gemspec,$(STAGED))",
		},
		Fmt:      []string{"bundle exec rubocop --autocorrect"},
		FmtCheck: []string{"bundle exec rubocop --only Layout"},
		Lint:     []string{"bundle exec rubocop"},
		Test:     []string{test},
		Precommit: []string{
			`@[ -z "$(STAGED_RB)" ] || bundle exec rubocop $(STAGED_RB)`,
		},
		Coverage: []string{
			"@mkdir -p $(REPORTS)",
			coverageGuard(helper),
			"COVERAGE=1 " + test,
			"diff-cover " + LcovReport + " --compare-branch=$(PAWL_BASE) --fail-under=$(DIFF_COVERAGE)",
		},
		Audit:    []string{"bundle exec bundler-audit check --update"},
		Mutation: []string{mutation},
	}
}

// LcovReport is where the SimpleCov setup of CoverageHelper writes the lcov report diff-cover reads.
const LcovReport = ".pawl/reports/ruby-lcov.info"

// CoverageHelper is the SimpleCov setup the coverage target needs at the top of the test helper, before the code
// under test loads; the generated recipe prints it when the helper lacks SimpleCov.
func CoverageHelper(testDir string) []string {
	return []string{
		"if ENV['COVERAGE']",
		"  require 'simplecov'",
		"  require 'simplecov-lcov'",
		"  SimpleCov::Formatter::LcovFormatter.config.report_with_single_file = true",
		"  SimpleCov::Formatter::LcovFormatter.config.single_report_path = '" + LcovReport + "'",
		"  SimpleCov.formatter = SimpleCov::Formatter::LcovFormatter",
		"  SimpleCov.start { add_filter '/" + testDir + "/' }",
		"end",
	}
}

// coverageGuard is a recipe line that fails with the setup to paste when the test helper does not start SimpleCov.
func coverageGuard(helper string) string {
	lines := CoverageHelper(filepath.Dir(helper))
	quoted := make([]string, len(lines))
	for i, l := range lines {
		quoted[i] = `"` + l + `"`
	}
	return "@grep -qs SimpleCov " + helper + ` || { printf '%s\n' "make coverage: add this to the top of ` + helper + `:" ` + strings.Join(quoted, " ") + "; exit 1; }"
}

const rubocopConfig = `# RuboCop configuration written by pawl. RuboCop's defaults implement the community Ruby Style Guide; this file
# adds the plugins and the thresholds the catalog needs. Project-owned: override cops here; pawl verify checks that
# the cops the catalog needs stay on (or are waived in pawl.toml with a reason).
plugins:
  - rubocop-performance
  - {{plugin}}

AllCops:
  NewCops: enable
  SuggestExtensions: false
  Exclude:
    - "vendor/**/*"
    - "node_modules/**/*"
    - "tmp/**/*"
    - "db/schema.rb"
    - ".pawl/**/*"

Metrics/CyclomaticComplexity:
  Max: {{complexity}}
Metrics/PerceivedComplexity:
  Max: {{complexity}}
Metrics/MethodLength:
  Max: 30
Metrics/ClassLength:
  Max: 300
Metrics/BlockLength:
  Exclude:
    - "test/**/*"
    - "spec/**/*"
    - "*.gemspec"
    - "Rakefile"

Lint/SuppressedException:
  AllowComments: true
  AllowNil: false
Style/RescueStandardError:
  EnforcedStyle: explicit
Style/Documentation:
  Enabled: false
Style/StringLiterals:
  EnforcedStyle: single_quotes
Layout/LineLength:
  Max: 120

{{testcops}}
`

const minitestCops = `Minitest/SkipWithoutReason:
  Enabled: true
Minitest/NoAssertions:
  Enabled: true
`

const rspecCops = `RSpec/Focus:
  Enabled: true
RSpec/Pending:
  Enabled: true
RSpec/NoExpectationExample:
  Enabled: true
`
