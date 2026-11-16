package shell

import (
	"testing"

	"github.com/rojosinalma/pawl/internal/stack/stacktest"
)

func TestDefaultConfigPassesVerifiers(t *testing.T) {
	a := Adapter{}
	stacktest.CheckDefaults(t, a.Configs(stacktest.Params), a.Verifiers(stacktest.Params))
}

func TestVerifiersReportRuleTurnedOff(t *testing.T) {
	off := func(name, file, old, replacement, want string) stacktest.RuleOff {
		return stacktest.RuleOff{Name: name, Edits: map[string]stacktest.Edit{file: stacktest.Replace(old, replacement)}, Want: want}
	}
	a := Adapter{}
	stacktest.CheckRulesOff(t, a.Configs(stacktest.Params), a.Verifiers(stacktest.Params), []stacktest.RuleOff{
		off("SC2034 disabled", ".shellcheckrc", "external-sources=true\n", "external-sources=true\ndisable=SC2086,SC2034\n", "code.dead shellcheck/SC2034"),
		off("hadolint no-fail", ".hadolint.yaml", "failure-threshold: warning", "no-fail: true", "code.lint hadolint"),
		off("hadolint threshold error", ".hadolint.yaml", "failure-threshold: warning", "failure-threshold: error", "code.lint hadolint"),
		off("unpinned-uses disabled", ".github/zizmor.yml", "rules: {}", "rules:\n  unpinned-uses:\n    disable: true", "drift.security zizmor/unpinned-uses"),
		off("template-injection disabled", ".github/zizmor.yml", "rules: {}", "rules:\n  template-injection:\n    disable: true", "drift.security zizmor/template-injection"),
	})
}

func TestMissingConfigReports(t *testing.T) {
	a := Adapter{}
	files := a.Configs(stacktest.Params)[1:] // without .shellcheckrc
	if got := stacktest.Failing(stacktest.Write(t, files, nil), a.Verifiers(stacktest.Params)); !stacktest.Contains(got, "code.lint shellcheck") {
		t.Fatalf("missing .shellcheckrc not reported: %v", got)
	}
}

func TestSuppressions(t *testing.T) {
	stacktest.CheckSuppressions(t, Adapter{}.Suppressions(), []stacktest.SuppressionCase{
		{Line: "# shellcheck disable=SC2086 -- the word splitting is wanted", Kind: "shellcheck-disable", Reason: "the word splitting is wanted"},
		{Line: "# shellcheck disable=SC2086,SC2034", Kind: "shellcheck-disable"},
		{Line: "# hadolint ignore=DL3008 -- the distribution pins the versions", Kind: "hadolint-ignore", Reason: "the distribution pins the versions"},
		{Line: "# hadolint ignore=DL3008", Kind: "hadolint-ignore"},
		{Line: "uses: ./local # zizmor: ignore[unpinned-uses] -- a local action", Kind: "zizmor-ignore", Reason: "a local action"},
		{Line: "uses: ./local # zizmor: ignore[unpinned-uses]", Kind: "zizmor-ignore"},
		{Line: `echo "shellcheck is a linter"`},
	})
}
