package checks

import (
	"bytes"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/rojosinalma/pawl/internal/config"
	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/ratchet"
	"github.com/rojosinalma/pawl/internal/report"
	"github.com/rojosinalma/pawl/internal/stack"
)

// workTypeEnv overrides the work type the branch name implies, for branches that do not follow the prefixes.
const workTypeEnv = "PAWL_WORK_TYPE"

// branchPrefixes map the conventional branch prefixes to the budgets.pr_lines keys.
var branchPrefixes = []struct{ prefix, workType string }{
	{"feature/", "feature"}, {"feat/", "feature"},
	{"bugfix/", "bugfix"}, {"fix/", "bugfix"},
	{"hotfix/", "hotfix"},
	{"refactor/", "refactor"},
}

// checkPrinciples verifies tests.principles: every declared principle has a suite that exists.
func (r *run) checkPrinciples() error {
	const rule = "tests.principles"
	if !r.active(rule) || len(r.p.Manifest.Principles) == 0 {
		return nil
	}
	r.mark(rule)
	raw := readOptional(r.p.Root, manifest.FileName)
	for _, pr := range r.p.Manifest.Principles {
		if pr.Suite != "" && stack.HasAny(r.p.Files, []string{pr.Suite}) {
			continue
		}
		r.add(report.Errorf(rule, manifest.FileName, lineOf(raw, strconv.Quote(pr.Suite)), "principle %q: suite %q matches no file", pr.Name, pr.Suite).
			WithFix("write the suite, or fix its glob in the [[principles]] entry"))
	}
	return nil
}

// checkContracts verifies drift.contracts: every contract file exists and a changed one carries a version bump.
func (r *run) checkContracts() error {
	const rule = "drift.contracts"
	contracts := r.p.Manifest.Contracts
	if !r.active(rule) || len(contracts) == 0 {
		return nil
	}
	r.mark(rule)
	for _, c := range contracts {
		if !anyExists(r.p.Root, []string{c.Path}) {
			r.add(report.Errorf(rule, manifest.FileName, 0, "contract %s: %s does not exist", c.Name, c.Path).
				WithFix("fix the path in the [[contracts]] entry, or remove the entry"))
		}
	}
	base := r.p.BaseRef()
	if base == "" {
		r.rep.Skip(rule, "no base ref: cannot tell which contracts changed")
		return nil
	}
	changed, err := r.p.Repo.Changed(base)
	if err != nil {
		return fmt.Errorf("changed files against %s: %w", base, err)
	}
	old, err := r.baseManifest(base)
	if err != nil {
		return err
	}
	for _, c := range contracts {
		if was, ok := contractVersion(old, c.Name); ok && was == c.Version && touches(changed, c.Path) {
			r.add(report.Errorf(rule, manifest.FileName, 0, "contract %s changed without a version bump (still %s)", c.Name, c.Version).
				WithFix(fmt.Sprintf("raise version of the [[contracts]] entry %q in %s", c.Name, manifest.FileName)))
		}
	}
	return nil
}

// baseManifest decodes pawl.toml as committed at base; nil when it did not exist there.
func (r *run) baseManifest(base string) (*manifest.Manifest, error) {
	raw, ok := r.p.Repo.Show(base, manifest.FileName)
	if !ok {
		return nil, nil
	}
	var m manifest.Manifest
	if _, err := toml.Decode(string(raw), &m); err != nil {
		return nil, fmt.Errorf("%s at %s: %w", manifest.FileName, base, err)
	}
	return &m, nil
}

func contractVersion(m *manifest.Manifest, name string) (string, bool) {
	if m == nil {
		return "", false
	}
	for _, c := range m.Contracts {
		if c.Name == name {
			return c.Version, true
		}
	}
	return "", false
}

// touches reports whether a changed file is the contract or lies under it (a contract may be a directory).
func touches(changed []string, contract string) bool {
	contract = strings.TrimSuffix(path.Clean(filepath.ToSlash(contract)), "/")
	for _, f := range changed {
		if f == contract || strings.HasPrefix(f, contract+"/") {
			return true
		}
	}
	return false
}

// checkPRSize verifies drift.pr-size: the change against the base stays within the work type's line budget.
func (r *run) checkPRSize() error {
	const rule = "drift.pr-size"
	if !r.active(rule) {
		return nil
	}
	r.mark(rule)
	base := r.p.BaseRef()
	if base == "" {
		r.rep.Skip(rule, "no base ref: nothing to measure the change against")
		return nil
	}
	wt := r.workType()
	budget, ok := r.p.Manifest.Budgets.PRLines[wt]
	if !ok || budget <= 0 {
		r.rep.Skip(rule, fmt.Sprintf("no budgets.pr_lines.%s in %s", wt, manifest.FileName))
		return nil
	}
	n, err := changedLines(r.p.Root, base)
	if err != nil {
		return err
	}
	if n > budget {
		r.add(report.Errorf(rule, "", 0, "the change is %d lines (added + deleted), over the %s budget of %d (budgets.pr_lines.%s)", n, wt, budget, wt).
			WithFix(fmt.Sprintf("split it into smaller pull requests; set %s when the branch name does not say the work type", workTypeEnv)))
	}
	return nil
}

// workType is the budget key: the environment override, then the branch prefix, then feature.
func (r *run) workType() string {
	if wt := config.WorkType(); wt != "" {
		return wt
	}
	if r.p.Repo != nil {
		branch := r.p.Repo.Branch()
		for _, bp := range branchPrefixes {
			if strings.HasPrefix(branch, bp.prefix) {
				return bp.workType
			}
		}
	}
	return "feature"
}

// changedLines counts added and deleted lines against base, without lockfiles and the baseline: both change by
// machine, and counting them would punish a dependency bump or a ratchet update.
func changedLines(root, base string) (int, error) {
	cmd := exec.Command("git", "diff", "--numstat", base, "--")
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("git diff --numstat %s: %s", base, strings.TrimSpace(stderr.String()))
	}
	total := 0
	for _, line := range strings.Split(stdout.String(), "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 3 || countExcluded(fields[2]) {
			continue
		}
		added, errA := strconv.Atoi(fields[0])
		deleted, errD := strconv.Atoi(fields[1])
		if errA != nil || errD != nil {
			continue // binary files report "-"
		}
		total += added + deleted
	}
	return total, nil
}

func countExcluded(file string) bool {
	// a rename reads "old => new" or "dir/{old => new}"; the new name decides
	if i := strings.LastIndex(file, " => "); i >= 0 {
		file = strings.TrimSuffix(file[i+4:], "}")
	}
	base := path.Base(file)
	return file == ratchet.FileName || lockfiles[base] || strings.HasSuffix(base, ".lock")
}
