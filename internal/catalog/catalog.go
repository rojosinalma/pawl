// Package catalog holds the abstract quality rules pawl verifies. The rules live in catalog.yaml, embedded in the
// binary; adapters map each rule to a stack's tool and the manifest records what a project does about it.
package catalog

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed catalog.yaml
var raw []byte

// Class groups rules by what they protect.
type Class string

const (
	Hygiene    Class = "hygiene"
	Escape     Class = "escape"
	Repo       Class = "repo"
	Resilience Class = "resilience"
	Tests      Class = "tests"
	Drift      Class = "drift"
	Speed      Class = "speed"
	Review     Class = "review"
)

// Ring names where an enforcement normally runs.
type Ring string

const (
	Precommit Ring = "precommit"
	Check     Ring = "check"
	Nightly   Ring = "nightly"
)

// Source cites the upstream guide a review rule was distilled from.
type Source struct {
	Repo     string `yaml:"repo"`
	Ref      string `yaml:"ref"`
	Path     string `yaml:"path"`
	License  string `yaml:"license"`
	Modified bool   `yaml:"modified"`
}

// Rule is one catalog entry.
type Rule struct {
	ID        string   `yaml:"id"`
	Class     Class    `yaml:"class"`
	Ring      Ring     `yaml:"ring"`
	Generic   bool     `yaml:"generic"`
	Title     string   `yaml:"title"`
	Statement string   `yaml:"statement"`
	Guidance  string   `yaml:"guidance"`
	Source    []Source `yaml:"source"`
}

// IsReview reports whether the rule is a checklist item no tool reaches.
func (r Rule) IsReview() bool { return r.Class == Review }

// Catalog is the loaded rule set, in file order.
type Catalog struct {
	Rules []Rule
	byID  map[string]Rule
}

type file struct {
	Rules []Rule `yaml:"rules"`
}

var loaded *Catalog

// Load parses the embedded catalog once and validates it.
func Load() (*Catalog, error) {
	if loaded != nil {
		return loaded, nil
	}
	var f file
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	c := &Catalog{Rules: f.Rules, byID: map[string]Rule{}}
	for _, r := range f.Rules {
		if err := validate(r); err != nil {
			return nil, err
		}
		if _, dup := c.byID[r.ID]; dup {
			return nil, fmt.Errorf("catalog: duplicate rule %q", r.ID)
		}
		c.byID[r.ID] = r
	}
	loaded = c
	return c, nil
}

func validate(r Rule) error {
	if r.ID == "" || !strings.Contains(r.ID, ".") {
		return fmt.Errorf("catalog: rule %q needs an id of the form class.name", r.ID)
	}
	if r.Title == "" || r.Statement == "" {
		return fmt.Errorf("catalog: rule %s needs a title and a statement", r.ID)
	}
	switch r.Class {
	case Hygiene, Escape, Repo, Resilience, Tests, Drift, Speed:
		if r.Ring == "" {
			return fmt.Errorf("catalog: rule %s needs a ring", r.ID)
		}
	case Review:
		if r.Guidance == "" {
			return fmt.Errorf("catalog: review rule %s needs guidance", r.ID)
		}
		for _, s := range r.Source {
			if s.Repo == "" || s.Ref == "" || s.Path == "" || s.License == "" || !s.Modified {
				return fmt.Errorf("catalog: rule %s has an incomplete source (repo, ref, path, license, modified)", r.ID)
			}
		}
	default:
		return fmt.Errorf("catalog: rule %s has unknown class %q", r.ID, r.Class)
	}
	return nil
}

// Get returns a rule by id.
func (c *Catalog) Get(id string) (Rule, bool) {
	r, ok := c.byID[id]
	return r, ok
}

// IDs returns every rule id in file order.
func (c *Catalog) IDs() []string {
	out := make([]string, 0, len(c.Rules))
	for _, r := range c.Rules {
		out = append(out, r.ID)
	}
	return out
}

// ByClass returns the rules of one class in file order.
func (c *Catalog) ByClass(class Class) []Rule {
	var out []Rule
	for _, r := range c.Rules {
		if r.Class == class {
			out = append(out, r)
		}
	}
	return out
}

// Classes returns the classes present, in a fixed order.
func (c *Catalog) Classes() []Class {
	order := map[Class]int{Hygiene: 0, Escape: 1, Repo: 2, Resilience: 3, Tests: 4, Drift: 5, Speed: 6, Review: 7}
	seen := map[Class]bool{}
	var out []Class
	for _, r := range c.Rules {
		if !seen[r.Class] {
			seen[r.Class] = true
			out = append(out, r.Class)
		}
	}
	sort.Slice(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })
	return out
}
