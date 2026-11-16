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
		{Path: ".cargo/config.toml", Content: cargoConfig},
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
	cfg := ".cargo/config.toml"
	return []stack.Verifier{
		stack.V("code.format", "rustfmt", stack.Exists("rustfmt.toml")),
		stack.V("code.lint", "clippy", stack.Exists(cfg), stack.Matches(cfg, regexp.MustCompile(`-Dwarnings|-D\s*warnings`), "rustflags with -D warnings")),
		stack.V("code.complexity", "clippy/cognitive_complexity", stack.Equal("clippy.toml", stack.TOML, "cognitive-complexity-threshold", p.Complexity)),
		stack.V("code.debug-leftovers", "clippy/print_stdout", stack.Matches(cfg, regexp.MustCompile(`clippy::print_stdout`), "clippy::print_stdout denied")),
		stack.V("code.swallowed-errors", "clippy/let_underscore_must_use", stack.Matches(cfg, regexp.MustCompile(`clippy::let_underscore_must_use`), "clippy::let_underscore_must_use denied")),
		stack.V("drift.audit", "cargo-deny", stack.Exists("deny.toml")),
	}
}

func (Adapter) Make(stack.Params) stack.Make {
	return stack.Make{
		Vars: []string{
			"CARGO_TEST_SCOPE ?= $(shell $(PAWL) affected rust 2>/dev/null)",
			"STAGED_RS := $(filter %.rs,$(STAGED))",
		},
		Fmt:      []string{"cargo fmt --all"},
		FmtCheck: []string{"cargo fmt --all -- --check"},
		Lint: []string{
			"cargo clippy --all-targets --all-features -- -D warnings",
			"cargo machete",
			"cargo deny check bans licenses sources",
		},
		Test: []string{"cargo test --all-features $(CARGO_TEST_SCOPE)"},
		Precommit: []string{
			`@[ -z "$(STAGED_RS)" ] || rustfmt --check --edition 2021 $(STAGED_RS)`,
			`@[ -z "$(STAGED_RS)" ] || cargo clippy --all-targets -- -D warnings`,
		},
		Coverage: []string{
			"@mkdir -p $(REPORTS)",
			"cargo llvm-cov --all-features --lcov --output-path $(REPORTS)/rust.lcov",
			"diff-cover $(REPORTS)/rust.lcov --compare-branch=$(PAWL_BASE) --fail-under=$(DIFF_COVERAGE)",
		},
		Audit:    []string{"cargo audit", "cargo deny check advisories"},
		Mutation: []string{
			"@mkdir -p $(REPORTS)",
			"git diff $(PAWL_BASE) > $(REPORTS)/diff.patch && cargo mutants --in-diff $(REPORTS)/diff.patch",
		},
	}
}

const rustfmtConfig = `# rustfmt configuration written by pawl. Project-owned.
edition = "2021"
max_width = 100
`

const clippyConfig = `# clippy configuration written by pawl. Thresholds for the catalog; the denied lints live in .cargo/config.toml.
cognitive-complexity-threshold = {{complexity}}
too-many-arguments-threshold = 6
`

const denyConfig = `# cargo-deny configuration written by pawl: advisories, licences, duplicate bans, sources.
[advisories]
version = 2
yanked = "deny"

[licenses]
version = 2
allow = ["MIT", "Apache-2.0", "Apache-2.0 WITH LLVM-exception", "BSD-2-Clause", "BSD-3-Clause", "ISC", "Unicode-3.0", "Zlib", "MPL-2.0"]

[bans]
multiple-versions = "warn"
wildcards = "deny"

[sources]
unknown-registry = "deny"
unknown-git = "deny"
`

const cargoConfig = `# cargo configuration written by pawl: warnings are errors; the clippy lints the catalog needs are denied.
[build]
rustflags = [
  "-Dwarnings",
  "-Wclippy::cognitive_complexity",
  "-Dclippy::print_stdout",
  "-Dclippy::print_stderr",
  "-Dclippy::dbg_macro",
  "-Dclippy::todo",
  "-Dclippy::unimplemented",
  "-Dclippy::let_underscore_must_use",
  "-Dclippy::map_err_ignore",
  "-Dclippy::unwrap_used",
  "-Dclippy::expect_used",
]
`
