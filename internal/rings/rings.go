// Package rings records how long each ring takes and compares the medians with the budgets in the manifest.
package rings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/report"
)

const (
	ruleID       = "speed.rings"
	keepPerRing  = 50
	sampleSize   = 10
	minSamples   = 3
	cmdTimeout   = 30 * time.Second
	timingsPath  = ".pawl/reports/timings.json"
	timingsVer   = 1
	ringPrecheck = "precommit"
)

// knownRings are the rings pawl knows, in report order.
func knownRings() []string { return []string{"precommit", "check", "nightly"} }

func isKnown(ring string) bool {
	for _, r := range knownRings() {
		if r == ring {
			return true
		}
	}
	return false
}

// runner runs an outbound command and returns its standard output.
type runner interface {
	Output(dir, name string, args ...string) ([]byte, error)
}

type execRunner struct{ timeout time.Duration }

// Output runs the command in dir under the runner's timeout and returns its standard output; a failure carries
// the command line and its standard error.
func (e execRunner) Output(dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

// entry is one recorded run.
type entry struct {
	Ring    string  `json:"ring"`
	Seconds float64 `json:"seconds"`
	At      string  `json:"at"`
}

// timings is the local timings file.
type timings struct {
	Version int                `json:"version"`
	Runs    map[string][]entry `json:"runs"`
}

// Record stores one run of a ring (the git hook calls `pawl record precommit <seconds>`). Local timings live in
// .pawl/reports/timings.json, which is ignored by git; at most the last 50 per ring are kept.
func Record(root, ring string, d time.Duration) error {
	return record(root, ring, d, time.Now())
}

func record(root, ring string, d time.Duration, now time.Time) error {
	if !isKnown(ring) {
		return fmt.Errorf("unknown ring %q (known: %s)", ring, strings.Join(knownRings(), ", "))
	}
	path := filepath.Join(root, timingsPath)
	t, err := readTimings(path)
	if err != nil {
		return err
	}
	runs := append(t.Runs[ring], entry{Ring: ring, Seconds: d.Seconds(), At: now.UTC().Format(time.RFC3339)})
	if len(runs) > keepPerRing {
		runs = runs[len(runs)-keepPerRing:]
	}
	t.Runs[ring] = runs
	return writeAtomic(path, t)
}

func readTimings(path string) (*timings, error) {
	t := &timings{Version: timingsVer, Runs: map[string][]entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read timings: %w", err)
	}
	if err := json.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if t.Runs == nil {
		t.Runs = map[string][]entry{}
	}
	t.Version = timingsVer
	return t, nil
}

func writeAtomic(path string, t *timings) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("encode timings: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".timings-*.json")
	if err != nil {
		return fmt.Errorf("create temp timings: %w", err)
	}
	_, err = tmp.Write(append(data, '\n'))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Budget compares the median of the last ten runs of every ring with its target (warning) and limit (error).
// The precommit ring reads the local timings; the check and nightly rings read the last ten completed runs on the
// default branch of the workflows named check and nightly through `gh run list`, when gh and a remote exist.
func Budget(p *project.Project) (*report.Report, error) {
	return budget(p, execRunner{timeout: cmdTimeout})
}

func budget(p *project.Project, run runner) (*report.Report, error) {
	r := &report.Report{}
	ci := &ciSource{p: p, run: run}
	for _, name := range ringOrder(p) {
		if !isKnown(name) {
			r.Skip(ruleID, name+": not a ring pawl knows")
			continue
		}
		target, limit, err := p.Manifest.Rings[name].Durations()
		if err != nil {
			return nil, fmt.Errorf("rings.%s: %w", name, err)
		}
		samples, why := durations(p, ci, name)
		if why != "" {
			r.Skip(ruleID, name+": "+why)
			continue
		}
		if len(samples) < minSamples {
			r.Skip(ruleID, fmt.Sprintf("%s: %d runs, need %d", name, len(samples), minSamples))
			continue
		}
		r.Checked = append(r.Checked, ruleID)
		if f, ok := judge(name, p.Manifest.Rings[name].Target, p.Manifest.Rings[name].Limit, median(samples), target, limit); ok {
			r.Add(f)
		}
	}
	return r, nil
}

// ringOrder returns the manifest's rings: the known ones in their order, then any others sorted.
func ringOrder(p *project.Project) []string {
	var out, extra []string
	for _, name := range knownRings() {
		if _, ok := p.Manifest.Rings[name]; ok {
			out = append(out, name)
		}
	}
	for name := range p.Manifest.Rings {
		if !isKnown(name) {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// durations returns the last ten samples of a ring, or the reason they cannot be read.
func durations(p *project.Project, ci *ciSource, ring string) ([]time.Duration, string) {
	if ring == ringPrecheck {
		t, err := readTimings(filepath.Join(p.Root, timingsPath))
		if err != nil {
			return nil, err.Error()
		}
		var out []time.Duration
		for _, e := range lastN(t.Runs[ring], sampleSize) {
			out = append(out, time.Duration(e.Seconds*float64(time.Second)))
		}
		return out, ""
	}
	return ci.durations(ring)
}

func lastN[T any](s []T, n int) []T {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

func median(d []time.Duration) time.Duration {
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func judge(ring, targetS, limitS string, m, target, limit time.Duration) (report.Finding, bool) {
	shown := m.Round(100 * time.Millisecond).String()
	switch {
	case m > limit:
		return report.Errorf(ruleID, "", 0, "%s median %s exceeds its limit %s (target %s)", ring, shown, limitS, targetS).
			WithFix(fixHint(ring)), true
	case m > target:
		return report.Warnf(ruleID, "", 0, "%s median %s is over its target %s (limit %s)", ring, shown, targetS, limitS).
			WithFix(fixHint(ring)), true
	}
	return report.Finding{}, false
}

func fixHint(ring string) string {
	if ring == ringPrecheck {
		return "move slow work out of `make precommit` into the check ring, or raise rings.precommit in pawl.toml with a reason"
	}
	return fmt.Sprintf("speed up the %s workflow (affected tests, caching, parallel jobs), or raise rings.%s in pawl.toml with a reason", ring, ring)
}
