// Package scaffold implements `pawl init`: it writes what a project lacks and never touches what it has.
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rojosinalma/pawl/internal/makefile"
	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/project"
)

// Options of an init run.
type Options struct {
	Stacks []string // override detection
	DryRun bool     // report, write nothing
	// Version is the running pawl's version; the workflows install this release ("" or "dev": the latest one).
	Version string
}

// Result lists what init did.
type Result struct {
	Stacks  []string
	Created []string // files written
	Kept    []string // files that existed and were left alone
	Merged  []string // files init appended to (the Makefile, .gitignore, AGENTS.md)
	Waived  []string // rule ids waived by default: the decisions left to the project
	Hints   []string // commands the project still has to run (install the added dependencies)
}

// Init detects the stacks, then writes the manifest, the tool configurations, the make targets and the rest of the
// scaffold (see writeExtras). Existing files are kept; the Makefile only gains the targets it lacks.
func Init(root string, opts Options) (*Result, error) {
	if manifest.Exists(root) {
		return nil, fmt.Errorf("%s exists; edit it, or delete it to start over", manifest.FileName)
	}
	p, err := project.Detect(root, opts.Stacks)
	if err != nil {
		return nil, err
	}
	res := &Result{Stacks: p.Manifest.Stacks}
	w := &writer{root: p.Root, dry: opts.DryRun, res: res, version: opts.Version}

	p.Manifest.Rules = p.DefaultRules()
	res.Waived = p.Manifest.Waived(p.Catalog)
	fillProject(p)

	if err := writeConfigs(p, w); err != nil {
		return nil, err
	}
	if err := writeMakefile(p, w); err != nil {
		return nil, err
	}
	if err := writeExtras(p, w); err != nil {
		return nil, err
	}

	// the manifest last, so a failed init leaves no manifest behind and can be run again
	if !opts.DryRun {
		if err := manifest.Write(p.Root, p.Manifest, p.Catalog); err != nil {
			return nil, err
		}
	}
	res.Created = append(res.Created, manifest.FileName)
	return res, nil
}

// writeConfigs writes the tool configurations of every active adapter that are missing.
func writeConfigs(p *project.Project, w *writer) error {
	for _, a := range p.Adapters {
		for _, c := range a.Configs(p.Params) {
			mode := c.Mode
			if mode == 0 {
				mode = 0o644
			}
			if err := w.create(c.Path, c.Content, mode); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeMakefile writes the Makefile whole, or appends the targets an existing one lacks.
func writeMakefile(p *project.Project, w *writer) error {
	generated := makefile.Generate(p.Adapters, p.Params, p.Manifest.Budgets.DiffCoverage, p.Manifest.Base)
	existing, ok, err := makefile.Read(p.Root)
	if err != nil {
		return err
	}
	if !ok {
		return w.create("Makefile", generated, 0o644)
	}
	merged := makefile.Merge(existing.Content, generated)
	if merged == existing.Content {
		w.res.Kept = append(w.res.Kept, existing.Path)
		return nil
	}
	return w.overwrite(existing.Path, merged, 0o644, true)
}

// fillProject sets the project facts the adapters detected (own module) and conventional defaults.
func fillProject(p *project.Project) {
	if p.Manifest.Project.OwnModule == "" {
		for _, key := range []string{"module", "package", "crate"} {
			if v := p.Params.Facts[key]; v != "" {
				p.Manifest.Project.OwnModule = v
				p.Params.OwnModule = v
				break
			}
		}
	}
	if p.Manifest.Project.TestDirs == nil {
		for _, d := range []string{"test", "tests", "spec"} {
			if st, err := os.Stat(filepath.Join(p.Root, d)); err == nil && st.IsDir() {
				p.Manifest.Project.TestDirs = append(p.Manifest.Project.TestDirs, d)
			}
		}
		p.Params.TestDirs = p.Manifest.Project.TestDirs
	}
}

// writer creates files under the root and records what happened.
type writer struct {
	root    string
	dry     bool
	res     *Result
	version string // the running pawl's version, for the workflows
}

// create writes the file when it does not exist.
func (w *writer) create(rel, content string, mode os.FileMode) error {
	path := filepath.Join(w.root, rel)
	if _, err := os.Stat(path); err == nil {
		w.res.Kept = append(w.res.Kept, rel)
		return nil
	}
	w.res.Created = append(w.res.Created, rel)
	if w.dry {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

// overwrite replaces the file; merged marks an append to an existing file.
func (w *writer) overwrite(rel, content string, mode os.FileMode, merged bool) error {
	if merged {
		w.res.Merged = append(w.res.Merged, rel)
	} else {
		w.res.Created = append(w.res.Created, rel)
	}
	if w.dry {
		return nil
	}
	path := filepath.Join(w.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}
