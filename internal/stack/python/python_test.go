package python

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/stack"
)

func params(facts map[string]string) stack.Params {
	if facts == nil {
		facts = map[string]string{"runner": "uv run ", "uv": "true", "source_dir": "src"}
	}
	return stack.Params{Complexity: 15, FileLines: 400, DiffCoverage: 80, Facts: facts}
}

func writeDefaults(t *testing.T, p stack.Params) string {
	t.Helper()
	dir := t.TempDir()
	for _, c := range (Adapter{}).Configs(p) {
		write(t, dir, c.Path, c.Content)
	}
	return dir
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func edit(t *testing.T, dir, rel, old, new string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), old) {
		t.Fatalf("%s lacks %q", rel, old)
	}
	write(t, dir, rel, strings.Replace(string(raw), old, new, 1))
}

func findings(dir string, p stack.Params) map[string][]string {
	out := map[string][]string{}
	for _, v := range (Adapter{}).Verifiers(p) {
		for _, f := range v.Check(dir) {
			out[f.RuleID] = append(out[f.RuleID], f.Message)
		}
	}
	return out
}

func TestDefaultsPassTheirVerifiers(t *testing.T) {
	p := params(nil)
	if got := findings(writeDefaults(t, p), p); len(got) > 0 {
		t.Fatalf("default configuration reports %v", got)
	}
}

func TestTurningARuleOffReports(t *testing.T) {
	cases := []struct {
		rule, file, old, new string
	}{
		{"code.swallowed-errors", "ruff.toml", `"BLE",      # blind except`, ``},
		{"code.debug-leftovers", "ruff.toml", `ignore = ["TD002"]`, `ignore = ["TD002", "T201"]`},
		{"code.complexity", "ruff.toml", `max-complexity = 15`, `max-complexity = 40`},
		{"code.complexity", "ruff.toml", `"C90",      # mccabe complexity`, ``},
		{"code.naming", "ruff.toml", `"N",        # pep8-naming`, ``},
		{"tests.smells", "ruff.toml", `"PT",       # pytest style`, ``},
		{"code.types", "pyrightconfig.json", `"typeCheckingMode": "strict"`, `"typeCheckingMode": "basic"`},
		{"resilience.exhaustive", "pyrightconfig.json", `"reportMatchNotExhaustive": "error"`, `"reportMatchNotExhaustive": "none"`},
	}
	for _, c := range cases {
		t.Run(c.rule+"/"+c.new, func(t *testing.T) {
			p := params(nil)
			dir := writeDefaults(t, p)
			edit(t, dir, c.file, c.old, c.new)
			if got := findings(dir, p); len(got[c.rule]) == 0 {
				t.Fatalf("no %s finding; got %v", c.rule, got)
			}
		})
	}
}

func TestRuffSectionInPyproject(t *testing.T) {
	p := params(map[string]string{"has_ruff_section": "true", "source_dir": "."})
	dir := writeDefaults(t, p)
	if _, err := os.Stat(filepath.Join(dir, "ruff.toml")); err == nil {
		t.Fatal("ruff.toml written although pyproject.toml has a [tool.ruff] section")
	}
	write(t, dir, "pyproject.toml", "[project]\nname = \"greeter\"\n\n[tool.ruff.lint]\nselect = [\"ALL\"]\nignore = [\"D\"]\n\n[tool.ruff.lint.mccabe]\nmax-complexity = 15\n")
	if got := findings(dir, p); len(got) > 0 {
		t.Fatalf("pyproject selecting ALL reports %v", got)
	}
	edit(t, dir, "pyproject.toml", `ignore = ["D"]`, `ignore = ["D", "BLE"]`)
	if got := findings(dir, p); len(got["code.swallowed-errors"]) == 0 {
		t.Fatalf("ignoring BLE in pyproject.toml does not report; got %v", got)
	}
}

func TestSuppressionsMatchWithAndWithoutReason(t *testing.T) {
	cases := []struct {
		kind, line, reason string
	}{
		{"noqa", "x = 1  # noqa: E501 -- a URL that cannot wrap", "a URL that cannot wrap"},
		{"noqa", "x = 1  # noqa: E501", ""},
		{"noqa", "x = 1  # noqa", ""},
		{"type-ignore", "x = f()  # type: ignore[attr-defined] -- the stub lags", "the stub lags"},
		{"type-ignore", "x = f()  # type: ignore", ""},
		{"pyright-ignore", "x = f()  # pyright: ignore[reportUnknownMemberType] -- untyped library", "untyped library"},
		{"nosec", "run(cmd)  # nosec B602 -- fixed command", "fixed command"},
	}
	byKind := map[string]stack.Suppression{}
	for _, s := range (Adapter{}).Suppressions() {
		byKind[s.Kind] = s
	}
	for _, c := range cases {
		s := byKind[c.kind]
		m := s.Pattern.FindStringSubmatch(c.line)
		if m == nil {
			t.Fatalf("%s does not match %q", c.kind, c.line)
		}
		if got := strings.TrimSpace(m[s.Pattern.SubexpIndex("reason")]); got != c.reason {
			t.Errorf("%q: reason = %q, want %q", c.line, got, c.reason)
		}
	}
}

func TestMakeRunsTheProjectEnvironment(t *testing.T) {
	m := (Adapter{}).Make(params(nil))
	lint := strings.Join(m.Lint, "\n")
	for _, want := range []string{"uv run pyright", "deptry src", "vulture src"} {
		if !strings.Contains(lint, want) {
			t.Errorf("lint lacks %q:\n%s", want, lint)
		}
	}
	if test := strings.Join(m.Test, " "); strings.Contains(test, "-p randomly") || !strings.Contains(test, "uv run pytest") {
		t.Errorf("test recipe = %q; pytest-randomly loads itself", test)
	}
	if !strings.Contains(strings.Join(m.Coverage, "\n"), "$(REPORTS)/python-coverage.xml") {
		t.Errorf("coverage writes no xml report: %v", m.Coverage)
	}
}

func TestRuffSelectorCoverage(t *testing.T) {
	cases := []struct {
		selector, code string
		want           bool
	}{
		{"ALL", "BLE001", true},
		{"B", "B018", true},
		{"B", "BLE001", false},
		{"S1", "S110", true},
		{"S2", "S110", false},
		{"C90", "C901", true},
		{"TD002", "TD003", false},
		{"N", "N802", true},
		{"N802", "N", false},
	}
	for _, c := range cases {
		if got := covers(c.selector, c.code); got != c.want {
			t.Errorf("covers(%q, %q) = %v, want %v", c.selector, c.code, got, c.want)
		}
	}
}
