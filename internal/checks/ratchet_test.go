package checks

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/ratchet"
	"github.com/rojosinalma/pawl/internal/report"
)

func reasoned(name string) string {
	return "package main\n\nfunc " + name + "() {\n\tx() " + nolint + " // close errors are irrelevant here\n}\n"
}

func TestRatchetRegressionAgainstBase(t *testing.T) {
	f := newFixture(t, true)
	f.write("a.go", reasoned("a"))
	rep := f.check("check")
	got := requireFinding(t, rep, "escape.suppressions", report.Error, "rose from 0 to 1 against the base commit; new: a.go nolint")
	if n := len(find(rep, "escape.suppressions", "rose from")); n != 1 {
		t.Errorf("the regression is reported %d times, want once:\n%s", n, dump(rep))
	}
	if !strings.Contains(got.Fix, "--grow") {
		t.Errorf("fix = %q", got.Fix)
	}
}

func TestRatchetStale(t *testing.T) {
	f := newFixture(t, true)
	f.onMain("suppression", func() {
		f.write("a.go", reasoned("a"))
		f.rebaseline()
	})
	requireClean(t, f.check("check"))
	f.remove("a.go")
	requireFinding(t, f.check("check"), "escape.suppressions", report.Error, "stale baseline: run `pawl baseline`")
	f.rebaseline()
	requireClean(t, f.check("check"))
}

func TestRatchetRaise(t *testing.T) {
	for _, tc := range []struct {
		name  string
		grow  bool
		clean bool
	}{
		{"without growth", false, false},
		{"with growth", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, true)
			f.write("a.go", reasoned("a"))
			f.rebaseline()
			if tc.grow {
				b, _, err := ratchet.Load(f.dir)
				if err != nil {
					t.Fatal(err)
				}
				b.Growth = append(b.Growth, ratchet.Growth{Reason: "vendored parser needs it", From: map[string]int{"escape.suppressions": 0}, To: map[string]int{"escape.suppressions": 1}})
				if err := ratchet.Write(f.dir, b); err != nil {
					t.Fatal(err)
				}
			}
			rep := f.check("check")
			if tc.clean {
				requireClean(t, rep)
				return
			}
			requireFinding(t, rep, "escape.suppressions", report.Error, "baseline raised from 0 to 1 without `pawl baseline --grow`")
		})
	}
}

func TestRatchetNoBaseline(t *testing.T) {
	f := newFixture(t, false)
	f.remove(ratchet.FileName)
	requireFinding(t, f.check("check"), "escape.suppressions", report.Error, "no baseline: run `pawl baseline`")
	requireFinding(t, f.check("check", "escape.duplication"), "escape.duplication", report.Error, "no baseline")
	if got := f.check("check", "repo.hook"); len(got.Findings) > 0 {
		t.Errorf("a run without ratchet rules does not need a baseline:\n%s", dump(got))
	}
}

func TestRatchetCorruptBaseline(t *testing.T) {
	f := newFixture(t, false)
	f.write(ratchet.FileName, "{")
	if _, err := Run(f.load(), Options{}); err == nil {
		t.Fatal("a corrupt baseline is a run error")
	}
}

func jscpdJSON(pairs ...[2]string) string {
	var dups []string
	for _, p := range pairs {
		dups = append(dups, fmt.Sprintf(`{"firstFile":{"name":%q,"start":1,"end":20},"secondFile":{"name":%q,"start":5,"end":24},"lines":20}`, p[0], p[1]))
	}
	return `{"duplicates":[` + strings.Join(dups, ",") + `]}`
}

func TestDuplication(t *testing.T) {
	f := newFixture(t, false)
	f.write(jscpdReport, jscpdJSON([2]string{"./a.go", "b.go"}))
	cur, err := Measure(f.load())
	if err != nil {
		t.Fatal(err)
	}
	if got := cur.Offenders["escape.duplication"]; len(got) != 1 || got[0] != "a.go ~ b.go" {
		t.Fatalf("offenders = %v", got)
	}
	requireFinding(t, f.check("check"), "escape.duplication", report.Error, "the baseline does not record it")
	f.rebaseline()
	requireClean(t, f.check("check"))

	abs := f.load().Root + "/c.go"
	f.write(jscpdReport, jscpdJSON([2]string{"./a.go", "b.go"}, [2]string{abs, "d.go"}))
	requireFinding(t, f.check("check"), "escape.duplication", report.Error, "rose from 1 to 2 against the baseline; new: c.go ~ d.go")

	f.write(jscpdReport, "not json")
	if _, err := Run(f.load(), Options{}); err == nil {
		t.Fatal("a corrupt jscpd report is a run error")
	}
}

func TestDuplicationReportMissing(t *testing.T) {
	f := newFixture(t, false)
	rep := f.check("check")
	if !skipped(rep, "escape.duplication") {
		t.Error("check ring skips without a report")
	}
	requireFinding(t, f.check("nightly"), "escape.duplication", report.Error, "jscpd report is missing")
}

func TestListOffenders(t *testing.T) {
	var list []string
	for i := 0; i < 13; i++ {
		list = append(list, fmt.Sprint(i))
	}
	got := listOffenders(list)
	if !strings.HasSuffix(got, "9 and 3 more") || strings.Contains(got, "10") {
		t.Errorf("listOffenders = %q", got)
	}
	if got := listOffenders(list[:2]); got != "0, 1" {
		t.Errorf("listOffenders = %q", got)
	}
}
