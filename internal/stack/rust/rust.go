// Package rust is the Rust adapter: rustfmt, clippy with denied warnings, the compiler for types, cycles and
// exhaustive matches, cargo test, cargo-llvm-cov with diff-cover, cargo-audit, cargo-deny, cargo-machete,
// cargo-mutants, cargo-geiger.
package rust

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rojosinalma/pawl/internal/makefile"
	"github.com/rojosinalma/pawl/internal/report"
	"github.com/rojosinalma/pawl/internal/stack"
	"github.com/rojosinalma/pawl/internal/stack/common"
)

// Pinned versions of the cargo tools without a native pin file (Renovate moves them in mise.toml).
const (
	CargoAuditVersion   = "0.23.0"
	CargoDenyVersion    = "0.20.2"
	CargoMacheteVersion = "0.10.0"
	CargoMutantsVersion = "27.1.0"
	CargoLlvmCovVersion = "0.9.1"
)

type Adapter struct{}

func init() { stack.Register(Adapter{}) }

func (Adapter) Name() string  { return "rust" }
func (Adapter) Title() string { return "Rust" }

func (Adapter) Detect(files []string) bool {
	for _, f := range files {
		if f == "Cargo.toml" || strings.HasSuffix(f, ".rs") {
			return true
		}
	}
	return false
}

func (Adapter) Facts(root string) map[string]string {
	facts := map[string]string{}
	if raw, err := os.ReadFile(filepath.Join(root, "Cargo.toml")); err == nil {
		s := string(raw)
		facts["workspace"] = strconv.FormatBool(strings.Contains(s, "[workspace]"))
		if m := regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`).FindStringSubmatch(s); m != nil {
			facts["crate"] = strings.ReplaceAll(m[1], "-", "_")
		}
	}
	return facts
}

func (Adapter) Tools() []stack.Tool {
	return []stack.Tool{
		{Name: "cargo", Check: []string{"cargo", "--version"}, Install: "the toolchain in rust-toolchain.toml; rustup", Rings: []string{"precommit", "check"}},
		{Name: "cargo-clippy", Check: []string{"cargo", "clippy", "--version"}, Install: "rustup component add clippy", Rings: []string{"precommit", "check"}},
		{Name: "rustfmt", Check: []string{"rustfmt", "--version"}, Install: "rustup component add rustfmt", Rings: []string{"precommit", "check"}},
		{Name: "cargo-audit", Version: CargoAuditVersion, Check: []string{"cargo", "audit", "--version"}, Mise: "aqua:rustsec/rustsec/cargo-audit", Install: "mise use aqua:rustsec/rustsec/cargo-audit@" + CargoAuditVersion, Rings: []string{"nightly"}},
		{Name: "cargo-deny", Version: CargoDenyVersion, Check: []string{"cargo", "deny", "--version"}, Mise: "cargo-deny", Install: "mise use cargo-deny@" + CargoDenyVersion, Rings: []string{"check"}},
		{Name: "cargo-machete", Version: CargoMacheteVersion, Check: []string{"cargo", "machete", "--version"}, Mise: "github:bnjbvr/cargo-machete", Install: "mise use github:bnjbvr/cargo-machete@" + CargoMacheteVersion, Rings: []string{"check"}},
		{Name: "cargo-llvm-cov", Version: CargoLlvmCovVersion, Check: []string{"cargo", "llvm-cov", "--version"}, Mise: "aqua:taiki-e/cargo-llvm-cov", Install: "mise use aqua:taiki-e/cargo-llvm-cov@" + CargoLlvmCovVersion, Rings: []string{"check"}},
		{Name: "cargo-mutants", Version: CargoMutantsVersion, Check: []string{"cargo", "mutants", "--version"}, Mise: "github:sourcefrog/cargo-mutants", Install: "mise use github:sourcefrog/cargo-mutants@" + CargoMutantsVersion, Rings: []string{"nightly"}},
	}
}

func (Adapter) SourceGlobs() []string { return []string{"**/*.rs"} }
func (Adapter) TestGlobs() []string   { return []string{"tests/**", "**/tests/**", "benches/**"} }

func (Adapter) Pins() []stack.Pin {
	return []stack.Pin{{What: "Rust toolchain (rust-toolchain.toml)", Files: []string{"rust-toolchain.toml", "rust-toolchain"}, Locks: []string{"Cargo.lock"}}}
}

func (Adapter) Suppressions() []stack.Suppression {
	globs := []string{"**/*.rs"}
	return []stack.Suppression{
		{Kind: "allow", Pattern: regexp.MustCompile(`#!?\[allow\((?:clippy::)?[\w:, ]+\)\](?:\s*//\s*(?P<reason>.+))?`), Globs: globs},
		{Kind: "expect", Pattern: regexp.MustCompile(`#!?\[expect\((?:clippy::)?[\w:, ]+(?:,\s*reason\s*=\s*"(?P<reason>[^"]+)")?\)\]`), Globs: globs},
		{Kind: "unsafe", Pattern: regexp.MustCompile(`\bunsafe\s*\{(?:\s*//\s*(?P<reason>.+))?`), Globs: globs},
	}
}

func (Adapter) Configs(p stack.Params) []stack.ConfigFile {
	return []stack.ConfigFile{
		{Path: "rustfmt.toml", Content: rustfmtConfig},
		{Path: "clippy.toml", Content: strings.ReplaceAll(clippyConfig, "{{complexity}}", strconv.Itoa(p.Complexity))},
		{Path: "deny.toml", Content: denyConfig},
	}
}

func (Adapter) Rules(stack.Params) map[string]stack.Enforcement {
	lint := func(via ...string) stack.Enforcement { return stack.Enforcement{Step: "lint", Via: via} }
	sg := common.RuleIDs("rust")
	return map[string]stack.Enforcement{
		"code.format":               {Step: "fmt-check", Via: []string{"rustfmt"}},
		"code.lint":                 lint("clippy"),
		"code.types":                lint("rustc"),
		"code.complexity":           lint("clippy/cognitive_complexity"),
		"code.boundaries":           {Waived: stack.Reason("rust", "crate layout is the boundary; declare workspace crates per layer and the compiler enforces visibility")},
		"code.dead":                 lint("rustc/dead_code"),
		"code.dropped-results":      lint("rustc/unused_must_use", "clippy/let_underscore_must_use"),
		"code.swallowed-errors":     lint("clippy/let_underscore_must_use", "clippy/map_err_ignore"),
		"code.debug-leftovers":      lint("clippy/print_stdout", "clippy/dbg_macro", "clippy/todo"),
		"code.naming":               lint("rustc/nonstandard_style"),
		"escape.test-branch":        {Waived: stack.Reason("rust", "cfg(test) is the idiomatic, compile-time test switch; no run-time branch exists")},
		"resilience.timeouts":       lint(sg["resilience.timeouts"]...),
		"resilience.exhaustive":     lint("rustc/match"),
		"resilience.config-owner":   lint(sg["resilience.config-owner"]...),
		"resilience.import-cycles":  lint("rustc"),
		"resilience.bounded-growth": lint(sg["resilience.bounded-growth"]...),
		"tests.runner":              {Step: "test", Via: []string{"cargo test"}},
		"tests.smells":              lint(sg["tests.smells"]...),
		"tests.diff-coverage":       {Step: "coverage", Via: []string{"cargo-llvm-cov", "diff-cover"}},
		"tests.mutation":            {Step: "mutation", Via: []string{"cargo-mutants"}},
		"tests.mock-edges":          {Waived: stack.Reason("rust", "tests substitute traits, which are the declared edges; nothing to detect")},
		"drift.audit":               {Step: "audit", Via: []string{"cargo-audit", "cargo-deny"}},
		"drift.unused-deps":         lint("cargo-machete"),
		"drift.security":            lint("cargo-deny/advisories", "clippy/unwrap_used"),
	}
}

func (Adapter) Verifiers(p stack.Params) []stack.Verifier {
	denied := func(rule, lint string) stack.Verifier {
		return stack.V(rule, "clippy/"+lint, stack.Matches("Makefile", regexp.MustCompile(`(?m)^CLIPPY_LINTS\b.*-D\s*clippy::`+lint+`\b`), "clippy::"+lint+" denied in CLIPPY_LINTS"))
	}
	return []stack.Verifier{
		stack.V("code.format", "rustfmt", stack.Exists("rustfmt.toml")),
		stack.V("code.lint", "clippy", stack.Matches("Makefile", regexp.MustCompile(`(?m)^CLIPPY_LINTS\b.*-D\s*warnings\b`), "-D warnings in CLIPPY_LINTS"),
			lintRunsClippy),
		stack.V("code.complexity", "clippy/cognitive_complexity", stack.Equal("clippy.toml", stack.TOML, "cognitive-complexity-threshold", p.Complexity)),
		denied("code.complexity", "cognitive_complexity"),
		denied("code.debug-leftovers", "print_stdout"),
		denied("code.debug-leftovers", "dbg_macro"),
		denied("code.swallowed-errors", "let_underscore_must_use"),
		denied("code.swallowed-errors", "map_err_ignore"),
		denied("drift.security", "unwrap_used"),
		stack.V("drift.audit", "cargo-deny", stack.Exists("deny.toml")),
	}
}

// lintRunsClippy reports when the lint target and its steps have no cargo clippy line that passes $(CLIPPY_LINTS).
func lintRunsClippy(v stack.Verify) []report.Finding {
	m, ok, err := makefile.Read(v.Root)
	if err != nil || !ok {
		return []report.Finding{report.Errorf(v.RuleID, "Makefile", 0, "missing; %s needs it for %s", v.Via, v.RuleID).WithFix("pawl init writes one")}
	}
	for _, line := range m.Recipes("lint") {
		if strings.Contains(line, "cargo clippy") && strings.Contains(line, "$(CLIPPY_LINTS)") {
			return nil
		}
	}
	return []report.Finding{report.Errorf(v.RuleID, m.Path, m.Targets["lint"].Line, "the lint target does not run cargo clippy with $(CLIPPY_LINTS)").WithFix("add `cargo clippy --all-targets -- $(CLIPPY_LINTS)` to lint, or waive the rule in pawl.toml with a reason")}
}

// clippyLints are the lints the catalog needs on top of clippy's defaults, passed on the command line so that they
// apply to this workspace only (rustflags would reach every dependency and every plain cargo build).
var clippyLints = []string{
	"-D warnings",
	"-D clippy::cognitive_complexity",
	"-D clippy::print_stdout",
	"-D clippy::print_stderr",
	"-D clippy::dbg_macro",
	"-D clippy::todo",
	"-D clippy::unimplemented",
	"-D clippy::let_underscore_must_use",
	"-D clippy::map_err_ignore",
	"-D clippy::unwrap_used",
	"-D clippy::expect_used",
}

func (Adapter) Make(stack.Params) stack.Make {
	return stack.Make{
		Vars: []string{
			"CLIPPY_LINTS ?= " + strings.Join(clippyLints, " "),
			"CARGO_TEST_SCOPE ?= $(shell $(PAWL) affected rust 2>/dev/null)",
			"STAGED_RS := $(filter %.rs,$(STAGED))",
		},
		Fmt:      []string{"cargo fmt --all"},
		FmtCheck: []string{"cargo fmt --all -- --check"},
		Lint: []string{
			"cargo clippy --locked --all-targets --all-features -- $(CLIPPY_LINTS)",
			"cargo machete",
			"cargo deny --locked check bans licenses sources",
		},
		Test: []string{"cargo test --locked --all-features $(CARGO_TEST_SCOPE)"},
		Precommit: []string{
			`@[ -z "$(STAGED_RS)" ] || cargo fmt --all -- --check`,
			`@[ -z "$(STAGED_RS)" ] || cargo clippy --locked --all-targets -- $(CLIPPY_LINTS)`,
		},
		Coverage: []string{
			"@mkdir -p $(REPORTS)",
			"cargo llvm-cov --locked --all-features --lcov --output-path $(REPORTS)/rust.lcov",
			"diff-cover $(REPORTS)/rust.lcov --compare-branch=$(PAWL_BASE) --fail-under=$(DIFF_COVERAGE)",
		},
		Audit: []string{"cargo audit", "cargo deny --locked check advisories"},
		Mutation: []string{
			"@mkdir -p $(REPORTS)",
			"git diff $(PAWL_BASE) > $(REPORTS)/diff.patch && cargo mutants --in-diff $(REPORTS)/diff.patch",
		},
	}
}

const rustfmtConfig = `# rustfmt configuration written by pawl. Project-owned. cargo fmt passes the edition from Cargo.toml.
max_width = 100
`

const clippyConfig = `# clippy configuration written by pawl. Thresholds for the catalog; the denied lints are CLIPPY_LINTS in the
# Makefile. Tests may unwrap, expect and print.
cognitive-complexity-threshold = {{complexity}}
too-many-arguments-threshold = 6
allow-unwrap-in-tests = true
allow-expect-in-tests = true
allow-print-in-tests = true
allow-dbg-in-tests = true
`

const denyConfig = `# cargo-deny configuration written by pawl: advisories, licences, duplicate bans, sources.
[advisories]
version = 2
yanked = "deny"

[licenses]
version = 2
unused-allowed-license = "allow"
allow = ["MIT", "Apache-2.0", "Apache-2.0 WITH LLVM-exception", "BSD-2-Clause", "BSD-3-Clause", "ISC", "Unicode-3.0", "Zlib", "MPL-2.0"]

[bans]
multiple-versions = "warn"
wildcards = "deny"

[sources]
unknown-registry = "deny"
unknown-git = "deny"
`
