package ghx

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rojosinalma/pawl/internal/catalog"
	"github.com/rojosinalma/pawl/internal/gitx"
	"github.com/rojosinalma/pawl/internal/project"
)

type reply struct {
	out string
	err error
}

type fakeRunner map[string]reply

func (f fakeRunner) Output(_, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	r, ok := f[key]
	if !ok {
		return nil, fmt.Errorf("unexpected command %q", key)
	}
	return []byte(r.out), r.err
}

const (
	remoteKey = "git remote get-url origin"
	prKey     = "gh pr view --json number,url,title,state,mergeStateStatus,statusCheckRollup"
)

func newProject(t *testing.T) *project.Project {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	return &project.Project{Root: root, Repo: &gitx.Repo{Root: root}, Catalog: cat}
}

const failingPR = `{"number":7,"url":"https://github.com/o/r/pull/7","title":"Add x","state":"OPEN","mergeStateStatus":"BLOCKED",
"statusCheckRollup":[
 {"__typename":"CheckRun","name":"check","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"https://github.com/o/r/actions/runs/123/job/9"},
 {"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"https://github.com/o/r/actions/runs/124/job/9"},
 {"__typename":"CheckRun","name":"nightly","status":"IN_PROGRESS","conclusion":"","detailsUrl":"https://github.com/o/r/actions/runs/125/job/9"},
 {"__typename":"StatusContext","context":"ci/external","state":"PENDING","targetUrl":"https://ci.example.com/1"},
 {"__typename":"CheckRun","name":"slow","status":"COMPLETED","conclusion":"TIMED_OUT","detailsUrl":"https://github.com/o/r/actions/runs/126/job/9"}
]}`

const failedLog = "check\tSet up job\t2026-11-16T10:00:00.1234567Z Runner version 2\n" +
	"check\tRun make check\t2026-11-16T10:01:00.1234567Z go vet ./...\n" +
	"check\tRun make check\t2026-11-16T10:01:01.1234567Z ##[error]internal/a/a.go:12:3: speed.rings: too slow\n" +
	"check\tRun make check\t2026-11-16T10:01:02.1234567Z other.go:1: second\n"

func TestStatusFailing(t *testing.T) {
	p := newProject(t)
	run := fakeRunner{
		remoteKey:                      {out: "git@github.com:o/r.git"},
		prKey:                          {out: failingPR},
		"gh run view 123 --log-failed": {out: failedLog},
		"gh run view 126 --log-failed": {err: errors.New("log gone")},
	}
	var b strings.Builder
	failed, err := status(p, &b, run)
	if err != nil || !failed {
		t.Fatalf("failed=%v err=%v", failed, err)
	}
	out := b.String()
	for _, want := range []string{
		"#7 Add x (open, merge blocked) https://github.com/o/r/pull/7",
		"failure   check",
		"internal/a/a.go:12:3: speed.rings: too slow",
		"fix: run make check locally",
		"see: pawl rules show speed.rings",
		"success   lint",
		"pending   nightly",
		"pending   ci/external",
		"timed_out slow",
		"log unavailable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "other.go") {
		t.Errorf("only the first finding line is printed:\n%s", out)
	}
}

func TestStatusPendingOnly(t *testing.T) {
	p := newProject(t)
	pr := `{"number":8,"url":"u","title":"t","state":"OPEN","statusCheckRollup":[
	 {"name":"check","status":"QUEUED"},
	 {"context":"ext","state":"SUCCESS"},
	 {"context":"ext2","state":"ERROR","targetUrl":"https://ci.example.com"}]}`
	var b strings.Builder
	failed, err := status(p, &b, fakeRunner{remoteKey: {out: "x"}, prKey: {out: pr}})
	if err != nil || !failed {
		t.Fatalf("an errored status context fails: failed=%v err=%v", failed, err)
	}
	if !strings.Contains(b.String(), "pending   check") || !strings.Contains(b.String(), "success   ext") {
		t.Fatalf("output:\n%s", b.String())
	}
	b.Reset()
	failed, err = status(p, &b, fakeRunner{remoteKey: {out: "x"}, prKey: {out: `{"number":9,"url":"u","title":"t","state":"OPEN","statusCheckRollup":[{"name":"check","status":"IN_PROGRESS"}]}`}})
	if err != nil || failed {
		t.Fatalf("pending is not failed: failed=%v err=%v", failed, err)
	}
	b.Reset()
	if _, err := status(p, &b, fakeRunner{remoteKey: {out: "x"}, prKey: {out: `{"number":9,"statusCheckRollup":[]}`}}); err != nil || !strings.Contains(b.String(), "no checks") {
		t.Fatalf("no checks: %v\n%s", err, b.String())
	}
}

func TestStatusUnavailable(t *testing.T) {
	cases := []struct {
		name string
		repo bool
		run  fakeRunner
		want string
	}{
		{"no repo", false, fakeRunner{}, "not a git repository"},
		{"no remote", true, fakeRunner{}, "no origin remote"},
		{"no gh", true, fakeRunner{remoteKey: {out: "x"}, prKey: {err: fmt.Errorf("gh: %w", exec.ErrNotFound)}}, "gh is not installed"},
		{"no pr", true, fakeRunner{remoteKey: {out: "x"}, prKey: {err: errors.New("gh pr view: exit status 1: no pull requests found for branch \"x\"")}}, "no pull request for this branch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newProject(t)
			if !tc.repo {
				p.Repo = nil
			}
			var b strings.Builder
			failed, err := status(p, &b, tc.run)
			if err != nil || failed || !strings.Contains(b.String(), tc.want) || strings.Count(b.String(), "\n") != 1 {
				t.Fatalf("failed=%v err=%v out=%q", failed, err, b.String())
			}
		})
	}
}

func TestStatusGhErrors(t *testing.T) {
	p := newProject(t)
	if _, err := status(p, &strings.Builder{}, fakeRunner{remoteKey: {out: "x"}, prKey: {err: errors.New("auth required")}}); err == nil {
		t.Fatal("want gh error")
	}
	if _, err := status(p, &strings.Builder{}, fakeRunner{remoteKey: {out: "x"}, prKey: {out: "{"}}); err == nil {
		t.Fatal("want parse error")
	}
}

func TestFirstFindingWithoutLocation(t *testing.T) {
	step, line := firstFinding("job\tRun make lint\t2026-11-16T10:00:00Z boom\n")
	if step != "Run make lint" || line != "" {
		t.Fatalf("step=%q line=%q", step, line)
	}
	if id := ruleID(&project.Project{}, "a.go:1: repo.hook: x"); id != "repo.hook" {
		t.Fatalf("without catalog: %q", id)
	}
	if id := ruleID(newProject(t), "a.go:1: not.arule: x"); id != "" {
		t.Fatalf("unknown rule: %q", id)
	}
}

func TestExecRunner(t *testing.T) {
	run := execRunner{timeout: 10 * time.Second}
	if out, err := run.Output(t.TempDir(), "git", "--version"); err != nil || !strings.HasPrefix(string(out), "git version") {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := run.Output(t.TempDir(), "pawl-no-such-binary"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	var b strings.Builder
	if failed, err := Status(&project.Project{Root: t.TempDir()}, &b); err != nil || failed {
		t.Fatalf("no repo: %v %v", failed, err)
	}
}
