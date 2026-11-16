package checks

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/report"
)

// The directives are assembled so this test file does not count as a suppression in pawl's own tree.
var (
	nolint    = "//" + "nolint:errcheck"
	todoMark  = "TO" + "DO"
	fixmeMark = "FIX" + "ME"
)

func TestForbiddenPatterns(t *testing.T) {
	f := newFixture(t, false)
	f.write(forbiddenFile, "# names that must not reach git\nacme-internal\n\n(\nsecret[0-9]+\n")
	f.write("notes.txt", "first line\nsee acme-internal docs\nsecret42 and acme-internal\n")
	rep := f.check("check", "repo.private")
	got := requireFinding(t, rep, "repo.private", report.Error, "forbidden pattern 1 (acm...)")
	if got.File != "notes.txt" || got.Line != 2 {
		t.Errorf("location = %s:%d, want notes.txt:2", got.File, got.Line)
	}
	requireFinding(t, rep, "repo.private", report.Error, "forbidden pattern 3 (sec...)")
	requireFinding(t, rep, "repo.private", report.Error, "pattern 2 is not a valid regular expression")
	for _, x := range rep.Findings {
		if strings.Contains(x.Message, "acme-internal") || strings.Contains(x.Message, "secret42") {
			t.Errorf("finding repeats the forbidden text: %s", x.Message)
		}
	}
	if n := len(find(rep, "repo.private", "forbidden pattern")); n != 3 {
		t.Errorf("got %d matches, want 3:\n%s", n, dump(rep))
	}
}

func TestForbiddenPatternsAbsent(t *testing.T) {
	f := newFixture(t, false)
	rep := f.check("check", "repo.private")
	requireClean(t, rep)
	if skipped(rep, "repo.private") {
		t.Error("an absent pattern file is not a skip")
	}
}

func TestForbiddenPatternsSkipBinary(t *testing.T) {
	f := newFixture(t, false)
	f.write(forbiddenFile, "acme\n")
	f.write("blob.bin", "acme\x00acme\n")
	requireClean(t, f.check("check", "repo.private"))
}

func TestPrecommitScansStagedFilesOnly(t *testing.T) {
	f := newFixture(t, true)
	f.write(forbiddenFile, "acme\n")
	f.write("committed.txt", "acme\n")
	f.commit("old file")
	f.write("staged.go", "package main\n\nfunc a() { b() } "+nolint+"\n")
	f.write("unstaged.go", "package main\n\nfunc c() { d() } "+nolint+"\n")
	f.write("excluded/x.txt", "acme\n")
	f.write("staged.txt", "acme\n")
	f.run("git", "add", "staged.go", "staged.txt", "excluded/x.txt")
	f.editManifest(func(m *manifest.Manifest) { m.Project.Exclude = []string{"excluded"} })
	rep := f.check("precommit")
	files := map[string]bool{}
	for _, x := range rep.Findings {
		files[x.File] = true
	}
	if !files["staged.go"] || !files["staged.txt"] || len(files) != 2 {
		t.Fatalf("precommit should report staged.go and staged.txt only:\n%s", dump(rep))
	}
	requireFinding(t, rep, "escape.suppressions", report.Error, "suppression without a reason (nolint)")
}

func TestSuppressions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		line   string
		reason bool
	}{
		{"without reason", "x() " + nolint, false},
		{"short reason", "x() " + nolint + " // too short", false},
		{"with reason", "x() " + nolint + " // close errors are irrelevant here", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, false)
			f.write("a.go", "package main\n\nfunc a() {\n\t"+tc.line+"\n}\n")
			rep := f.check("check", "escape.suppressions")
			got := find(rep, "escape.suppressions", "without a reason")
			if tc.reason && len(got) > 0 {
				t.Fatalf("reasoned suppression reported: %v", got)
			}
			if !tc.reason {
				x := requireFinding(t, rep, "escape.suppressions", report.Error, "without a reason (nolint)")
				if x.File != "a.go" || x.Line != 4 || !strings.Contains(x.Fix, "// <reason>") {
					t.Errorf("finding = %+v", x)
				}
			}
			requireFinding(t, rep, "escape.suppressions", report.Error, "rose from 0 to 1")
		})
	}
}

func TestSuppressionsSkippedFiles(t *testing.T) {
	f := newFixture(t, false)
	directive := "gitleaks" + ":allow"
	for _, path := range []string{".pawl/notes.txt", "go.sum", "deps.lock", ".golangci.yml", "bin.dat", "README.md", "docs/guide.rst"} {
		content := directive + "\n"
		if path == "bin.dat" {
			content = "\x00" + content
		}
		f.write(path, content)
	}
	cur, err := Measure(f.load())
	if err != nil {
		t.Fatal(err)
	}
	if n := cur.Metrics["escape.suppressions"]; n != 0 {
		t.Fatalf("suppressions = %d, want 0: %v", n, cur.Offenders["escape.suppressions"])
	}
	f.write("deploy.yaml", directive+"\n")
	cur, err = Measure(f.load())
	if err != nil {
		t.Fatal(err)
	}
	if got := cur.Offenders["escape.suppressions"]; len(got) != 1 || got[0] != "deploy.yaml gitleaks-allow" {
		t.Fatalf("offenders = %v", got)
	}
}

func TestReasonSyntax(t *testing.T) {
	for _, kind := range []string{"nolint", "noqa", "unsafe", "biome-ignore"} {
		if s := reasonSyntax(kind); !strings.Contains(s, "reason") {
			t.Errorf("%s: %q", kind, s)
		}
	}
	if s := reasonSyntax("unknown"); s != "<directive> -- <reason>" {
		t.Errorf("fallback = %q", s)
	}
}

func TestMetricsMeasured(t *testing.T) {
	f := newFixture(t, false)
	f.editManifest(func(m *manifest.Manifest) {
		m.Budgets.FileLines = 7
		m.Quarantine = []manifest.QuarantinedTest{{Name: "TestFlaky", Issue: "https://example.com/1"}}
	})
	f.write("big.go", "package main\n"+strings.Repeat("// line\n", 10))
	f.write("big_test.go", "package main\n"+strings.Repeat("// line\n", 10))
	f.write("todo.go", fmt.Sprintf("package main\n\n// %s: tidy\n// %s see #12\n// %s https://example.com/x\nvar s = \"%s in a string\"\n", todoMark, fixmeMark, todoMark, todoMark))
	f.write("todo_test.go", "package main\n\n// "+todoMark+": tests may keep them\n")
	cur, err := Measure(f.load())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"code.file-size":   {"big.go", "big_test.go"},
		"code.todo-links":  {"todo.go"},
		"tests.quarantine": {"TestFlaky"},
	}
	for metric, offenders := range want {
		if got := strings.Join(cur.Offenders[metric], ","); got != strings.Join(offenders, ",") {
			t.Errorf("%s offenders = %s, want %v", metric, got, offenders)
		}
	}
	if _, ok := cur.Metrics["escape.duplication"]; ok {
		t.Error("duplication without a report must be omitted")
	}
}

func TestMetricsOmittedWhenWaived(t *testing.T) {
	f := newFixture(t, false)
	f.editManifest(func(m *manifest.Manifest) {
		m.Rules["code.file-size"] = manifest.Rule{Waived: "generated code dominates this repository"}
		m.Rules["code.debug-leftovers"] = manifest.Rule{Waived: "no linter covers this stack yet"}
	})
	cur, err := Measure(f.load())
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range []string{"code.file-size", "code.todo-links"} {
		if _, ok := cur.Metrics[metric]; ok {
			t.Errorf("%s measured although its rule is waived", metric)
		}
	}
}

func TestDirectivesInsideStringLiteralsAreNotSuppressions(t *testing.T) {
	directive := "//" + "nolint:errcheck"
	cases := []struct {
		line string
		want bool
	}{
		{"x := call() " + directive + " // the error is logged by the caller", false},
		{`{"` + directive + `", "", true},`, true},
		{"re := regexp.MustCompile(`" + directive + "`)", true},
		{"msg := '" + directive + "'", true},
		{"// don't care " + directive, false},
		{`s := "a" ` + directive, false},
		{`{Line: "x() ` + directive + ` // the reason runs to the end", Kind: "nolint"},`, true},
		{`s := "a\"` + directive + `"`, true},
	}
	for _, c := range cases {
		start := strings.Index(c.line, directive)
		if got := quoted(c.line, start); got != c.want {
			t.Errorf("quoted(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}
