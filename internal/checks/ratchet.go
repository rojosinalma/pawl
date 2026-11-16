package checks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rojosinalma/pawl/internal/ratchet"
	"github.com/rojosinalma/pawl/internal/report"
)

const (
	jscpdReport = ".pawl/reports/jscpd-report.json"
	// maxListed bounds the offenders a regression names; the rest are counted.
	maxListed = 10
)

// metric is one ratchet value: its name in the baseline, the catalog rule it belongs to, and how to measure it.
type metric struct {
	name string
	rule string
	// enforcedOnly measures the metric only when a step enforces the rule; otherwise only a waiver turns it off.
	enforcedOnly bool
	measure      func(r *run) (offenders []string, ok bool, err error)
}

var metrics = []metric{
	{name: "code.file-size", rule: "code.file-size", measure: func(r *run) ([]string, bool, error) {
		o, err := r.oversized()
		return o, err == nil, err
	}},
	{name: "escape.suppressions", rule: "escape.suppressions", measure: measureSuppressions},
	{name: "code.todo-links", rule: "code.debug-leftovers", enforcedOnly: true, measure: func(r *run) ([]string, bool, error) {
		o, err := r.unlinkedTodos()
		return o, err == nil, err
	}},
	{name: "escape.duplication", rule: "escape.duplication", enforcedOnly: true, measure: measureDuplication},
	{name: "tests.quarantine", rule: "tests.runner", measure: func(r *run) ([]string, bool, error) {
		names := []string{}
		for _, q := range r.p.Manifest.Quarantine {
			names = append(names, q.Name)
		}
		return names, true, nil
	}},
}

func ruleOf(metricName string) string {
	for _, m := range metrics {
		if m.name == metricName {
			return m.rule
		}
	}
	return metricName
}

func (r *run) metricOn(m metric) bool {
	if !r.want(m.rule) {
		return false
	}
	if m.enforcedOnly {
		return r.enforced(m.rule)
	}
	return !r.waived(m.rule)
}

// measure computes every ratchet metric the manifest and the Only filter leave on; a metric whose input is absent
// is omitted.
func (r *run) measure() (*ratchet.Current, error) {
	cur := ratchet.NewCurrent()
	for _, m := range metrics {
		if !r.metricOn(m) {
			continue
		}
		r.mark(m.rule)
		offenders, ok, err := m.measure(r)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m.name, err)
		}
		if ok {
			cur.Set(m.name, offenders)
		}
	}
	return cur, nil
}

func measureSuppressions(r *run) ([]string, bool, error) {
	hits, err := r.suppressionHits(r.p.Files)
	if err != nil {
		return nil, false, err
	}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.offender())
	}
	return out, true, nil
}

// jscpdFile is the part of the jscpd JSON reporter's output pawl reads.
type jscpdFile struct {
	Name  string `json:"name"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type jscpdOutput struct {
	Duplicates []struct {
		FirstFile  jscpdFile `json:"firstFile"`
		SecondFile jscpdFile `json:"secondFile"`
		Lines      int       `json:"lines"`
	} `json:"duplicates"`
}

// measureDuplication reads the jscpd report `make lint` writes. Without it the check ring skips the metric (lint
// may not have run in this checkout) and the nightly ring, which runs lint first, fails.
func measureDuplication(r *run) ([]string, bool, error) {
	raw, err := os.ReadFile(filepath.Join(r.p.Root, jscpdReport))
	if errors.Is(err, fs.ErrNotExist) {
		if r.ring == ringNightly {
			r.add(report.Errorf("escape.duplication", jscpdReport, 0, "the jscpd report is missing").WithFix("run `make lint` (jscpd) before `pawl check --ring nightly`"))
		} else {
			r.rep.Skip("escape.duplication", "run `make lint` (jscpd) first")
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", jscpdReport, err)
	}
	var out jscpdOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false, fmt.Errorf("%s: %w", jscpdReport, err)
	}
	offenders := make([]string, 0, len(out.Duplicates))
	for _, d := range out.Duplicates {
		offenders = append(offenders, r.span(d.FirstFile)+" ~ "+r.span(d.SecondFile))
	}
	return offenders, true, nil
}

// span renders the file of a jscpd location relative to the root, so offenders compare across checkouts.
func (r *run) span(f jscpdFile) string {
	name := filepath.ToSlash(f.Name)
	if filepath.IsAbs(f.Name) {
		if rel, err := filepath.Rel(r.p.Root, f.Name); err == nil {
			name = filepath.ToSlash(rel)
		}
	}
	return strings.TrimPrefix(name, "./") // the file, not the lines: the baseline must survive edits above the block
}

// ratchetRule is the rule a missing baseline is reported under: escape.suppressions, or the first ratchet rule the
// Only filter admits.
func (r *run) ratchetRule() (string, bool) {
	if r.want("escape.suppressions") {
		return "escape.suppressions", true
	}
	for _, m := range metrics {
		if r.want(m.rule) {
			return m.rule, true
		}
	}
	return "", false
}

// checkRatchet compares the measurement with the baseline at the base commit and in the working tree.
func (r *run) checkRatchet() error {
	rule, ok := r.ratchetRule()
	if !ok {
		return nil
	}
	cur, err := r.measure()
	if err != nil {
		return err
	}
	working, ok, err := ratchet.Load(r.p.Root)
	if err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	if !ok {
		r.add(report.Errorf(rule, ratchet.FileName, 0, "no baseline: run `pawl baseline`").WithFix("pawl baseline"))
		return nil
	}
	reported := map[string]bool{}
	if base := r.p.BaseRef(); base != "" {
		committed, ok, err := ratchet.LoadAt(r.p.Repo, base)
		if err != nil {
			return fmt.Errorf("baseline at %s: %w", base, err)
		}
		if ok {
			grown := grownMetrics(committed, working)
			r.addRegressions(exceptGrown(ratchet.Regressions(committed, cur), grown), "the base commit", reported)
			r.checkRaised(committed, working, cur, grown)
		}
	}
	r.addRegressions(ratchet.Regressions(working, cur), "the baseline", reported)
	for _, d := range ratchet.Stale(working, cur) {
		recorded := fmt.Sprintf("the baseline records %d", d.Baseline)
		if _, known := working.Metrics[d.Metric]; !known {
			recorded = "the baseline does not record it"
		}
		r.add(report.Errorf(ruleOf(d.Metric), ratchet.FileName, 0, "%s is %d, %s; stale baseline: run `pawl baseline` to record the improvement", d.Metric, d.Current, recorded).
			WithFix("pawl baseline"))
	}
	return nil
}

// addRegressions reports each risen metric once, even when both the base and the working baseline show it.
func (r *run) addRegressions(deltas []ratchet.Delta, against string, reported map[string]bool) {
	for _, d := range deltas {
		if reported[d.Metric] {
			continue
		}
		reported[d.Metric] = true
		msg := fmt.Sprintf("%s rose from %d to %d against %s", d.Metric, d.Baseline, d.Current, against)
		if len(d.NewOffenders) > 0 {
			msg += "; new: " + listOffenders(d.NewOffenders)
		}
		r.add(report.Errorf(ruleOf(d.Metric), "", 0, "%s", msg).
			WithFix("fix the new offenders, or record a deliberate raise with `pawl baseline --grow \"<reason>\"`"))
	}
}

func listOffenders(list []string) string {
	if len(list) <= maxListed {
		return strings.Join(list, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(list[:maxListed], ", "), len(list)-maxListed)
}

// grownMetrics returns the metrics a growth entry added since the committed baseline raises: `pawl baseline --grow`
// is the one way up, so those metrics are compared with the working baseline only.
func grownMetrics(committed, working *ratchet.Baseline) map[string]bool {
	grown := map[string]bool{}
	if len(working.Growth) <= len(committed.Growth) {
		return grown
	}
	for _, g := range working.Growth[len(committed.Growth):] {
		for k := range g.To {
			grown[k] = true
		}
	}
	return grown
}

func exceptGrown(deltas []ratchet.Delta, grown map[string]bool) []ratchet.Delta {
	var out []ratchet.Delta
	for _, d := range deltas {
		if !grown[d.Metric] {
			out = append(out, d)
		}
	}
	return out
}

// checkRaised fails a working baseline above the committed one unless a new growth entry records the reason.
func (r *run) checkRaised(committed, working *ratchet.Baseline, cur *ratchet.Current, grown map[string]bool) {
	keys := make([]string, 0, len(cur.Metrics))
	for k := range cur.Metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		was, inBase := committed.Metrics[k]
		now, inWorking := working.Metrics[k]
		if inBase && inWorking && now > was && !grown[k] {
			r.add(report.Errorf(ruleOf(k), ratchet.FileName, 0, "%s: baseline raised from %d to %d without `pawl baseline --grow`", k, was, now).
				WithFix("restore the committed baseline, or raise it with `pawl baseline --grow \"<reason>\"`"))
		}
	}
}
