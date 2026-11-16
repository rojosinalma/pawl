package ringrun

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// GNU make 4.4.1, `make -k --trace check` with stderr on the same pipe: lint-b and lint-c fail, lint-a and test pass.
const sample44 = `Makefile:7: update target 'lint-a' due to: target is .PHONY
echo a-out
a-out
Makefile:9: update target 'lint-b' due to: target is .PHONY
echo b-out; echo b-err >&2; exit 1
b-out
b-err
make: *** [Makefile:9: lint-b] Error 1
Makefile:11: update target 'lint-c' due to: target is .PHONY
echo c-out; exit 2
c-out
make: *** [Makefile:11: lint-c] Error 2
Makefile:13: update target 'test' due to: target is .PHONY
echo testing
testing
make: Target 'check' not remade because of errors.
`

// GNU make 4.4.1, a recipe that runs a sub-make which fails.
const sample44Sub = `make: Entering directory '/work/sub'
Makefile:2: update target 'x' due to: target does not exist
make -C .. lint-b
make[1]: Entering directory '/work'
Makefile:9: update target 'lint-b' due to: target is .PHONY
echo b-out; echo b-err >&2; exit 1
b-out
b-err
make[1]: *** [Makefile:9: lint-b] Error 1
make[1]: Leaving directory '/work'
make: *** [Makefile:2: x] Error 2
make: Leaving directory '/work/sub'
`

// GNU make 4.3 prints the same trace format as 4.4.
const sample43 = `Makefile:40: update target 'lint-typos' due to: target is .PHONY
typos
docs/guide.md:3:7: a typo
make: *** [Makefile:40: lint-typos] Error 2
Makefile:43: update target 'lint-gitleaks' due to: target is .PHONY
gitleaks git . --redact --no-banner --exit-code 1
make: Target 'check' not remade because of errors.
`

// GNU make 4.1: a target that is not a file gets "target 'x' does not exist".
const sample41 = `Makefile:2: target 'build' does not exist
go build ./...
Makefile:5: update target 'test' due to: build
go test ./...
--- FAIL: TestX
make: *** [Makefile:5: test] Error 1
`

// GNU make 3.81 (macOS), `make -k check`: no --trace, the error lines name only the target.
const sample381 = `a-out
b-out
b-err
make: *** [lint-b] Error 1
echo c-out; exit 2
c-out
make: *** [lint-c] Error 2
testing
make: Target ` + "`check'" + ` not remade because of errors.
`

func TestHasTrace(t *testing.T) {
	cases := []struct {
		name, in string
		want     bool
	}{
		{"4.4.1", "GNU Make 4.4.1\nBuilt for x86_64-pc-linux-gnu\n", true},
		{"4.3", "GNU Make 4.3\nBuilt for x86_64-pc-linux-gnu\n", true},
		{"4.0", "GNU Make 4.0\n", true},
		{"3.81", "GNU Make 3.81\nCopyright (C) 2006  Free Software Foundation, Inc.\n", false},
		{"bsd make", "make: unknown option -- -\n", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasTrace(c.in); got != c.want {
				t.Errorf("hasTrace(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestFailedTargets(t *testing.T) {
	cases := []struct {
		name, in string
		want     []string
	}{
		{"4.4", sample44, []string{"lint-b", "lint-c"}},
		{"4.4 sub-make", sample44Sub, []string{"lint-b", "x"}},
		{"4.3", sample43, []string{"lint-typos"}},
		{"3.81", sample381, []string{"lint-b", "lint-c"}},
		{"ignored error", "make: [ign.mk:2: all] Error 1 (ignored)\nmake: [all] Error 1 (ignored)\n", nil},
		{"each once", "make: *** [a] Error 1\nmake[2]: *** [a] Error 1\n", []string{"a"}},
		{"signal", "make: *** [Makefile:3: slow] Terminated\n", []string{"slow"}},
		{"no rule", "make: *** No rule to make target 'nope'.  Stop.\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := failedTargets(splitLines(c.in)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("failedTargets = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSplitSegments(t *testing.T) {
	cases := []struct {
		name, in string
		want     []segment
	}{
		{"4.4", sample44, []segment{
			{"lint-a", []string{"echo a-out", "a-out"}},
			{"lint-b", []string{"echo b-out; echo b-err >&2; exit 1", "b-out", "b-err", "make: *** [Makefile:9: lint-b] Error 1"}},
			{"lint-c", []string{"echo c-out; exit 2", "c-out", "make: *** [Makefile:11: lint-c] Error 2"}},
			{"test", []string{"echo testing", "testing"}},
		}},
		{"4.4 sub-make: the error line goes to the target it names", sample44Sub, []segment{
			{"", []string{"make: Entering directory '/work/sub'"}},
			{"x", []string{"make -C .. lint-b", "make[1]: Entering directory '/work'", "make: *** [Makefile:2: x] Error 2"}},
			{"lint-b", []string{"echo b-out; echo b-err >&2; exit 1", "b-out", "b-err", "make[1]: *** [Makefile:9: lint-b] Error 1", "make[1]: Leaving directory '/work'", "make: Leaving directory '/work/sub'"}},
		}},
		{"4.1", sample41, []segment{
			{"build", []string{"go build ./..."}},
			{"test", []string{"go test ./...", "--- FAIL: TestX", "make: *** [Makefile:5: test] Error 1"}},
		}},
		{"empty", "", []segment{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitSegments(splitLines(c.in))
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("splitSegments =\n%q\nwant\n%q", got, c.want)
			}
		})
	}
}

func numbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("line %d", i+1)
	}
	return out
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name            string
		n               int
		wantLen         int
		wantFirst, last string
	}{
		{"short", 10, 10, "line 1", "line 10"},
		{"at the cap", 400, 400, "line 1", "line 400"},
		{"over the cap", 1000, 241, "line 1", "line 1000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncate(numbered(c.n), "check")
			if len(got) != c.wantLen || got[0] != c.wantFirst || got[len(got)-1] != c.last {
				t.Fatalf("truncate(%d) = %d lines, %q … %q", c.n, len(got), got[0], got[len(got)-1])
			}
		})
	}
}

func TestTruncateMarker(t *testing.T) {
	got := truncate(numbered(1000), "check")
	if got[39] != "line 40" || got[41] != "line 801" {
		t.Errorf("head ends %q, tail starts %q", got[39], got[41])
	}
	if want := "… 760 lines omitted (pawl run check --verbose shows everything) …"; got[40] != want {
		t.Errorf("marker %q, want %q", got[40], want)
	}
}

func TestDockerArgs(t *testing.T) {
	cases := []struct {
		name    string
		environ []string
		want    string
	}{
		{"no variables", []string{"PATH=/bin"}, "run --rm -u 1000:100 -v /src/p:/work -w /work img:1 make -k check"},
		{"set ones pass, empty ones do not", []string{"PAWL_BASE=origin/main", "PAWL_FULL=", "PAWL_WORK_TYPE=fix", "OTHER=1"},
			"run --rm -u 1000:100 -v /src/p:/work -w /work -e PAWL_BASE -e PAWL_WORK_TYPE img:1 make -k check"},
		{"nil environment", nil, "run --rm -u 1000:100 -v /src/p:/work -w /work img:1 make -k check"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strings.Join(dockerArgs("img:1", "/src/p", 1000, 100, c.environ, []string{"-k", "check"}), " ")
			if got != c.want {
				t.Errorf("dockerArgs =\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}

func TestFailReport(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		isTraced bool
		want     []string
	}{
		{"4.4 traced", sample44, true, []string{
			"pawl: check failed in 1.5s: lint-b, lint-c",
			"--- lint-b", "echo b-out; echo b-err >&2; exit 1", "b-out", "b-err", "make: *** [Makefile:9: lint-b] Error 1",
			"--- lint-c", "echo c-out; exit 2", "c-out", "make: *** [Makefile:11: lint-c] Error 2",
		}},
		{"3.81 whole output", sample381, false, append([]string{"pawl: check failed in 1.5s: lint-b, lint-c"}, splitLines(sample381)...)},
		{"no failed target: whole output without trace lines", "Makefile:1: update target 'x' due to: y\nMakefile:3: *** missing separator.  Stop.\n", true,
			[]string{"pawl: check failed in 1.5s", "Makefile:3: *** missing separator.  Stop."}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := failReport("check", 1500*time.Millisecond, splitLines(c.in), c.isTraced)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("failReport =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(c.want, "\n"))
			}
		})
	}
}

func TestSummaryLines(t *testing.T) {
	cases := []struct{ got, want string }{
		{passLine("check", 2340*time.Millisecond, 4), "pawl: check passed in 2.3s (4 steps)"},
		{passLine("precommit", 12345*time.Microsecond, -1), "pawl: precommit passed in 12ms"},
		{failLine("nightly", 61*time.Second, []string{"a", "b"}), "pawl: nightly failed in 1m1s: a, b"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
	if n := stepCount(splitLines(sample44), true); n != 4 {
		t.Errorf("stepCount = %d, want 4", n)
	}
	if n := stepCount(splitLines(sample44), false); n != -1 {
		t.Errorf("stepCount without trace = %d, want -1", n)
	}
}

func TestLookupLastWins(t *testing.T) {
	if got := lookup([]string{"A=1", "B", "A=2"}, "A"); got != "2" {
		t.Errorf("lookup = %q, want 2", got)
	}
}
