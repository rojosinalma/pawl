package affected

import (
	"encoding/json"
	"errors"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/runner"
)

// goPackage is the part of `go list -json` the closure needs.
type goPackage struct {
	ImportPath   string
	Dir          string
	Imports      []string
	TestImports  []string
	XTestImports []string
}

func goStrategy() strategy {
	return strategy{
		everything: func(*project.Project) string { return "./..." },
		nothing:    "-run ^$ ./...",
		owns:       func(f string) bool { return strings.HasSuffix(f, ".go") || testdataOwner(f) != "" },
		affected:   goAffected,
	}
}

func goAffected(p *project.Project, run runner.Runner, changed []string) (string, bool) {
	out, err := run.Output(p.Root, "go", "list", "-json", "./...")
	if err != nil {
		return "", false
	}
	pkgs, err := decodePackages(out)
	if err != nil {
		return "", false
	}
	byDir := map[string]goPackage{}
	for _, pkg := range pkgs {
		if rel, ok := relTo(p.Root, pkg.Dir); ok {
			byDir[rel] = pkg
		}
	}
	var seeds []string
	for _, f := range changed {
		if pkg, ok := ownerPackage(f, byDir); ok {
			seeds = append(seeds, pkg.ImportPath)
		}
	}
	if len(seeds) == 0 {
		return goStrategy().nothing, true
	}
	hit := closure(seeds, reverseImports(pkgs))
	var dirs []string
	for rel, pkg := range byDir {
		if hit[pkg.ImportPath] {
			dirs = append(dirs, goDirArg(rel))
		}
	}
	sort.Strings(dirs)
	return strings.Join(dirs, " "), true
}

func decodePackages(out []byte) ([]goPackage, error) {
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var pkgs []goPackage
	for {
		var pkg goPackage
		err := dec.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			return pkgs, nil
		}
		if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, pkg)
	}
}

// testdataOwner returns the directory that owns a testdata file ("." for a root testdata), or "".
func testdataOwner(f string) string {
	if strings.HasPrefix(f, "testdata/") {
		return "."
	}
	if i := strings.Index(f, "/testdata/"); i >= 0 {
		return f[:i]
	}
	return ""
}

// ownerPackage finds the package of a changed file: the nearest directory at or above it that is a package.
func ownerPackage(f string, byDir map[string]goPackage) (goPackage, bool) {
	dir := testdataOwner(f)
	if dir == "" {
		if !strings.HasSuffix(f, ".go") {
			return goPackage{}, false
		}
		dir = path.Dir(f)
	}
	for {
		if pkg, ok := byDir[dir]; ok {
			return pkg, true
		}
		if dir == "." {
			return goPackage{}, false
		}
		dir = path.Dir(dir)
	}
}

func reverseImports(pkgs []goPackage) map[string][]string {
	rev := map[string][]string{}
	for _, pkg := range pkgs {
		for _, lists := range [][]string{pkg.Imports, pkg.TestImports, pkg.XTestImports} {
			for _, imp := range lists {
				rev[imp] = append(rev[imp], pkg.ImportPath)
			}
		}
	}
	return rev
}

// closure returns the seeds and everything that reaches them through rev.
func closure(seeds []string, rev map[string][]string) map[string]bool {
	hit := map[string]bool{}
	queue := append([]string(nil), seeds...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if hit[cur] {
			continue
		}
		hit[cur] = true
		queue = append(queue, rev[cur]...)
	}
	return hit
}

func goDirArg(rel string) string {
	if rel == "." {
		return "."
	}
	return "./" + rel
}

// relTo returns target relative to root (slash-separated), trying the root with its symlinks resolved too.
func relTo(root, target string) (string, bool) {
	roots := []string{root}
	if resolved, err := filepath.EvalSymlinks(root); err == nil && resolved != root {
		roots = append(roots, resolved)
	}
	for _, r := range roots {
		rel, err := filepath.Rel(r, target)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return filepath.ToSlash(rel), true
		}
	}
	return "", false
}

// cargoMetadata is the part of `cargo metadata --no-deps` the closure needs.
type cargoMetadata struct {
	Packages []struct {
		Name         string `json:"name"`
		ManifestPath string `json:"manifest_path"`
		Dependencies []struct {
			Path string `json:"path"`
		} `json:"dependencies"`
	} `json:"packages"`
}

func rustStrategy() strategy {
	return strategy{
		everything: func(*project.Project) string { return "--workspace" },
		nothing:    "--workspace --no-run",
		owns:       func(f string) bool { return strings.HasSuffix(f, ".rs") },
		affected:   rustAffected,
	}
}

func rustAffected(p *project.Project, run runner.Runner, changed []string) (string, bool) {
	out, err := run.Output(p.Root, "cargo", "metadata", "--format-version", "1", "--no-deps")
	if err != nil {
		return "", false
	}
	var meta cargoMetadata
	if err := json.Unmarshal(out, &meta); err != nil || len(meta.Packages) < 2 {
		return "", false
	}
	dirs := memberDirs(p.Root, meta)
	var seeds []string
	for _, f := range changed {
		if !strings.HasSuffix(f, ".rs") {
			continue
		}
		name := crateOf(f, dirs)
		if name == "" {
			return "", false
		}
		seeds = append(seeds, name)
	}
	var args []string
	for name := range closure(seeds, reverseDeps(p.Root, meta, dirs)) {
		args = append(args, "-p "+name)
	}
	sort.Strings(args)
	return strings.Join(args, " "), true
}

// memberDirs maps each workspace member's directory, relative to the root, to its name.
func memberDirs(root string, meta cargoMetadata) map[string]string {
	dirs := map[string]string{}
	for _, pkg := range meta.Packages {
		if rel, ok := relTo(root, filepath.Dir(pkg.ManifestPath)); ok {
			dirs[rel] = pkg.Name
		}
	}
	return dirs
}

// reverseDeps maps a member to the members that depend on it by path.
func reverseDeps(root string, meta cargoMetadata, dirs map[string]string) map[string][]string {
	rev := map[string][]string{}
	for _, pkg := range meta.Packages {
		for _, dep := range pkg.Dependencies {
			if dep.Path == "" {
				continue
			}
			if rel, ok := relTo(root, dep.Path); ok && dirs[rel] != "" {
				rev[dirs[rel]] = append(rev[dirs[rel]], pkg.Name)
			}
		}
	}
	return rev
}

// crateOf returns the member whose directory is the longest prefix of f, or "".
func crateOf(f string, dirs map[string]string) string {
	best, name := -1, ""
	for dir, n := range dirs {
		if (dir == "." || strings.HasPrefix(f, dir+"/")) && len(dir) > best {
			best, name = len(dir), n
		}
	}
	return name
}
