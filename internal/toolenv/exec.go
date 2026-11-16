package toolenv

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rojosinalma/pawl/internal/config"
)

// LookPath finds the program on the PATH of env rather than on the process PATH; a name with a slash is used as
// it is. A program that is not there is an *exec.Error wrapping exec.ErrNotFound.
func LookPath(env []string, name string) (string, error) {
	if strings.Contains(name, "/") {
		return exec.LookPath(name)
	}
	for _, dir := range filepath.SplitList(pathOf(env)) {
		if dir == "" {
			continue
		}
		if path, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return path, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// pathOf returns the last PATH entry of env, the one a child process sees.
func pathOf(env []string) string {
	path := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}
	return path
}

// Exec runs commands in an environment of their own, each killed when Timeout passes. It is a runner.Runner whose
// programs are found on the PATH of Env (the process environment when Env is nil).
type Exec struct {
	Env     []string
	Timeout time.Duration
	// IsCombined returns standard error in the output too, as runner.Exec does.
	IsCombined bool
}

// Output runs the command in dir and returns standard output, or both streams when IsCombined is set; a failure
// carries the command line and, when standard error is kept apart and not empty, its text.
func (e Exec) Output(dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.Timeout)
	defer cancel()
	var out, stderr bytes.Buffer
	errOut := io.Writer(&stderr)
	if e.IsCombined {
		errOut = &out
	}
	err := e.run(ctx, dir, &out, errOut, name, args)
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out.Bytes(), fmt.Errorf("%w: %s", err, msg)
		}
	}
	return out.Bytes(), err
}

// Run runs the command in dir with its output going to stdout and stderr, under the timeout and ctx.
func (e Exec) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()
	return e.run(ctx, dir, stdout, stderr, name, args)
}

func (e Exec) run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args []string) error {
	env := e.Env
	if env == nil {
		env = config.Environ()
	}
	line := strings.Join(append([]string{name}, args...), " ")
	path, err := LookPath(env, name)
	if err != nil {
		return fmt.Errorf("%s: %w", line, err)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", line, err)
	}
	return nil
}
