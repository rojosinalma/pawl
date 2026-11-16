package runner

import (
	"strings"
	"testing"
	"time"
)

func TestExecSeparateStreams(t *testing.T) {
	run := Exec{Timeout: 10 * time.Second}
	out, err := run.Output(t.TempDir(), "sh", "-c", "echo out; echo err >&2")
	if err != nil || string(out) != "out\n" {
		t.Fatalf("got %q, %v", out, err)
	}
	_, err = run.Output(t.TempDir(), "sh", "-c", "echo broken >&2; exit 3")
	if err == nil || err.Error() != "sh -c echo broken >&2; exit 3: exit status 3: broken" {
		t.Fatalf("error with stderr: %v", err)
	}
	_, err = run.Output(t.TempDir(), "sh", "-c", "exit 4")
	if err == nil || err.Error() != "sh -c exit 4: exit status 4" {
		t.Fatalf("error without stderr: %v", err)
	}
}

func TestExecCombined(t *testing.T) {
	run := Exec{Timeout: 10 * time.Second, IsCombined: true}
	out, err := run.Output(t.TempDir(), "sh", "-c", "echo out; echo err >&2")
	if err != nil || !strings.Contains(string(out), "out\n") || !strings.Contains(string(out), "err\n") {
		t.Fatalf("got %q, %v", out, err)
	}
	out, err = run.Output(t.TempDir(), "sh", "-c", "echo why >&2; exit 2")
	if err == nil || err.Error() != "sh -c echo why >&2; exit 2: exit status 2" || string(out) != "why\n" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestExecTimeout(t *testing.T) {
	if _, err := (Exec{Timeout: 50 * time.Millisecond}).Output(t.TempDir(), "sleep", "5"); err == nil {
		t.Fatal("want a timeout error")
	}
}
