package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunReportsAVersionAndExitCodes(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--version"}, &out, &errOut); code != 0 {
		t.Fatalf("--version exited %d: %s", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "pawl version ") || strings.TrimSpace(strings.TrimPrefix(out.String(), "pawl version ")) == "" {
		t.Errorf("--version printed %q", out.String())
	}
	out.Reset()
	if code := run([]string{"no-such-command"}, &out, &errOut); code != 1 {
		t.Errorf("an unknown command exited %d", code)
	}
}
