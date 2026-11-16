package rings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rojosinalma/pawl/internal/gitx"
	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/report"
)

type reply struct {
	out string
	err error
}

// fakeRunner answers commands by their joined name and arguments; a missing key is a failure.
type fakeRunner map[string]reply

func (f fakeRunner) Output(_, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	r, ok := f[key]
	if !ok {
		return nil, fmt.Errorf("unexpected command %q", key)
	}
	return []byte(r.out), r.err
}

func newProject(t *testing.T, repo bool) *project.Project {
	t.Helper()
	m := manifest.Defaults()
	p := &project.Project{Root: t.TempDir(), Manifest: &m}
	if repo {
		p.Repo = &gitx.Repo{Root: p.Root}
	}
	return p
}

func readFile(t *testing.T, root string) timings {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, timingsPath))
	if err != nil {
		t.Fatal(err)
	}
	var tm timings
	if err := json.Unmarshal(data, &tm); err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestRecordAppendsAndTrims(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.FixedZone("x", 3600))
	for i := 0; i < 55; i++ {
		if err := record(root, "precommit", time.Duration(i)*time.Second, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := Record(root, "check", 2*time.Second); err != nil {
		t.Fatal(err)
	}
	tm := readFile(t, root)
	if tm.Version != 1 || len(tm.Runs["precommit"]) != keepPerRing || len(tm.Runs["check"]) != 1 {
		t.Fatalf("unexpected timings: version %d, %d precommit, %d check", tm.Version, len(tm.Runs["precommit"]), len(tm.Runs["check"]))
	}
	first := tm.Runs["precommit"][0]
	if first.Seconds != 5 || first.At != "2026-11-16T09:00:00Z" || first.Ring != "precommit" {
		t.Fatalf("first kept entry %+v", first)
	}
	leftovers, _ := filepath.Glob(filepath.Join(root, ".pawl/reports/.timings-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left: %v", leftovers)
	}
}

func TestRecordRejectsUnknownRing(t *testing.T) {
	if err := Record(t.TempDir(), "weekly", time.Second); err == nil || !strings.Contains(err.Error(), "precommit, check, nightly") {
		t.Fatalf("want unknown ring error, got %v", err)
	}
}

func TestRecordCorruptFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, timingsPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Record(root, "precommit", time.Second); err == nil {
		t.Fatal("want parse error")
	}
	if _, why := durations(&project.Project{Root: root}, nil, "precommit"); why == "" {
		t.Fatal("want a reason for the unreadable file")
	}
}

func TestRecordUnwritableDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".pawl"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Record(root, "precommit", time.Second); err == nil {
		t.Fatal("want error when .pawl is a file")
	}
}

func TestMedian(t *testing.T) {
	s := time.Second
	if m := median([]time.Duration{3 * s, 1 * s, 2 * s}); m != 2*s {
		t.Fatalf("odd median %v", m)
	}
	if m := median([]time.Duration{4 * s, 1 * s, 2 * s, 3 * s}); m != 2500*time.Millisecond {
		t.Fatalf("even median %v", m)
	}
}

func recordAll(t *testing.T, root string, secs ...float64) {
	t.Helper()
	for _, v := range secs {
		if err := Record(root, "precommit", time.Duration(v*float64(time.Second))); err != nil {
			t.Fatal(err)
		}
	}
}

func onlyPrecommit(p *project.Project) {
	p.Manifest.Rings = map[string]manifest.Ring{"precommit": {Target: "10s", Limit: "20s"}}
}

func TestBudgetPrecommit(t *testing.T) {
	cases := []struct {
		name     string
		secs     []float64
		severity report.Severity
		message  string
	}{
		{"within", []float64{5, 6, 7}, "", ""},
		{"over target", []float64{12, 12, 12}, report.Warning, "precommit median 12s is over its target 10s (limit 20s)"},
		{"over limit", []float64{99, 99, 99, 99, 99, 99, 99, 99, 21.3, 21.3, 21.3, 21.3, 21.3, 21.3, 21.3, 21.3}, report.Error,
			"precommit median 21.3s exceeds its limit 20s (target 10s)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newProject(t, false)
			onlyPrecommit(p)
			recordAll(t, p.Root, tc.secs...)
			r, err := Budget(p)
			if err != nil {
				t.Fatal(err)
			}
			assertOne(t, r, tc.severity, tc.message)
		})
	}
}

// assertOne checks that the ring was evaluated and produced the finding (none when severity is empty).
func assertOne(t *testing.T, r *report.Report, severity report.Severity, message string) {
	t.Helper()
	if len(r.Checked) != 1 || r.Checked[0] != ruleID {
		t.Fatalf("checked %v", r.Checked)
	}
	switch {
	case severity == "" && len(r.Findings) != 0:
		t.Fatalf("want no findings, got %+v", r.Findings)
	case severity == "":
	case len(r.Findings) != 1 || r.Findings[0].Severity != severity || r.Findings[0].Message != message || r.Findings[0].Fix == "":
		t.Fatalf("findings %+v", r.Findings)
	}
}

func TestBudgetSkipsFewSamples(t *testing.T) {
	p := newProject(t, false)
	onlyPrecommit(p)
	recordAll(t, p.Root, 1, 2)
	r, err := Budget(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Skipped) != 1 || r.Skipped[0].Reason != "precommit: 2 runs, need 3" || len(r.Checked) != 0 {
		t.Fatalf("skipped %+v checked %v", r.Skipped, r.Checked)
	}
}

func TestBudgetBadRingAndUnknownRing(t *testing.T) {
	p := newProject(t, false)
	p.Manifest.Rings = map[string]manifest.Ring{"weekly": {Target: "1s", Limit: "2s"}}
	r, err := Budget(p)
	if err != nil || len(r.Skipped) != 1 || !strings.Contains(r.Skipped[0].Reason, "weekly") {
		t.Fatalf("got %+v, %v", r, err)
	}
	p.Manifest.Rings = map[string]manifest.Ring{"precommit": {Target: "x", Limit: "2s"}}
	if _, err := Budget(p); err == nil {
		t.Fatal("want duration error")
	}
}

const runList = `[
 {"databaseId":1,"conclusion":"success","createdAt":"2026-11-01T10:00:00Z","updatedAt":"2026-11-01T10:20:00Z"},
 {"databaseId":2,"conclusion":"failure","createdAt":"2026-11-02T10:00:00Z","updatedAt":"2026-11-02T10:01:00Z"},
 {"databaseId":3,"conclusion":"success","createdAt":"2026-11-03T10:00:00Z","updatedAt":"2026-11-03T10:20:00Z"},
 {"databaseId":4,"conclusion":"success","createdAt":"2026-11-04T10:00:00Z","updatedAt":"2026-11-04T10:20:00Z"}
]`

// jobsJSON has 2 minutes of setup and the given minutes of work.
func jobsJSON(work int) string {
	return fmt.Sprintf(`{"jobs":[{"steps":[
 {"name":"Set up job","startedAt":"2026-11-01T10:00:00Z","completedAt":"2026-11-01T10:01:00Z"},
 {"name":"Run actions/checkout@v4","startedAt":"2026-11-01T10:01:00Z","completedAt":"2026-11-01T10:02:00Z"},
 {"name":"Run make check","startedAt":"2026-11-01T10:02:00Z","completedAt":"2026-11-01T10:%02d:00Z"},
 {"name":"Post Run actions/checkout@v4","startedAt":"2026-11-01T10:30:00Z","completedAt":"2026-11-01T10:31:00Z"},
 {"name":"Complete job","startedAt":"2026-11-01T10:31:00Z","completedAt":"2026-11-01T10:32:00Z"}]}]}`, 2+work)
}

func ciRunner(branch string) fakeRunner {
	return fakeRunner{
		"git remote get-url origin":                                      {out: "git@github.com:o/r.git\n"},
		"gh repo view --json defaultBranchRef -q .defaultBranchRef.name": {out: branch},
		"gh run list --workflow check.yml --branch trunk --status completed --limit 10 --json databaseId,conclusion,createdAt,updatedAt": {out: runList},
		"gh run view 1 --json jobs": {out: jobsJSON(11)},
		"gh run view 3 --json jobs": {out: jobsJSON(11)},
		"gh run view 4 --json jobs": {err: errors.New("boom")},
		"gh run list --workflow nightly.yml --branch trunk --status completed --limit 10 --json databaseId,conclusion,createdAt,updatedAt": {out: "[]"},
	}
}

func TestBudgetCI(t *testing.T) {
	p := newProject(t, true)
	delete(p.Manifest.Rings, "precommit")
	r, err := budget(p, ciRunner("trunk\n"))
	if err != nil {
		t.Fatal(err)
	}
	// samples: 11m, 11m (setup excluded), 20m (wall time fallback): median 11m, over the 10m target.
	if len(r.Findings) != 1 || r.Findings[0].Severity != report.Warning || !strings.HasPrefix(r.Findings[0].Message, "check median 11m0s") {
		t.Fatalf("findings %+v", r.Findings)
	}
	if len(r.Skipped) != 1 || r.Skipped[0].Reason != "nightly: 0 runs, need 3" {
		t.Fatalf("skipped %+v", r.Skipped)
	}
}

func TestBudgetCIFallsBackToManifestBase(t *testing.T) {
	p := newProject(t, true)
	delete(p.Manifest.Rings, "precommit")
	p.Manifest.Base = "origin/trunk"
	run := ciRunner("")
	run["gh repo view --json defaultBranchRef -q .defaultBranchRef.name"] = reply{err: errors.New("auth")}
	r, err := budget(p, run)
	if err != nil || len(r.Checked) != 1 {
		t.Fatalf("got %+v, %v", r, err)
	}
}

func TestBudgetCIUnavailable(t *testing.T) {
	cases := []struct {
		name string
		repo bool
		run  fakeRunner
		want string
	}{
		{"no repo", false, fakeRunner{}, "check: not a git repository"},
		{"no remote", true, fakeRunner{}, "check: no origin remote"},
		{"no gh", true, fakeRunner{
			"git remote get-url origin":                                      {out: "x"},
			"gh repo view --json defaultBranchRef -q .defaultBranchRef.name": {err: fmt.Errorf("gh: %w", exec.ErrNotFound)},
		}, "check: gh is not installed"},
		{"list fails", true, fakeRunner{
			"git remote get-url origin":                                      {out: "x"},
			"gh repo view --json defaultBranchRef -q .defaultBranchRef.name": {out: "trunk"},
		}, "check: gh run list failed"},
		{"bad json", true, fakeRunner{
			"git remote get-url origin":                                      {out: "x"},
			"gh repo view --json defaultBranchRef -q .defaultBranchRef.name": {out: "trunk"},
			"gh run list --workflow check.yml --branch trunk --status completed --limit 10 --json databaseId,conclusion,createdAt,updatedAt": {out: "{"},
		}, "check: gh run list output"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newProject(t, tc.repo)
			p.Manifest.Rings = map[string]manifest.Ring{"check": {Target: "10m", Limit: "15m"}}
			r, err := budget(p, tc.run)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Skipped) != 1 || !strings.HasPrefix(r.Skipped[0].Reason, tc.want) {
				t.Fatalf("skipped %+v", r.Skipped)
			}
		})
	}
}

func TestRunDurationBadJobs(t *testing.T) {
	c := &ciSource{p: newProject(t, true), run: fakeRunner{"gh run view 9 --json jobs": {out: "nope"}}}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if d := c.runDuration(ghRun{DatabaseID: 9, CreatedAt: start, UpdatedAt: start.Add(time.Minute)}); d != time.Minute {
		t.Fatalf("fallback %v", d)
	}
	c.run = fakeRunner{"gh run view 9 --json jobs": {out: `{"jobs":[]}`}}
	if d := c.runDuration(ghRun{DatabaseID: 9, CreatedAt: start, UpdatedAt: start.Add(time.Minute)}); d != time.Minute {
		t.Fatalf("empty jobs fallback %v", d)
	}
}

func TestExecRunner(t *testing.T) {
	run := execRunner{timeout: 10 * time.Second}
	out, err := run.Output(t.TempDir(), "git", "--version")
	if err != nil || !strings.HasPrefix(string(out), "git version") {
		t.Fatalf("git --version: %q %v", out, err)
	}
	if _, err := run.Output(t.TempDir(), "git", "no-such-command"); err == nil || !strings.Contains(err.Error(), "git no-such-command") {
		t.Fatalf("want wrapped error, got %v", err)
	}
	if _, err := run.Output(t.TempDir(), "pawl-no-such-binary"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
