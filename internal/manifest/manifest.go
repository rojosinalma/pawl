// Package manifest reads and writes pawl.toml: what a project does about every catalog rule.
package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/rojosinalma/pawl/internal/catalog"
)

// FileName is the manifest's name in a project root.
const FileName = "pawl.toml"

// Version is the manifest format version this build writes and reads.
const Version = 1

// Manifest is the parsed pawl.toml.
type Manifest struct {
	Version    int               `toml:"version"`
	Stacks     []string          `toml:"stacks"`
	Base       string            `toml:"base"`
	Project    Project           `toml:"project"`
	Budgets    Budgets           `toml:"budgets"`
	Rings      map[string]Ring   `toml:"rings"`
	Rules      map[string]Rule   `toml:"rules"`
	Principles []Principle       `toml:"principles"`
	Contracts  []Contract        `toml:"contracts"`
	Quarantine []QuarantinedTest `toml:"quarantine"`
}

// Project holds the project facts the generic checks and the ast-grep pack need.
type Project struct {
	ConfigModule string   `toml:"config_module"`
	OwnModule    string   `toml:"own_module"`
	TestDirs     []string `toml:"test_dirs"`
	Exclude      []string `toml:"exclude"`
}

// Budgets are the numeric limits of the catalog rules.
type Budgets struct {
	FileLines    int            `toml:"file_lines"`
	Complexity   int            `toml:"complexity"`
	DiffCoverage int            `toml:"diff_coverage"`
	PRLines      map[string]int `toml:"pr_lines"`
}

// Ring is a time budget: the median over the target warns, over the limit fails.
type Ring struct {
	Target string `toml:"target"`
	Limit  string `toml:"limit"`
}

// Durations parses the ring's budget strings.
func (r Ring) Durations() (target, limit time.Duration, err error) {
	target, err = time.ParseDuration(r.Target)
	if err != nil {
		return 0, 0, fmt.Errorf("ring target %q: %w", r.Target, err)
	}
	limit, err = time.ParseDuration(r.Limit)
	if err != nil {
		return 0, 0, fmt.Errorf("ring limit %q: %w", r.Limit, err)
	}
	if limit < target {
		return 0, 0, fmt.Errorf("ring limit %s is below its target %s", r.Limit, r.Target)
	}
	return target, limit, nil
}

// Rule is what the project does about one catalog rule: a step (make target) with the tools that implement it, or a
// waiver with a reason. Exactly one of Step and Waived is set.
type Rule struct {
	Step   string   `toml:"step,omitempty"`
	Via    []string `toml:"via,omitempty"`
	Waived string   `toml:"waived,omitempty"`
	// Gaps lists the stacks of this project the step does not cover, each with its reason ("shell: no type checker").
	Gaps []string `toml:"gaps,omitempty"`
}

// Enforced reports whether the rule is backed by a step.
func (r Rule) Enforced() bool { return r.Step != "" }

// Principle names a product principle and the test suite that protects it.
type Principle struct {
	Name  string `toml:"name"`
	Suite string `toml:"suite"`
}

// Contract is a schema or API whose breaking changes need a version bump.
type Contract struct {
	Name    string `toml:"name"`
	Path    string `toml:"path"`
	Kind    string `toml:"kind"`
	Version string `toml:"version"`
}

// QuarantinedTest is a flaky test taken out of the run with an issue link; the list is a ratchet metric.
type QuarantinedTest struct {
	Name  string `toml:"name"`
	Issue string `toml:"issue"`
	Since string `toml:"since"`
}

// Defaults returns the budgets and rings pawl proposes.
func Defaults() Manifest {
	return Manifest{
		Version: Version,
		Base:    "origin/main",
		Budgets: Budgets{
			FileLines:    800,
			Complexity:   15,
			DiffCoverage: 80,
			PRLines:      map[string]int{"feature": 800, "bugfix": 200, "hotfix": 50, "refactor": 800},
		},
		Rings: map[string]Ring{
			"precommit": {Target: "10s", Limit: "20s"},
			"check":     {Target: "10m", Limit: "15m"},
			"nightly":   {Target: "20m", Limit: "30m"},
		},
		Rules: map[string]Rule{},
	}
}

// Load reads the manifest of the project at root.
func Load(root string) (*Manifest, error) {
	path := filepath.Join(root, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	meta, err := toml.Decode(string(data), &m)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		return nil, fmt.Errorf("%s: unknown keys: %s", FileName, strings.Join(keys, ", "))
	}
	if m.Version != Version {
		return nil, fmt.Errorf("%s: version %d, this pawl reads version %d", FileName, m.Version, Version)
	}
	if m.Rules == nil {
		m.Rules = map[string]Rule{}
	}
	return &m, nil
}

// Exists reports whether the project has a manifest.
func Exists(root string) bool {
	_, err := os.Stat(filepath.Join(root, FileName))
	return err == nil
}

// Problem is one validation finding.
type Problem struct {
	RuleID  string
	Message string
}

func (p Problem) String() string {
	if p.RuleID == "" {
		return p.Message
	}
	return p.RuleID + ": " + p.Message
}

// Validate checks the manifest against the catalog: every rule present, exactly one of step or waived, reasons
// non-empty, rings parseable, budgets positive.
func (m *Manifest) Validate(cat *catalog.Catalog) []Problem {
	var out []Problem
	for _, r := range cat.Rules {
		e, ok := m.Rules[r.ID]
		switch {
		case !ok:
			out = append(out, Problem{r.ID, "missing from the manifest: add a step or a waiver"})
		case e.Step != "" && e.Waived != "":
			out = append(out, Problem{r.ID, "has both a step and a waiver; keep one"})
		case e.Step == "" && e.Waived == "":
			out = append(out, Problem{r.ID, "has neither a step nor a waiver"})
		case e.Waived != "" && len(strings.Fields(e.Waived)) < 3:
			out = append(out, Problem{r.ID, "waiver needs a reason (at least three words)"})
		case r.IsReview() && e.Step != "":
			out = append(out, Problem{r.ID, "review rules cannot be enforced by a step; they are the generated checklist"})
		}
	}
	ids := make([]string, 0, len(m.Rules))
	for id := range m.Rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, ok := cat.Get(id); !ok {
			out = append(out, Problem{id, "is not a catalog rule in this version of pawl"})
		}
	}
	for name, ring := range m.Rings {
		if _, _, err := ring.Durations(); err != nil {
			out = append(out, Problem{"", fmt.Sprintf("rings.%s: %v", name, err)})
		}
	}
	for _, name := range []string{"precommit", "check", "nightly"} {
		if _, ok := m.Rings[name]; !ok {
			out = append(out, Problem{"", fmt.Sprintf("rings.%s is missing", name)})
		}
	}
	if m.Budgets.FileLines <= 0 || m.Budgets.Complexity <= 0 || m.Budgets.DiffCoverage < 0 || m.Budgets.DiffCoverage > 100 {
		out = append(out, Problem{"", "budgets: file_lines and complexity must be positive, diff_coverage between 0 and 100"})
	}
	for _, q := range m.Quarantine {
		if q.Name == "" || q.Issue == "" {
			out = append(out, Problem{"", "quarantine entries need a name and an issue link"})
		}
	}
	return out
}

// Waived returns the ids of waived rules in catalog order.
func (m *Manifest) Waived(cat *catalog.Catalog) []string {
	var out []string
	for _, r := range cat.Rules {
		if e, ok := m.Rules[r.ID]; ok && e.Waived != "" {
			out = append(out, r.ID)
		}
	}
	return out
}

// Steps returns every distinct step named by an enforced rule, sorted.
func (m *Manifest) Steps() []string {
	set := map[string]bool{}
	for _, e := range m.Rules {
		if e.Step != "" {
			set[e.Step] = true
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Write renders the manifest as commented TOML in catalog order and writes it to root/pawl.toml.
func Write(root string, m *Manifest, cat *catalog.Catalog) error {
	var b bytes.Buffer
	b.WriteString("# pawl manifest: what this project does about every catalog rule. `pawl rules` explains each rule;\n")
	b.WriteString("# `pawl check` fails when a rule has neither a step nor a waiver. Project-owned: edit freely.\n\n")
	fmt.Fprintf(&b, "version = %d\n", m.Version)
	fmt.Fprintf(&b, "stacks = %s\n", tomlStrings(m.Stacks))
	fmt.Fprintf(&b, "base = %q\n\n", m.Base)

	b.WriteString("[project]\n")
	fmt.Fprintf(&b, "config_module = %q   # the one place that reads the environment (resilience.config-owner)\n", m.Project.ConfigModule)
	fmt.Fprintf(&b, "own_module = %q      # what tests must not mock (tests.mock-edges)\n", m.Project.OwnModule)
	fmt.Fprintf(&b, "test_dirs = %s\n", tomlStrings(m.Project.TestDirs))
	fmt.Fprintf(&b, "exclude = %s          # paths the checks skip (generated code, fixtures), as path prefixes\n\n", tomlStrings(m.Project.Exclude))

	b.WriteString("[budgets]\n")
	fmt.Fprintf(&b, "file_lines = %d\n", m.Budgets.FileLines)
	fmt.Fprintf(&b, "complexity = %d\n", m.Budgets.Complexity)
	fmt.Fprintf(&b, "diff_coverage = %d\n", m.Budgets.DiffCoverage)
	b.WriteString("[budgets.pr_lines]\n")
	for _, k := range []string{"feature", "bugfix", "hotfix", "refactor"} {
		fmt.Fprintf(&b, "%s = %d\n", k, m.Budgets.PRLines[k])
	}
	b.WriteString("\n")

	for _, name := range []string{"precommit", "check", "nightly"} {
		r := m.Rings[name]
		fmt.Fprintf(&b, "[rings.%s]\ntarget = %q\nlimit = %q\n", name, r.Target, r.Limit)
	}
	b.WriteString("\n")

	for _, class := range cat.Classes() {
		fmt.Fprintf(&b, "# --- %s %s\n", class, strings.Repeat("-", 100-len(class)))
		for _, r := range cat.ByClass(class) {
			e := m.Rules[r.ID]
			fmt.Fprintf(&b, "# %s\n[rules.%q]\n", r.Title, r.ID)
			if e.Step != "" {
				fmt.Fprintf(&b, "step = %q\n", e.Step)
				if len(e.Via) > 0 {
					fmt.Fprintf(&b, "via = %s\n", tomlStrings(e.Via))
				}
				if len(e.Gaps) > 0 {
					fmt.Fprintf(&b, "gaps = %s\n", tomlStrings(e.Gaps))
				}
			} else {
				fmt.Fprintf(&b, "waived = %q\n", e.Waived)
			}
			b.WriteString("\n")
		}
	}

	if len(m.Principles) > 0 {
		for _, p := range m.Principles {
			fmt.Fprintf(&b, "[[principles]]\nname = %q\nsuite = %q\n\n", p.Name, p.Suite)
		}
	} else {
		b.WriteString("# [[principles]]            # a product principle and the test suite that protects it (tests.principles)\n# name = \"...\"\n# suite = \"test/principles/*_test.go\"\n\n")
	}
	if len(m.Contracts) > 0 {
		for _, c := range m.Contracts {
			fmt.Fprintf(&b, "[[contracts]]\nname = %q\npath = %q\nkind = %q\nversion = %q\n\n", c.Name, c.Path, c.Kind, c.Version)
		}
	} else {
		b.WriteString("# [[contracts]]             # a schema or API whose breaking change needs a version bump (drift.contracts)\n# name = \"api\"\n# path = \"docs/openapi.yaml\"\n# kind = \"openapi\"\n# version = \"1.0.0\"\n\n")
	}
	for _, q := range m.Quarantine {
		fmt.Fprintf(&b, "[[quarantine]]\nname = %q\nissue = %q\nsince = %q\n\n", q.Name, q.Issue, q.Since)
	}
	return os.WriteFile(filepath.Join(root, FileName), b.Bytes(), 0o644)
}

func tomlStrings(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// ErrNoManifest is returned by Require when the project has no pawl.toml.
var ErrNoManifest = errors.New("no pawl.toml here; run `pawl init`")

// Require loads the manifest or returns ErrNoManifest.
func Require(root string) (*Manifest, error) {
	if !Exists(root) {
		return nil, ErrNoManifest
	}
	return Load(root)
}
