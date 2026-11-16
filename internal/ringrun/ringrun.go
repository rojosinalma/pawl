// Package ringrun runs a make target of the project, a ring (precommit, check, nightly) above all, and reports it the way
// an agent or a person needs it: one line when it passes, only the failing steps when it does not.
package ringrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rojosinalma/pawl/internal/makefile"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/rings"
	"github.com/rojosinalma/pawl/internal/runner"
)

const (
	// cancelGrace is how long a cancelled make has between SIGTERM and SIGKILL (and its pipes stay open).
	cancelGrace = 3 * time.Second
	// gitTimeout bounds the read of the clone's toolbox setting.
	gitTimeout = 10 * time.Second
)

// Options of one run.
type Options struct {
	// Verbose streams make's own output instead of the summary.
	Verbose bool
	// NoToolbox runs make on this machine even when the clone names a toolbox image (git config pawl.toolbox).
	NoToolbox bool
	// Environ is the environment make runs with (the caller adds the directories of the pinned tools to PATH).
	Environ []string
	Stdout  io.Writer
	Stderr  io.Writer
}

// Run executes `make <ring>` for precommit, check or nightly and returns whether the ring passed. The error is for
// failures to run at all (no Makefile, unknown ring, make or docker missing), never for a failing ring.
func Run(ctx context.Context, p *project.Project, ring string, opts Options) (passed bool, err error) {
	return run(ctx, osExecutor{}, p, ring, opts)
}

// command is one process to run.
type command struct {
	name   string
	args   []string
	dir    string
	env    []string // nil inherits the process environment
	stdout io.Writer
	stderr io.Writer
}

// executor runs a command and returns its exit code; the error is for a command that could not run or was cancelled.
type executor interface {
	run(ctx context.Context, c command) (exitCode int, err error)
}

// osExecutor runs commands as child processes; a cancelled one gets SIGTERM, then SIGKILL after cancelGrace.
type osExecutor struct{}

func (osExecutor) run(ctx context.Context, c command) (int, error) {
	cmd := exec.CommandContext(ctx, c.name, c.args...)
	cmd.Dir = c.dir
	cmd.Env = c.env
	cmd.Stdout = c.stdout
	cmd.Stderr = c.stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = cancelGrace
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return -1, ctxErr
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return -1, fmt.Errorf("run %s: %w", c.name, err)
	}
	return 0, nil
}

// site is where make runs: in the toolbox image when one is named, otherwise on this machine.
type site struct {
	image   string // empty: this machine
	root    string
	environ []string
}

// command returns the command that runs make with the arguments at the site.
func (s site) command(makeArgs []string, stdout, stderr io.Writer) command {
	c := command{name: "make", args: makeArgs, dir: s.root, env: s.environ, stdout: stdout, stderr: stderr}
	if s.image != "" {
		c.name = "docker"
		c.args = dockerArgs(s.image, s.root, os.Getuid(), os.Getgid(), s.environ, makeArgs)
	}
	return c
}

// explain turns a failure to start make into an error that says what is missing and what to do.
func (s site) explain(err error) error {
	switch {
	case s.image != "" && errors.Is(err, exec.ErrNotFound):
		return fmt.Errorf("the clone names the toolbox image %s (git config pawl.toolbox) but docker is not available (%w); install docker or run with --no-toolbox", s.image, err)
	case errors.Is(err, exec.ErrNotFound):
		return fmt.Errorf("make is not available (%w); install GNU make", err)
	}
	return fmt.Errorf("run make: %w", err)
}

func run(ctx context.Context, ex executor, p *project.Project, ring string, opts Options) (bool, error) {
	if err := checkTarget(p.Root, ring); err != nil {
		return false, err
	}
	s := site{root: p.Root, environ: opts.Environ}
	if !opts.NoToolbox {
		s.image = toolboxImage(p.Root)
	}
	stdout, stderr := orDiscard(opts.Stdout), orDiscard(opts.Stderr)
	if opts.Verbose {
		return runVerbose(ctx, ex, s, p.Root, ring, stdout, stderr)
	}
	return runQuiet(ctx, ex, s, p.Root, ring, stdout)
}

func runVerbose(ctx context.Context, ex executor, s site, root, ring string, stdout, stderr io.Writer) (bool, error) {
	// a copy of both streams, for the failed targets in the summary
	errorLines := &lockedBuffer{}
	start := time.Now()
	code, err := ex.run(ctx, s.command([]string{"-k", ring}, io.MultiWriter(stdout, errorLines), io.MultiWriter(stderr, errorLines)))
	if err != nil {
		return false, finishErr(ctx, s, err)
	}
	d := time.Since(start)
	record(root, ring, d)
	if code == 0 {
		writeLines(stdout, []string{passLine(ring, d, -1)})
		return true, nil
	}
	writeLines(stdout, []string{failLine(ring, d, failedTargets(splitLines(errorLines.String())))})
	return false, nil
}

// lockedBuffer is a buffer two copying goroutines (make's stdout and stderr) write to.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func runQuiet(ctx context.Context, ex executor, s site, root, ring string, stdout io.Writer) (bool, error) {
	isTraced, err := detectTrace(ctx, ex, s)
	if err != nil {
		return false, err
	}
	args := []string{"-k", ring}
	if isTraced {
		args = []string{"-k", "--trace", ring}
	}
	var out bytes.Buffer
	start := time.Now()
	// one writer for both streams: os/exec gives make a single pipe, so the order make wrote in is kept
	code, err := ex.run(ctx, s.command(args, &out, &out))
	if err != nil {
		return false, finishErr(ctx, s, err)
	}
	d := time.Since(start)
	record(root, ring, d)
	lines := splitLines(out.String())
	if code == 0 {
		writeLines(stdout, []string{passLine(ring, d, stepCount(lines, isTraced))})
		return true, nil
	}
	writeLines(stdout, failReport(ring, d, lines, isTraced))
	return false, nil
}

// finishErr returns the context's error for a cancelled run and the explained start failure otherwise.
func finishErr(ctx context.Context, s site, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return s.explain(err)
}

// detectTrace asks the make at the site for its version; only a missing make or docker is an error.
func detectTrace(ctx context.Context, ex executor, s site) (bool, error) {
	var out bytes.Buffer
	_, err := ex.run(ctx, s.command([]string{"--version"}, &out, io.Discard))
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, exec.ErrNotFound) {
			return false, finishErr(ctx, s, err)
		}
		return false, nil
	}
	return hasTrace(out.String()), nil
}

// record stores the duration of a ring for `pawl budget`; other targets have no budget and are not recorded.
func record(root, target string, d time.Duration) {
	switch target {
	case "precommit", "check", "nightly":
		_ = rings.Record(root, target, d) // a timing that cannot be written must not fail the run
	}
}

// targetName is what a make target pawl runs may look like: no flag, no variable assignment, no shell syntax.
var targetName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]*$`)

// checkTarget fails for a name that is not a plain make target and for a project whose Makefile lacks it.
func checkTarget(root, ring string) error {
	if !targetName.MatchString(ring) {
		return fmt.Errorf("%q is not a make target name", ring)
	}
	m, ok, err := makefile.Read(root)
	if err != nil {
		return fmt.Errorf("read the Makefile: %w", err)
	}
	if !ok {
		return fmt.Errorf("no make target %s: %s has no Makefile; run `pawl init`", ring, root)
	}
	if !m.Has(ring) {
		return fmt.Errorf("no make target %s in %s; run `pawl init`", ring, m.Path)
	}
	return nil
}

// toolboxImage returns the image the clone names in git config pawl.toolbox, or "" when none is named or git fails.
func toolboxImage(root string) string {
	out, err := runner.Exec{Timeout: gitTimeout}.Output(root, "git", "config", "--get", "pawl.toolbox")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

func writeLines(w io.Writer, lines []string) {
	for _, line := range lines {
		_, _ = fmt.Fprintln(w, line)
	}
}
