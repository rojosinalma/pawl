// Package gitx wraps the git commands pawl needs: the tree, the changes against a base, the hook path.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo is a checkout.
type Repo struct {
	Root string
}

// Open finds the repository root for dir; it returns an error when dir is not inside a git work tree.
func Open(dir string) (*Repo, error) {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git repository", dir)
	}
	return &Repo{Root: strings.TrimSpace(out)}, nil
}

// IsRepo reports whether dir is inside a git work tree.
func IsRepo(dir string) bool {
	_, err := Open(dir)
	return err == nil
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}

// Files returns the paths git tracks or does not ignore, relative to the root, sorted as git lists them.
func (r *Repo) Files() ([]string, error) {
	out, err := run(r.Root, "ls-files", "-co", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	return splitZ(out, r.Root, true), nil
}

// Tracked returns the tracked paths only.
func (r *Repo) Tracked() ([]string, error) {
	out, err := run(r.Root, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	return splitZ(out, r.Root, true), nil
}

// Staged returns the paths staged for commit (added, copied, modified, renamed).
func (r *Repo) Staged() ([]string, error) {
	out, err := run(r.Root, "diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z")
	if err != nil {
		return nil, err
	}
	return splitZ(out, r.Root, true), nil
}

// MergeBase returns the merge base of HEAD and ref, or "" when ref is unknown.
func (r *Repo) MergeBase(ref string) string {
	out, err := run(r.Root, "merge-base", "HEAD", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Changed returns the paths that differ between base and the working tree (including staged and unstaged changes).
func (r *Repo) Changed(base string) ([]string, error) {
	out, err := run(r.Root, "diff", "--name-only", "--diff-filter=ACMR", "-z", base)
	if err != nil {
		return nil, err
	}
	return splitZ(out, r.Root, true), nil
}

// ChangedLines returns, per file, the count of added lines between base and the working tree.
func (r *Repo) ChangedLines(base string) (added, deleted int, err error) {
	out, err := run(r.Root, "diff", "--numstat", base)
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] == "-" {
			continue
		}
		var a, d int
		fmt.Sscanf(fields[0], "%d", &a)
		fmt.Sscanf(fields[1], "%d", &d)
		added += a
		deleted += d
	}
	return added, deleted, nil
}

// Show returns the content of path at ref; ok is false when the file does not exist there.
func (r *Repo) Show(ref, path string) (content []byte, ok bool) {
	out, err := run(r.Root, "show", ref+":"+filepath.ToSlash(path))
	if err != nil {
		return nil, false
	}
	return []byte(out), true
}

// HooksPath returns the configured core.hooksPath, relative to the root when it was set relative; "" when unset.
func (r *Repo) HooksPath() string {
	out, err := run(r.Root, "config", "--get", "core.hooksPath")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// SetHooksPath sets core.hooksPath for this repository.
func (r *Repo) SetHooksPath(path string) error {
	_, err := run(r.Root, "config", "core.hooksPath", path)
	return err
}

// Branch returns the current branch name, or "" when detached.
func (r *Repo) Branch() string {
	out, err := run(r.Root, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// RemoteURL returns the URL of origin, or "".
func (r *Repo) RemoteURL() string {
	out, err := run(r.Root, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Log returns the subjects of the commits between base and HEAD.
func (r *Repo) Log(base string) ([]string, error) {
	out, err := run(r.Root, "log", "--format=%s", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(out), "\n"), nil
}

func splitZ(out, root string, onlyExisting bool) []string {
	var files []string
	for _, p := range strings.Split(out, "\x00") {
		if p == "" {
			continue
		}
		if onlyExisting {
			if _, err := os.Stat(filepath.Join(root, p)); err != nil {
				continue
			}
		}
		files = append(files, p)
	}
	return files
}
