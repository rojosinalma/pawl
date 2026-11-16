// Package runner runs outbound commands under a timeout, behind an interface the callers fake in their tests.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Runner runs an outbound command in dir and returns its output.
type Runner interface {
	Output(dir, name string, args ...string) ([]byte, error)
}

// Exec runs commands as child processes; each one is killed when Timeout passes.
type Exec struct {
	Timeout time.Duration
	// IsCombined returns standard error in the output too, for tools that print what the caller reads (a version
	// banner, for example) on either stream.
	IsCombined bool
}

// Output runs the command in dir under the timeout. It returns standard output, or both streams when IsCombined is
// set; a failure carries the command line and, when standard error is kept apart and not empty, its text.
func (e Exec) Output(dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	line := name + " " + strings.Join(args, " ")
	if e.IsCombined {
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return out.Bytes(), fmt.Errorf("%s: %w", line, err)
		}
		return out.Bytes(), nil
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return out, fmt.Errorf("%s: %w: %s", line, err, msg)
	}
	return out, fmt.Errorf("%s: %w", line, err)
}
