// Package install implements `pawl install`: mise itself when the machine lacks it, the tools pinned in mise.toml,
// then the steps each stack does with its own package manager.
package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/rojosinalma/pawl/internal/config"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/scaffold"
	"github.com/rojosinalma/pawl/internal/toolenv"
)

const (
	miseInstallTimeout = 30 * time.Minute
	stackStepTimeout   = 15 * time.Minute
)

// Options of one install.
type Options struct {
	// SkipMise leaves a machine without mise alone: only the stack's own steps run.
	SkipMise bool
	Stdout   io.Writer
	Stderr   io.Writer
}

// Step is one thing install did or tried.
type Step struct {
	Name    string   // what it is for ("mise", "tools of mise.toml", "go tools", "gems")
	Command []string // the command that ran; empty for the mise download
	Skipped string   // why it did not run; "" when it ran
	Err     error    // the failure; nil when it passed or was skipped
	Note    string   // something the user should know about a step that passed ("add ~/.local/bin to PATH")
}

// Result lists the steps in the order they ran.
type Result struct {
	Steps []Step
}

// Failed reports whether a step failed.
func (r *Result) Failed() bool {
	for _, s := range r.Steps {
		if s.Err != nil {
			return true
		}
	}
	return false
}

// command is one child process of install.
type command struct {
	dir     string
	env     []string
	argv    []string
	timeout time.Duration
}

// installer holds what install reaches outside the process, so the tests replace it.
type installer struct {
	baseURL  string            // the release download base; the GitHub releases URL
	binDir   string            // where mise goes; "" for ~/.local/bin
	goos     string            // the platform whose mise build is downloaded
	goarch   string            //
	sums     map[string]string // SHA-256 by platform ("linux-x64")
	client   *http.Client
	pathVar  string   // PATH, to tell whether binDir is on it
	baseEnv  []string // the environment of `mise install`
	misePath func() string
	environ  func(ctx context.Context, root string) ([]string, error)
	run      func(ctx context.Context, c command, stdout, stderr io.Writer) error
}

func newInstaller() *installer {
	return &installer{
		baseURL:  miseReleases,
		goos:     runtime.GOOS,
		goarch:   runtime.GOARCH,
		sums:     pinnedSHA256(),
		client:   &http.Client{Timeout: downloadTimeout},
		pathVar:  config.Path(),
		baseEnv:  config.Environ(),
		misePath: toolenv.MisePath,
		environ:  toolenv.Environ,
		run: func(ctx context.Context, c command, stdout, stderr io.Writer) error {
			return toolenv.Exec{Env: c.env, Timeout: c.timeout}.Run(ctx, c.dir, stdout, stderr, c.argv[0], c.argv[1:]...)
		},
	}
}

// Run installs what the project's rings need. The error is for failures to start at all; a failing step is in the
// result.
func Run(ctx context.Context, p *project.Project, opts Options) (*Result, error) {
	if p == nil {
		return nil, errors.New("install: no project")
	}
	return newInstaller().install(ctx, p, opts), nil
}

// install runs mise, the tools of the mise configuration and the stack's steps, in that order.
func (in *installer) install(ctx context.Context, p *project.Project, opts Options) *Result {
	stdout, stderr := opts.Stdout, opts.Stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	hasConfig := toolenv.HasConfig(p.Root)
	mise, miseStep := in.miseStep(ctx, hasConfig, opts.SkipMise)
	res := &Result{Steps: []Step{miseStep}}
	res.Steps = append(res.Steps, in.toolsStep(ctx, p.Root, mise, hasConfig, miseStep.Err != nil, stdout, stderr))
	res.Steps = append(res.Steps, in.stackSteps(ctx, p, stdout, stderr)...)
	return res
}

// miseStep finds or installs mise and returns its path ("" when there is none).
func (in *installer) miseStep(ctx context.Context, hasConfig, skip bool) (string, Step) {
	step := Step{Name: "mise"}
	existing := in.misePath()
	switch {
	case skip:
		step.Skipped = "skipped on request"
		return existing, step
	case !hasConfig:
		step.Skipped = "the project has no mise.toml, .mise.toml or .tool-versions"
		return existing, step
	case existing != "":
		step.Skipped = "already at " + existing
		return existing, step
	}
	dir := in.binDir
	if dir == "" {
		var err error
		if dir, err = toolenv.LocalBin(); err != nil {
			step.Err = fmt.Errorf("where to install mise: %w", err)
			return "", step
		}
	}
	path, err := in.installMise(ctx, dir)
	if err != nil {
		step.Err = err
		return "", step
	}
	if !slices.Contains(filepath.SplitList(in.pathVar), dir) {
		step.Note = fmt.Sprintf("installed %s; add %s to PATH to call mise yourself; pawl finds it there", path, dir)
	}
	return path, step
}

// toolsStep runs `mise install` for the project's mise configuration.
func (in *installer) toolsStep(ctx context.Context, root, mise string, hasConfig, miseFailed bool, stdout, stderr io.Writer) Step {
	step := Step{Name: "tools of mise.toml"}
	switch {
	case !hasConfig:
		step.Skipped = "no mise configuration"
		return step
	case miseFailed:
		step.Skipped = "mise did not install"
		return step
	case mise == "":
		step.Skipped = "mise is not installed"
		return step
	}
	step.Command = []string{mise, "install", "--yes"}
	env := append(append([]string(nil), in.baseEnv...), toolenv.MiseVars(root)...)
	step.Err = in.run(ctx, command{dir: root, env: env, argv: step.Command, timeout: miseInstallTimeout}, stdout, stderr)
	return step
}

// stackSteps runs each stack's setup command in the tool environment, so the mise tools (uv, node, bun) are found.
func (in *installer) stackSteps(ctx context.Context, p *project.Project, stdout, stderr io.Writer) []Step {
	setup := scaffold.SetupSteps(p)
	if len(setup) == 0 {
		return nil
	}
	env, err := in.environ(ctx, p.Root)
	if err != nil {
		env = in.baseEnv
	}
	var out []Step
	for _, s := range setup {
		step := Step{Name: s.Name, Command: s.Command}
		err := in.run(ctx, command{dir: p.Root, env: env, argv: s.Command, timeout: stackStepTimeout}, stdout, stderr)
		if errors.Is(err, exec.ErrNotFound) {
			err = fmt.Errorf("%s is not installed (neither on PATH nor among the mise tools): %w", s.Command[0], err)
		}
		step.Err = err
		out = append(out, step)
	}
	return out
}
