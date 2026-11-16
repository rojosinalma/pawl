package golang

import (
	"testing"

	"github.com/rojosinalma/pawl/internal/stack/stacktest"
)

const cfg = ".golangci.yml"

func TestDefaultConfigPassesVerifiers(t *testing.T) {
	a := Adapter{}
	stacktest.CheckDefaults(t, a.Configs(stacktest.Params), a.Verifiers(stacktest.Params))
}

func TestDefaultAllKeepsLintersOn(t *testing.T) {
	a := Adapter{}
	root := stacktest.Write(t, a.Configs(stacktest.Params), map[string]stacktest.Edit{cfg: stacktest.Replace("default: standard", "default: all")})
	if got := stacktest.Failing(root, a.Verifiers(stacktest.Params)); len(got) > 0 {
		t.Fatalf("linters.default: all fails verifiers: %v", got)
	}
}

func TestVerifiersReportRuleTurnedOff(t *testing.T) {
	off := func(name, old, replacement, want string) stacktest.RuleOff {
		return stacktest.RuleOff{Name: name, Edits: map[string]stacktest.Edit{cfg: stacktest.Replace(old, replacement)}, Want: want}
	}
	a := Adapter{}
	stacktest.CheckRulesOff(t, a.Configs(stacktest.Params), a.Verifiers(stacktest.Params), []stacktest.RuleOff{
		off("gocognit removed", "    - gocognit", "", "code.complexity golangci-lint/gocognit"),
		off("threshold raised", "min-complexity: 15", "min-complexity: 40", "code.complexity golangci-lint/gocognit"),
		off("errcheck disabled", "  enable:\n", "  disable: [errcheck]\n  enable:\n", "code.dropped-results golangci-lint/errcheck"),
		off("default none", "default: standard", "default: none", "code.dead golangci-lint/unused"),
		off("nilerr removed", "    - nilerr", "", "code.swallowed-errors golangci-lint/nilerr"),
		off("forbidigo removed", "    - forbidigo", "", "code.debug-leftovers golangci-lint/forbidigo"),
		off("forbid pattern changed", "        - pattern: ^fmt", "        - pattern: ^log", "code.debug-leftovers golangci-lint/forbidigo"),
		off("goimports removed", "    - goimports", "", "code.format golangci-lint/goimports"),
		off("version 1", `version: "2"`, `version: "1"`, "code.lint golangci-lint"),
		off("gosec removed", "    - gosec", "", "drift.security golangci-lint/gosec"),
		off("noctx removed", "    - noctx", "", "resilience.timeouts golangci-lint/noctx"),
		off("revive removed", "    - revive", "", "code.naming golangci-lint/revive"),
	})
}

func TestSuppressions(t *testing.T) {
	stacktest.CheckSuppressions(t, Adapter{}.Suppressions(), []stacktest.SuppressionCase{
		{Line: "x() //nolint:errcheck // the error is logged by the caller", Kind: "nolint", Reason: "the error is logged by the caller"},
		{Line: "x() //nolint:errcheck,gosec", Kind: "nolint"},
		{Line: "x() //nolint", Kind: "nolint"},
		{Line: "x() // #nosec G104 -- the file is a fixed path", Kind: "nosec", Reason: "the file is a fixed path"},
		{Line: "x() // #nosec", Kind: "nosec"},
		{Line: "//lint:ignore SA1019 the old API is needed for Go 1.20", Kind: "lint:ignore", Reason: "the old API is needed for Go 1.20"},
		{Line: "//lint:ignore SA1019", Kind: "lint:ignore"},
		{Line: `msg := "no directive here"`},
	})
}
