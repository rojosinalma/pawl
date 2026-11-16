package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func sample() *Report {
	r := &Report{Tool: "pawl", Version: "test", Checked: []string{"code.lint", "repo.hook"}}
	r.Add(
		Errorf("repo.hook", ".githooks/pre-commit", 0, "missing").WithFix("pawl init"),
		Warnf("code.lint", "a.go", 12, "50%% slower"),
		Errorf("code.lint", "a.go", 3, "first\nsecond"),
	)
	r.Skip("escape.duplication", "no report")
	return r
}

func TestTextOutputIsSortedAndSummarised(t *testing.T) {
	var b bytes.Buffer
	r := sample()
	if err := r.Write(&b, Text); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	first := strings.Index(out, "a.go:3: code.lint")
	second := strings.Index(out, "a.go:12: warning: code.lint")
	third := strings.Index(out, ".githooks/pre-commit: repo.hook: missing")
	if first < 0 || second < first || third < second {
		t.Errorf("unexpected order:\n%s", out)
	}
	for _, want := range []string{"    fix: pawl init", "skipped: escape.duplication: no report", "2 checks, 2 errors, 1 warnings"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output lacks %q:\n%s", want, out)
		}
	}
	if !r.Failed() || r.Errors() != 2 {
		t.Errorf("Failed=%v Errors=%d", r.Failed(), r.Errors())
	}
}

func TestCleanAndWarningSummaries(t *testing.T) {
	var b bytes.Buffer
	clean := &Report{Checked: []string{"a"}}
	_ = clean.Write(&b, Text)
	if !strings.Contains(b.String(), "1 checks, no findings") || clean.Failed() {
		t.Errorf("clean summary: %q", b.String())
	}
	b.Reset()
	warn := &Report{Checked: []string{"a"}}
	warn.Add(Warnf("speed.rings", "", 0, "slow"))
	_ = warn.Write(&b, Text)
	if !strings.Contains(b.String(), "1 checks, 1 warnings") || warn.Failed() {
		t.Errorf("warning summary: %q", b.String())
	}
}

func TestJSONAndSARIFAreValid(t *testing.T) {
	var b bytes.Buffer
	if err := sample().Write(&b, JSON); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(b.Bytes(), &decoded); err != nil || len(decoded.Findings) != 3 {
		t.Fatalf("JSON: %v %+v", err, decoded)
	}
	b.Reset()
	if err := sample().Write(&b, SARIF); err != nil {
		t.Fatal(err)
	}
	var sarif struct {
		Version string `json:"version"`
		Runs    []struct {
			Results []struct {
				RuleID string `json:"ruleId"`
				Level  string `json:"level"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(b.Bytes(), &sarif); err != nil {
		t.Fatal(err)
	}
	if sarif.Version != "2.1.0" || len(sarif.Runs) != 1 || len(sarif.Runs[0].Results) != 3 {
		t.Errorf("SARIF = %+v", sarif)
	}
	b.Reset()
	if err := (&Report{Tool: "pawl"}).Write(&b, SARIF); err != nil || !strings.Contains(b.String(), `"results": []`) {
		t.Errorf("empty SARIF: %v %s", err, b.String())
	}
}

func TestGitHubAnnotationsEscapeNewlines(t *testing.T) {
	var b bytes.Buffer
	if err := sample().Write(&b, GitHub); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"::error file=a.go,line=3,title=code.lint::first%0Asecond", "::warning file=a.go,line=12,title=code.lint::50%25 slower", "Fix: pawl init"} {
		if !strings.Contains(out, want) {
			t.Errorf("annotations lack %q:\n%s", want, out)
		}
	}
}

func TestParseFormat(t *testing.T) {
	for _, name := range []string{"text", "json", "sarif", "github"} {
		if _, err := ParseFormat(name); err != nil {
			t.Errorf("ParseFormat(%s): %v", name, err)
		}
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Error("ParseFormat accepted xml")
	}
}
