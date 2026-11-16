package ratchet

import (
	"reflect"
	"testing"
)

func TestRegressionsAndStale(t *testing.T) {
	base := &Baseline{
		Metrics:   map[string]int{"escape.suppressions": 2, "code.file-size": 1, "escape.duplication": 3},
		Offenders: map[string][]string{"code.file-size": {"a.go"}},
	}
	cur := NewCurrent()
	cur.Set("escape.suppressions", []string{"x.go:1 nolint", "y.go:2 nolint", "z.go:3 nolint"})
	cur.Set("code.file-size", []string{"b.go"})
	cur.Count("escape.duplication", 1)
	cur.Count("code.todo-links", 4)

	reg := Regressions(base, cur)
	if len(reg) != 1 {
		t.Fatalf("Regressions = %+v", reg)
	}
	if reg[0].Metric != "escape.suppressions" || reg[0].Baseline != 2 || reg[0].Current != 3 || len(reg[0].NewOffenders) != 3 {
		t.Errorf("rise not reported: %+v", reg[0])
	}

	stale := Stale(base, cur)
	got := map[string]bool{}
	for _, d := range stale {
		got[d.Metric] = true
	}
	if !got["escape.duplication"] || !got["code.todo-links"] || got["escape.suppressions"] {
		t.Errorf("Stale = %+v", stale)
	}
}

func TestWriteLoadAndFromCurrent(t *testing.T) {
	root := t.TempDir()
	if _, ok, err := Load(root); ok || err != nil {
		t.Fatalf("Load on an empty project: ok=%v err=%v", ok, err)
	}
	cur := NewCurrent()
	cur.Set("code.file-size", []string{"b.go", "a.go"})
	cur.Count("escape.duplication", 0)
	prev := &Baseline{Growth: []Growth{{Date: "2026-01-01", Reason: "imported legacy code"}}}
	b := FromCurrent(cur, prev)
	if err := Write(root, b); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(root)
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if got.Version != 1 || got.Updated == "" || got.Metrics["code.file-size"] != 2 {
		t.Errorf("baseline = %+v", got)
	}
	if !reflect.DeepEqual(got.Offenders["code.file-size"], []string{"a.go", "b.go"}) {
		t.Errorf("offenders not sorted: %v", got.Offenders)
	}
	if _, has := got.Offenders["escape.duplication"]; has {
		t.Error("empty offender list was stored")
	}
	if len(got.Growth) != 1 {
		t.Errorf("growth history lost: %+v", got.Growth)
	}
	if len(Regressions(got, cur)) != 0 || len(Stale(got, cur)) != 0 {
		t.Error("a fresh baseline is neither regressed nor stale")
	}
	if _, ok, _ := LoadAt(nil, "HEAD"); ok {
		t.Error("LoadAt without a repository returned a baseline")
	}
}
