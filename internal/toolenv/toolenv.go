// Package toolenv finds mise and builds the environment in which the project's pinned tools are on PATH, so the
// rings and `pawl doctor` work without the user activating mise in a shell.
package toolenv

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rojosinalma/pawl/internal/config"
	"github.com/rojosinalma/pawl/internal/runner"
)

// binPathsTimeout bounds `mise bin-paths`; it only reads mise's state, so it is quick when it works at all.
const binPathsTimeout = 30 * time.Second

// ConfigFiles are the files that make a directory a mise project.
var ConfigFiles = []string{"mise.toml", ".mise.toml", ".tool-versions"}

// HasConfig reports whether root has one of the mise configuration files.
func HasConfig(root string) bool {
	for _, f := range ConfigFiles {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			return true
		}
	}
	return false
}

// LocalBin returns the directory pawl installs mise into: ~/.local/bin.
func LocalBin() (string, error) {
	home, err := config.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

// MisePath returns the mise binary to use: the one on PATH, else the one pawl installed (~/.local/bin/mise), else "".
func MisePath() string {
	if path, err := exec.LookPath("mise"); err == nil {
		return path
	}
	dir, err := LocalBin()
	if err != nil {
		return ""
	}
	path := filepath.Join(dir, "mise")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return path
	}
	return ""
}

// MiseVars are the variables every mise call of pawl gets: the project's config is trusted without a prompt and
// mise answers its own questions with yes.
func MiseVars(root string) []string {
	return []string{"MISE_TRUSTED_CONFIG_PATHS=" + root, "MISE_YES=1"}
}

// Environ returns the process environment with the directories of the project's mise tools (`mise bin-paths`, run
// in root) in front of PATH, and ~/.local/bin, where pawl installs mise and make, at its end when that directory
// exists and PATH lacks it. Without mise or without a mise.toml or .tool-versions in root the tool directories are
// left out.
func Environ(ctx context.Context, root string) ([]string, error) {
	base := config.Environ()
	if dir, err := LocalBin(); err == nil {
		base = appendPath(base, dir)
	}
	r := Exec{Env: append(append([]string(nil), base...), MiseVars(root)...), Timeout: binPathsTimeout}
	return environ(ctx, r, MisePath(), base, root), nil
}

// environ asks mise for the tool directories of root and prepends the existing ones to the PATH of base. Any
// failure leaves base as it is: the tools are then missing, and doctor says which.
func environ(ctx context.Context, r runner.Runner, mise string, base []string, root string) []string {
	if mise == "" || !HasConfig(root) || ctx.Err() != nil {
		return base
	}
	out, err := r.Output(root, mise, "bin-paths")
	if err != nil {
		return base
	}
	var dirs []string
	for _, line := range strings.Split(string(out), "\n") {
		dir := strings.TrimSpace(line)
		if info, err := os.Stat(dir); dir != "" && err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return PrependPath(base, dirs)
}

// PrependPath returns a copy of env whose PATH starts with dirs.
func PrependPath(env, dirs []string) []string {
	out := append([]string(nil), env...)
	if len(dirs) == 0 {
		return out
	}
	front := strings.Join(dirs, string(filepath.ListSeparator))
	for i, kv := range out {
		if old, ok := strings.CutPrefix(kv, "PATH="); ok {
			if old != "" {
				front += string(filepath.ListSeparator) + old
			}
			out[i] = "PATH=" + front
			return out
		}
	}
	return append(out, "PATH="+front)
}

// appendPath returns env with dir at the end of its PATH, unchanged when dir is not a directory or already on PATH.
func appendPath(env []string, dir string) []string {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return env
	}
	path := pathOf(env)
	if slices.Contains(filepath.SplitList(path), dir) {
		return env
	}
	if path != "" {
		dir = path + string(filepath.ListSeparator) + dir
	}
	out := append([]string(nil), env...)
	for i := len(out) - 1; i >= 0; i-- {
		if strings.HasPrefix(out[i], "PATH=") {
			out[i] = "PATH=" + dir
			return out
		}
	}
	return append(out, "PATH="+dir)
}
