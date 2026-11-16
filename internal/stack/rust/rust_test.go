package rust

import (
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/makefile"
	"github.com/rojosinalma/pawl/internal/stack"
	"github.com/rojosinalma/pawl/internal/stack/stacktest"
)

// files are the configurations plus the Makefile init writes, which holds CLIPPY_LINTS.
func files() []stack.ConfigFile {
	a := Adapter{}
	mk := makefile.Generate([]stack.Adapter{a}, stacktest.Params, 80, "origin/main")
	return append(a.Configs(stacktest.Params), stack.ConfigFile{Path: "Makefile", Content: mk})
}

func TestDefaultConfigPassesVerifiers(t *testing.T) {
	stacktest.CheckDefaults(t, files(), Adapter{}.Verifiers(stacktest.Params))
}

func TestVerifiersReportRuleTurnedOff(t *testing.T) {
	off := func(name, file, old, replacement, want string) stacktest.RuleOff {
		return stacktest.RuleOff{Name: name, Edits: map[string]stacktest.Edit{file: stacktest.Replace(old, replacement)}, Want: want}
	}
	dropAll := func(_ *testing.T, content string) string {
		return strings.ReplaceAll(content, "-- $(CLIPPY_LINTS)", "")
	}
	stacktest.CheckRulesOff(t, files(), Adapter{}.Verifiers(stacktest.Params), []stacktest.RuleOff{
		{Name: "clippy without the lints", Edits: map[string]stacktest.Edit{"Makefile": dropAll}, Want: "code.lint clippy"},
		off("warnings allowed", "Makefile", "-D warnings ", "", "code.lint clippy"),
		off("print_stdout dropped", "Makefile", "-D clippy::print_stdout ", "", "code.debug-leftovers clippy/print_stdout"),
		off("print_stdout warned only", "Makefile", "-D clippy::print_stdout", "-W clippy::print_stdout", "code.debug-leftovers clippy/print_stdout"),
		off("dbg_macro dropped", "Makefile", "-D clippy::dbg_macro ", "", "code.debug-leftovers clippy/dbg_macro"),
		off("let_underscore dropped", "Makefile", "-D clippy::let_underscore_must_use ", "", "code.swallowed-errors clippy/let_underscore_must_use"),
		off("map_err_ignore dropped", "Makefile", "-D clippy::map_err_ignore ", "", "code.swallowed-errors clippy/map_err_ignore"),
		off("unwrap_used dropped", "Makefile", "-D clippy::unwrap_used ", "", "drift.security clippy/unwrap_used"),
		off("complexity dropped", "Makefile", "-D clippy::cognitive_complexity ", "", "code.complexity clippy/cognitive_complexity"),
		off("threshold raised", "clippy.toml", "cognitive-complexity-threshold = 15", "cognitive-complexity-threshold = 40", "code.complexity clippy/cognitive_complexity"),
	})
}

func TestSuppressions(t *testing.T) {
	stacktest.CheckSuppressions(t, Adapter{}.Suppressions(), []stacktest.SuppressionCase{
		{Line: "#[allow(clippy::print_stdout)] // the CLI prints its result", Kind: "allow", Reason: "the CLI prints its result"},
		{Line: "#[allow(clippy::print_stdout)]", Kind: "allow"},
		{Line: "#![allow(dead_code, clippy::todo)]", Kind: "allow"},
		{Line: `#[expect(clippy::unwrap_used, reason = "the value is checked above")]`, Kind: "expect", Reason: "the value is checked above"},
		{Line: "#[expect(clippy::unwrap_used)]", Kind: "expect"},
		{Line: "unsafe { // the pointer comes from Box::into_raw", Kind: "unsafe", Reason: "the pointer comes from Box::into_raw"},
		{Line: "unsafe {", Kind: "unsafe"},
		{Line: `let s = "allow me";`},
	})
}
