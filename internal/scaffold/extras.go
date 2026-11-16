package scaffold

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rojosinalma/pawl/internal/checks"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/ratchet"
)

//go:embed templates/*
var templates embed.FS

// hookPath is the git hook init writes; hooksDir is what core.hooksPath points at.
const (
	hooksDir = ".githooks"
	hookPath = hooksDir + "/pre-commit"
)

// ignoreLines are the .gitignore entries pawl needs: local reports and the agent's local additions.
var ignoreLines = []string{".pawl/reports/", "AGENTS.local.md"}

// writeExtras writes the rest of the scaffold after the configurations and the Makefile: the git hook and
// core.hooksPath, mise.toml with the pinned tools, the workflows, the AGENTS.md block, the .gitignore lines, the
// stack's own additions (Gemfile group, pyproject dependency group) and the first baseline.
func writeExtras(p *project.Project, w *writer) error {
	steps := []func(*project.Project, *writer) error{
		writeHook,
		writeMise,
		writeAdditions,
		writeWorkflows,
		writeAgents,
		writeGitignore,
		writeBaseline,
	}
	for _, step := range steps {
		if err := step(p, w); err != nil {
			return err
		}
	}
	return nil
}

// writeHook writes the pre-commit hook and points core.hooksPath at it.
func writeHook(p *project.Project, w *writer) error {
	raw, err := templates.ReadFile("templates/pre-commit.sh")
	if err != nil {
		return fmt.Errorf("hook template: %w", err)
	}
	if err := w.create(hookPath, string(raw), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", hookPath, err)
	}
	if p.Repo == nil || w.dry {
		return nil
	}
	// A project that already points core.hooksPath somewhere else (husky, lefthook) keeps its setup: init never
	// replaces what exists. The hint says how to switch.
	if current := p.Repo.HooksPath(); current != "" && current != hooksDir {
		w.res.Hints = append(w.res.Hints, fmt.Sprintf("core.hooksPath is %q; run `git config core.hooksPath %s`, or call `pawl run precommit` from your own hook", current, hooksDir))
		return nil
	}
	if err := p.Repo.SetHooksPath(hooksDir); err != nil {
		return fmt.Errorf("set core.hooksPath: %w", err)
	}
	return nil
}

// writeGitignore appends the lines pawl needs that .gitignore lacks, or creates the file.
func writeGitignore(_ *project.Project, w *writer) error {
	const name = ".gitignore"
	existing, ok, err := w.read(name)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, line := range strings.Split(existing, "\n") {
		present[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, l := range ignoreLines {
		if !present[l] {
			missing = append(missing, l)
		}
	}
	if !ok {
		return w.create(name, "# pawl\n"+strings.Join(missing, "\n")+"\n", 0o644)
	}
	if len(missing) == 0 {
		w.res.Kept = append(w.res.Kept, name)
		return nil
	}
	return w.overwrite(name, withNewline(existing)+"\n# pawl\n"+strings.Join(missing, "\n")+"\n", 0o644, true)
}

// writeBaseline records the first ratchet values; an unmeasurable tree starts from an empty baseline.
func writeBaseline(p *project.Project, w *writer) error {
	if _, ok, err := w.read(ratchet.FileName); err != nil {
		return err
	} else if ok {
		w.res.Kept = append(w.res.Kept, ratchet.FileName)
		return nil
	}
	w.res.Created = append(w.res.Created, ratchet.FileName)
	if w.dry {
		return nil
	}
	cur, err := checks.Measure(p)
	if err != nil {
		cur = ratchet.NewCurrent()
	}
	if err := ratchet.Write(p.Root, ratchet.FromCurrent(cur, nil)); err != nil {
		return fmt.Errorf("write %s: %w", ratchet.FileName, err)
	}
	return nil
}

// read returns a file under the root; ok is false when it does not exist.
func (w *writer) read(rel string) (string, bool, error) {
	raw, err := os.ReadFile(filepath.Join(w.root, rel))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read %s: %w", rel, err)
	}
	return string(raw), true, nil
}

// exists reports whether a file exists under the root.
func (w *writer) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(w.root, rel))
	return err == nil
}

// appendTo adds text to an existing file, or creates it with the header first; both are recorded.
func (w *writer) appendTo(rel, header, text string) error {
	existing, ok, err := w.read(rel)
	if err != nil {
		return err
	}
	if !ok {
		return w.create(rel, header+text, 0o644)
	}
	return w.overwrite(rel, withNewline(existing)+text, 0o644, true)
}

// writeFile writes rel under root through os.Root, so a name that would escape the project fails instead of
// writing outside it.
func writeFile(root, rel, content string, mode os.FileMode) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := r.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return err
	}
	return r.WriteFile(rel, []byte(content), mode)
}

// withNewline returns s ending with a newline (an empty s stays empty).
func withNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
