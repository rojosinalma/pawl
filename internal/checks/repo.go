package checks

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rojosinalma/pawl/internal/report"
	"github.com/rojosinalma/pawl/internal/stack"
)

// hookFile is the pre-commit hook pawl init writes; core.hooksPath points git at its directory.
const (
	hookDir  = ".githooks"
	hookFile = hookDir + "/pre-commit"
)

var (
	shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// makeOrPawlRe finds a make or pawl invocation at a command position in a shell line.
	makeOrPawlRe = regexp.MustCompile(`(^|[\s;&|(])(\S*/)?(make|pawl)(\s|$)|\$[({]?PAWL\b`)
	makeRe       = regexp.MustCompile(`(^|[\s;&|(])make(\s|$)`)
)

// checkHook verifies repo.hook: the hook exists, is executable, runs `make precommit`, and git uses its directory.
func (r *run) checkHook() error {
	const rule = "repo.hook"
	if !r.active(rule) {
		return nil
	}
	r.mark(rule)
	info, err := os.Stat(filepath.Join(r.p.Root, hookFile))
	if err != nil {
		r.add(report.Errorf(rule, hookFile, 0, "the pre-commit hook is missing").
			WithFix("run `pawl init`; it writes the hook that runs `make precommit`"))
	} else {
		if info.Mode().Perm()&0o111 == 0 {
			r.add(report.Errorf(rule, hookFile, 0, "the hook is not executable").WithFix("chmod +x " + hookFile))
		}
		if !strings.Contains(readOptional(r.p.Root, hookFile), "make precommit") {
			r.add(report.Errorf(rule, hookFile, 0, "the hook does not run `make precommit`").
				WithFix("add the line `make precommit` to " + hookFile))
		}
	}
	if r.p.Repo == nil {
		return nil
	}
	if got := r.p.Repo.HooksPath(); !r.isHookDir(got) {
		r.add(report.Errorf(rule, "", 0, "core.hooksPath is %q; git does not run %s", got, hookFile).
			WithFix("git config core.hooksPath " + hookDir))
	}
	return nil
}

// isHookDir reports whether a core.hooksPath value names the project's hook directory, relative or absolute.
func (r *run) isHookDir(got string) bool {
	if got == "" {
		return false
	}
	if filepath.IsAbs(got) {
		return filepath.Clean(got) == filepath.Join(r.p.Root, hookDir)
	}
	return path.Clean(filepath.ToSlash(got)) == hookDir
}

// checkPins verifies repo.pinned: every active stack with files in the tree records its toolchain version and
// tracks its lockfile.
func (r *run) checkPins() error {
	const rule = "repo.pinned"
	if !r.active(rule) {
		return nil
	}
	r.mark(rule)
	tracked, err := r.trackedSet()
	if err != nil {
		return err
	}
	for _, a := range r.p.StackAdapters() {
		if !a.Detect(r.p.Files) {
			continue // the stack is listed but has no file here yet: nothing to pin
		}
		for _, pin := range a.Pins() {
			r.checkPin(a.Name(), pin, tracked)
		}
	}
	return nil
}

func (r *run) checkPin(stackName string, pin stack.Pin, tracked func(string) bool) {
	const rule = "repo.pinned"
	if len(pin.Files) > 0 && !anyExists(r.p.Root, pin.Files) {
		r.add(report.Errorf(rule, pin.Files[0], 0, "%s: no pin for the %s", stackName, pin.What).
			WithFix("create one of: " + strings.Join(pin.Files, ", ")))
	}
	if len(pin.Locks) == 0 {
		return
	}
	for _, l := range pin.Locks {
		if tracked(l) {
			return
		}
	}
	r.add(report.Errorf(rule, pin.Locks[0], 0, "%s: no lockfile is tracked (one of %s)", stackName, strings.Join(pin.Locks, ", ")).
		WithFix("generate the lockfile with the stack's package manager and `git add` it"))
}

// trackedSet returns a predicate for "the file is tracked"; outside git a file that exists counts.
func (r *run) trackedSet() (func(string) bool, error) {
	if r.p.Repo == nil {
		return func(f string) bool { return anyExists(r.p.Root, []string{f}) }, nil
	}
	files, err := r.p.Repo.Tracked()
	if err != nil {
		return nil, fmt.Errorf("tracked files: %w", err)
	}
	set := map[string]bool{}
	for _, f := range files {
		set[f] = true
	}
	return func(f string) bool { return set[f] }, nil
}

func anyExists(root string, files []string) bool {
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			return true
		}
	}
	return false
}

// checkWorkflows verifies repo.ci-shape on every GitHub Actions workflow.
func (r *run) checkWorkflows() error {
	const rule = "repo.ci-shape"
	if !r.active(rule) {
		return nil
	}
	r.mark(rule)
	for _, f := range stack.Filter(r.p.Files, []string{".github/workflows/*.yml", ".github/workflows/*.yaml"}) {
		raw, err := os.ReadFile(filepath.Join(r.p.Root, f))
		if err != nil {
			return fmt.Errorf("workflow %s: %w", f, err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			r.add(report.Errorf(rule, f, 0, "cannot parse the workflow: %v", err).WithFix("fix the YAML; `actionlint` points at the error"))
			continue
		}
		r.add(workflowFindings(f, &doc)...)
	}
	return nil
}

// workflowFindings checks one parsed workflow: pinned actions everywhere, and make or pawl in every run step of the
// ring workflows (check, nightly).
func workflowFindings(file string, doc *yaml.Node) []report.Finding {
	root := mapping(doc)
	if root == nil {
		return nil
	}
	ring := isRingWorkflow(file, scalar(field(root, "name")))
	var out []report.Finding
	runsMake := false
	for _, job := range mappingValues(field(root, "jobs")) {
		out = append(out, usesFinding(file, field(job, "uses"))...)
		for _, step := range seq(field(job, "steps")) {
			out = append(out, usesFinding(file, field(step, "uses"))...)
			if run := field(step, "run"); run != nil && ring {
				runsMake = runsMake || makeRe.MatchString(run.Value)
				out = append(out, runFinding(file, run)...)
			}
		}
	}
	if ring && !runsMake {
		out = append(out, report.Errorf("repo.ci-shape", file, 0, "the ring workflow runs no make target").
			WithFix("add a step `run: make check` (or `make nightly`)"))
	}
	return out
}

// runFinding warns about a run step of a ring workflow that bypasses make and pawl: CI then runs something no
// machine runs with `make check`.
func runFinding(file string, run *node) []report.Finding {
	if makeOrPawlRe.MatchString(run.Value) {
		return nil
	}
	return []report.Finding{report.Warnf("repo.ci-shape", file, run.Line, "run step `%s` calls neither make nor pawl", firstLine(run.Value)).
		WithFix("move the command into a make target so CI and machines run the same thing")}
}

func isRingWorkflow(file, name string) bool {
	base := strings.TrimSuffix(strings.TrimSuffix(path.Base(file), ".yml"), ".yaml")
	n := strings.ToLower(strings.TrimSpace(name))
	return base == "check" || base == "nightly" || n == "check" || n == "nightly"
}

// usesFinding reports an action reference not pinned to a full commit SHA.
func usesFinding(file string, n *node) []report.Finding {
	if n == nil {
		return nil
	}
	ref := strings.TrimSpace(n.Value)
	switch {
	case ref == "", strings.HasPrefix(ref, "./"):
		return nil
	case strings.HasPrefix(ref, "docker://"):
		if strings.Contains(ref, "@sha256:") {
			return nil
		}
		return []report.Finding{report.Errorf("repo.ci-shape", file, n.Line, "image `%s` is not pinned to a digest", ref).
			WithFix("pin it: docker://image@sha256:<digest>")}
	}
	at := strings.LastIndex(ref, "@")
	if at >= 0 && shaRe.MatchString(ref[at+1:]) {
		return nil
	}
	return []report.Finding{report.Errorf("repo.ci-shape", file, n.Line, "action `%s` is not pinned to a commit SHA", ref).
		WithFix("pin it: `uses: owner/repo@<40-hex commit sha> # vX.Y.Z` (zizmor and Renovate keep it current)")}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " ..."
	}
	return s
}

// node is the YAML node type the workflow walker uses; every helper below accepts nil.
type node = yaml.Node

func mapping(doc *node) *node {
	if doc != nil && doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc == nil || doc.Kind != yaml.MappingNode {
		return nil
	}
	return doc
}

// field returns the value node of key in a mapping, or nil.
func field(m *node, key string) *node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func seq(n *node) []*node {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	return n.Content
}

func scalar(n *node) string {
	if n == nil {
		return ""
	}
	return n.Value
}

func mappingValues(n *node) []*node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []*node
	for i := 1; i < len(n.Content); i += 2 {
		out = append(out, n.Content[i])
	}
	return out
}
