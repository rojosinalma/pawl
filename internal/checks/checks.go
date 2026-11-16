// Package checks is the catalog verification behind `pawl verify`: the manifest against the catalog, the make
// targets, the adapters' configuration verifiers, pawl's own text checks, and the ratchet against the base commit.
package checks

import (
	"fmt"

	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/ratchet"
	"github.com/rojosinalma/pawl/internal/report"
	"github.com/rojosinalma/pawl/internal/stack"
)

// Ring names accepted by Options.Ring.
const (
	ringPrecommit = "precommit"
	ringCheck     = "check"
	ringNightly   = "nightly"
)

// Options select what a run covers.
type Options struct {
	// Ring is "" or "check" for the full verification, "precommit" for the fast checks on staged files, "nightly"
	// for the full verification plus the nightly-only rules.
	Ring string
	// Only limits the run to these rule ids (used by make recipes such as `pawl verify --only escape.duplication`).
	Only []string
}

// run is the state of one verification: the project, the ring, the rule filter and the report being built.
type run struct {
	p       *project.Project
	ring    string
	only    map[string]bool
	rep     *report.Report
	checked map[string]bool
	// hits caches the suppression scan so the reason check and the ratchet read every file once.
	hits     []hit
	hitsDone bool
}

func newRun(p *project.Project, ring string, only []string) *run {
	r := &run{p: p, ring: ring, rep: &report.Report{Tool: "pawl", Ring: ring}, checked: map[string]bool{}}
	if len(only) > 0 {
		r.only = map[string]bool{}
		for _, id := range only {
			r.only[id] = true
		}
	}
	return r
}

func normalizeRing(ring string) (string, error) {
	switch ring {
	case "", ringCheck:
		return ringCheck, nil
	case ringPrecommit, ringNightly:
		return ring, nil
	}
	return "", fmt.Errorf("unknown ring %q (precommit, check, nightly)", ring)
}

// Run verifies the project and returns the findings. The error is for failures to run at all (unreadable files,
// git errors), never for findings.
func Run(p *project.Project, opts Options) (*report.Report, error) {
	ring, err := normalizeRing(opts.Ring)
	if err != nil {
		return nil, err
	}
	r := newRun(p, ring, opts.Only)
	steps := []func() error{r.checkManifest}
	if ring == ringPrecommit {
		steps = append(steps, r.precommitText)
	} else {
		steps = append(steps, r.checkTargets, r.checkVerifiers, r.checkHook, r.checkPins, r.checkWorkflows,
			r.fullText, r.checkPrinciples, r.checkContracts, r.checkPRSize, r.checkSpeed, r.checkRatchet)
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	r.dropUnwanted()
	return r.rep, nil
}

// Measure computes the current ratchet metrics (suppressions, oversized files, duplicated blocks, TODOs without a
// link, quarantined tests) without comparing them; `pawl baseline` records the result.
func Measure(p *project.Project) (*ratchet.Current, error) {
	return newRun(p, ringCheck, nil).measure()
}

// want reports whether the Only filter admits the rule.
func (r *run) want(rule string) bool { return r.only == nil || r.only[rule] }

// waived reports whether the manifest waives the rule; a waived rule's pawl verify does not run.
func (r *run) waived(rule string) bool { return r.p.Manifest.Rules[rule].Waived != "" }

// enforced reports whether the manifest backs the rule with a step.
func (r *run) enforced(rule string) bool { return r.p.Manifest.Rules[rule].Enforced() }

// active reports whether a pawl-owned check for the rule runs: admitted by Only and not waived.
func (r *run) active(rule string) bool { return r.want(rule) && !r.waived(rule) }

// mark records that the rule was evaluated, once.
func (r *run) mark(rule string) {
	if r.checked[rule] {
		return
	}
	r.checked[rule] = true
	r.rep.Checked = append(r.rep.Checked, rule)
}

func (r *run) add(f ...report.Finding) { r.rep.Add(f...) }

// dropUnwanted removes findings of rules outside the Only filter; checks that report under several rules (the
// manifest validation, the ratchet) are cheaper to run whole and filter afterwards.
func (r *run) dropUnwanted() {
	if r.only == nil {
		return
	}
	kept := r.rep.Findings[:0]
	for _, f := range r.rep.Findings {
		if r.only[f.RuleID] {
			kept = append(kept, f)
		}
	}
	r.rep.Findings = kept
}

// checkManifest reports every manifest problem as an error on pawl.toml, at the rule's table when it has one.
func (r *run) checkManifest() error {
	raw := readOptional(r.p.Root, manifest.FileName)
	for _, rule := range r.p.Catalog.IDs() {
		if r.want(rule) {
			r.mark(rule)
		}
	}
	for _, pr := range r.p.Manifest.Validate(r.p.Catalog) {
		rule := pr.RuleID
		fix := "edit " + manifest.FileName
		if rule == "" {
			rule = "repo.targets"
		} else {
			fix = fmt.Sprintf("edit [rules.%q] in %s: exactly one of step (a make target) or waived (a reason)", rule, manifest.FileName)
		}
		line := lineOf(raw, fmt.Sprintf("[rules.%q]", pr.RuleID))
		r.add(report.Errorf(rule, manifest.FileName, line, "%s", pr.Message).WithFix(fix))
	}
	return nil
}

// stagedFiles returns the staged files the manifest does not exclude; ok is false outside a repository.
func (r *run) stagedFiles() ([]string, bool, error) {
	if r.p.Repo == nil {
		return nil, false, nil
	}
	staged, err := r.p.Repo.Staged()
	if err != nil {
		return nil, false, fmt.Errorf("staged files: %w", err)
	}
	var out []string
	for _, f := range staged {
		if !stack.Excluded(f, r.p.Manifest.Project.Exclude) {
			out = append(out, f)
		}
	}
	return out, true, nil
}

// precommitText runs the fast text checks (forbidden names, suppression reasons) on the staged files only.
func (r *run) precommitText() error {
	files, ok, err := r.stagedFiles()
	if err != nil {
		return err
	}
	if !ok {
		for _, rule := range []string{"repo.private", "escape.suppressions"} {
			if r.active(rule) {
				r.rep.Skip(rule, "not a git repository: no staged files")
			}
		}
		return nil
	}
	if err := r.checkPrivate(files); err != nil {
		return err
	}
	return r.checkSuppressionReasons(files)
}

// fullText runs the text checks of the full rings on every candidate file.
func (r *run) fullText() error {
	if err := r.checkPrivate(r.p.Files); err != nil {
		return err
	}
	return r.checkSuppressionReasons(r.p.Files)
}
