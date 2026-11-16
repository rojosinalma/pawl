// Package ratchet keeps the shrink-only baseline: counts that may fall but never rise without a recorded reason.
package ratchet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/rojosinalma/pawl/internal/gitx"
)

// FileName is the baseline path, relative to the project root.
const FileName = ".pawl/baseline.json"

// Baseline is the recorded state.
type Baseline struct {
	Version   int                 `json:"version"`
	Updated   string              `json:"updated"`
	Metrics   map[string]int      `json:"metrics"`
	Offenders map[string][]string `json:"offenders,omitempty"`
	Growth    []Growth            `json:"growth,omitempty"`
}

// Growth records the one way up: a deliberate raise with its reason.
type Growth struct {
	Date   string         `json:"date"`
	Reason string         `json:"reason"`
	From   map[string]int `json:"from"`
	To     map[string]int `json:"to"`
}

// Current is what a run measured.
type Current struct {
	Metrics   map[string]int
	Offenders map[string][]string
}

// NewCurrent returns an empty measurement.
func NewCurrent() *Current {
	return &Current{Metrics: map[string]int{}, Offenders: map[string][]string{}}
}

// Set records a metric with its offenders.
func (c *Current) Set(metric string, offenders []string) {
	sort.Strings(offenders)
	c.Metrics[metric] = len(offenders)
	c.Offenders[metric] = offenders
}

// Count records a bare count.
func (c *Current) Count(metric string, n int) { c.Metrics[metric] = n }

// Load reads the baseline from the working tree; ok is false when there is none.
func Load(root string) (*Baseline, bool, error) {
	raw, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return parse(raw)
}

// LoadAt reads the baseline as committed at ref; ok is false when it did not exist there.
func LoadAt(repo *gitx.Repo, ref string) (*Baseline, bool, error) {
	if repo == nil || ref == "" {
		return nil, false, nil
	}
	raw, ok := repo.Show(ref, FileName)
	if !ok {
		return nil, false, nil
	}
	return parse(raw)
}

func parse(raw []byte) (*Baseline, bool, error) {
	var b Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, false, fmt.Errorf("%s: %w", FileName, err)
	}
	if b.Metrics == nil {
		b.Metrics = map[string]int{}
	}
	return &b, true, nil
}

// Write stores the baseline.
func Write(root string, b *Baseline) error {
	b.Version = 1
	b.Updated = time.Now().UTC().Format("2006-01-02")
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(FileName)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, FileName), append(raw, '\n'), 0o644)
}

// FromCurrent builds a baseline from a measurement, keeping the growth history of prev.
func FromCurrent(cur *Current, prev *Baseline) *Baseline {
	b := &Baseline{Metrics: map[string]int{}, Offenders: map[string][]string{}}
	for k, v := range cur.Metrics {
		b.Metrics[k] = v
	}
	for k, v := range cur.Offenders {
		if len(v) > 0 {
			b.Offenders[k] = append([]string(nil), v...)
		}
	}
	if prev != nil {
		b.Growth = prev.Growth
	}
	return b
}

// Delta is one metric's movement.
type Delta struct {
	Metric       string
	Baseline     int
	Current      int
	NewOffenders []string
}

// Regressions returns the metrics whose count rose, with the offenders the baseline did not list. A count that
// stays the same is never a regression, whatever moved: renaming a file must not need a recorded reason.
func Regressions(base *Baseline, cur *Current) []Delta {
	var out []Delta
	for _, k := range sortedKeys(cur.Metrics) {
		was, known := base.Metrics[k]
		now := cur.Metrics[k]
		// a metric the baseline never recorded is new: Stale reports it, it is not a regression
		if known && now > was {
			out = append(out, Delta{Metric: k, Baseline: was, Current: now, NewOffenders: diff(cur.Offenders[k], base.Offenders[k])})
		}
	}
	return out
}

// Stale returns the metrics whose baseline is above the current value (improvements not yet recorded) and the
// metrics the current run measures that the baseline lacks.
func Stale(base *Baseline, cur *Current) []Delta {
	var out []Delta
	for _, k := range sortedKeys(cur.Metrics) {
		was, known := base.Metrics[k]
		now := cur.Metrics[k]
		if !known || now < was {
			out = append(out, Delta{Metric: k, Baseline: was, Current: now})
		}
	}
	return out
}

func diff(cur, base []string) []string {
	set := map[string]bool{}
	for _, b := range base {
		set[b] = true
	}
	var out []string
	for _, c := range cur {
		if !set[c] {
			out = append(out, c)
		}
	}
	return out
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
