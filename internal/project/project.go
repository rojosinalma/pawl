// Package project loads everything a pawl command needs about the repository it runs in: the catalog, the
// manifest, the active adapters, their parameters and the candidate files.
package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rojosinalma/pawl/internal/catalog"
	"github.com/rojosinalma/pawl/internal/gitx"
	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/stack"

	// The adapters register themselves.
	_ "github.com/rojosinalma/pawl/internal/stack/common"
	_ "github.com/rojosinalma/pawl/internal/stack/golang"
	_ "github.com/rojosinalma/pawl/internal/stack/python"
	_ "github.com/rojosinalma/pawl/internal/stack/ruby"
	_ "github.com/rojosinalma/pawl/internal/stack/rust"
	_ "github.com/rojosinalma/pawl/internal/stack/shell"
	_ "github.com/rojosinalma/pawl/internal/stack/typescript"
)

// CommonStack is the adapter every project has; it is never listed in the manifest's stacks.
const CommonStack = "common"

// Project is the loaded state.
type Project struct {
	Root     string
	Repo     *gitx.Repo // nil outside a git repository
	Catalog  *catalog.Catalog
	Manifest *manifest.Manifest
	Adapters []stack.Adapter // the common adapter first, then the manifest's stacks
	Params   stack.Params
	Files    []string // tracked or not ignored, minus the manifest's excludes, relative to Root
}

// excludedDirs are skipped when the root is not a git repository.
var excludedDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".venv": true, "target": true, "dist": true, ".worktrees": true, ".agents": true}

// Load reads the project at root; it needs a manifest.
func Load(root string) (*Project, error) {
	m, err := manifest.Require(root)
	if err != nil {
		return nil, err
	}
	return build(root, m)
}

// Detect builds a project without a manifest: default budgets and the stacks found in the tree (or the given ones).
func Detect(root string, stacks []string) (*Project, error) {
	m := manifest.Defaults()
	p, err := build(root, &m)
	if err != nil {
		return nil, err
	}
	if len(stacks) == 0 {
		for _, name := range stack.Detect(p.Files) {
			if name != CommonStack {
				stacks = append(stacks, name)
			}
		}
	}
	m.Stacks = stacks
	return build(root, &m)
}

func build(root string, m *manifest.Manifest) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cat, err := catalog.Load()
	if err != nil {
		return nil, err
	}
	p := &Project{Root: abs, Catalog: cat, Manifest: m}
	if repo, err := gitx.Open(abs); err == nil && sameDir(repo.Root, abs) {
		p.Repo = repo
	}
	files, err := listFiles(abs, p.Repo)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if !stack.Excluded(f, m.Project.Exclude) {
			p.Files = append(p.Files, f)
		}
	}
	common, _ := stack.Get(CommonStack)
	p.Adapters = []stack.Adapter{common}
	for _, name := range m.Stacks {
		a, ok := stack.Get(name)
		if !ok || name == CommonStack {
			return nil, fmt.Errorf("%s: unknown stack %q (known: %s)", manifest.FileName, name, strings.Join(StackNames(), ", "))
		}
		p.Adapters = append(p.Adapters, a)
	}
	p.Params = stack.Params{
		ConfigModule: m.Project.ConfigModule,
		OwnModule:    m.Project.OwnModule,
		FileLines:    m.Budgets.FileLines,
		Complexity:   m.Budgets.Complexity,
		DiffCoverage: m.Budgets.DiffCoverage,
		TestDirs:     m.Project.TestDirs,
		Exclude:      m.Project.Exclude,
		Facts:        map[string]string{},
	}
	for _, a := range p.Adapters {
		for k, v := range a.Facts(abs) {
			p.Params.Facts[k] = v
		}
	}
	return p, nil
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

func listFiles(root string, repo *gitx.Repo) ([]string, error) {
	if repo != nil {
		return repo.Files()
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if excludedDirs[d.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

// StackNames returns the stacks a manifest may list.
func StackNames() []string {
	var out []string
	for _, a := range stack.All() {
		if a.Name() != CommonStack {
			out = append(out, a.Name())
		}
	}
	return out
}

// StackAdapters returns the adapters without the common one.
func (p *Project) StackAdapters() []stack.Adapter {
	var out []stack.Adapter
	for _, a := range p.Adapters {
		if a.Name() != CommonStack {
			out = append(out, a)
		}
	}
	return out
}

// SourceFiles returns the candidate files that are source code of an active stack.
func (p *Project) SourceFiles() []string {
	var globs []string
	for _, a := range p.Adapters {
		globs = append(globs, a.SourceGlobs()...)
	}
	return stack.Filter(p.Files, globs)
}

// IsTest reports whether the path is a test file of an active stack or under a declared test directory.
func (p *Project) IsTest(path string) bool {
	for _, a := range p.Adapters {
		if stack.Match(path, a.TestGlobs()) {
			return true
		}
	}
	return stack.Excluded(path, p.Manifest.Project.TestDirs)
}

// BaseRef returns the commit the ratchet and the change scope compare against: the merge base of HEAD and the
// manifest's base. It is "" outside a repository or when the base is unknown (a fresh clone without the remote ref).
func (p *Project) BaseRef() string {
	if p.Repo == nil {
		return ""
	}
	if env := os.Getenv("PAWL_BASE"); env != "" {
		if mb := p.Repo.MergeBase(env); mb != "" {
			return mb
		}
	}
	return p.Repo.MergeBase(p.Manifest.Base)
}

// DefaultRules merges what each active adapter does about every catalog rule into manifest entries: a rule is
// enforced when any adapter enforces it (the tools are joined), the stacks that waive it become gaps, and a rule
// every adapter waives is waived with the reasons joined. Review rules are waived with their guidance.
func (p *Project) DefaultRules() map[string]manifest.Rule {
	out := map[string]manifest.Rule{}
	per := make([]map[string]stack.Enforcement, len(p.Adapters))
	for i, a := range p.Adapters {
		per[i] = a.Rules(p.Params)
	}
	for _, r := range p.Catalog.Rules {
		if r.IsReview() {
			out[r.ID] = manifest.Rule{Waived: "no tool reaches this; it is on the review checklist in AGENTS.md"}
			continue
		}
		var rule manifest.Rule
		var reasons []string
		for i, a := range p.Adapters {
			e, ok := per[i][r.ID]
			if !ok {
				continue
			}
			if e.Step == "" {
				reasons = append(reasons, e.Waived)
				continue
			}
			if rule.Step == "" {
				rule.Step = e.Step
			}
			for _, v := range e.Via {
				if !contains(rule.Via, v) {
					rule.Via = append(rule.Via, v)
				}
			}
			_ = a
		}
		switch {
		case rule.Step != "":
			rule.Gaps = reasons
		case len(reasons) > 0:
			rule.Waived = strings.Join(reasons, "; ")
		default:
			rule.Waived = "no stack of this project has a tool for it yet"
		}
		out[r.ID] = rule
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
