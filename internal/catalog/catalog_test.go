package catalog

import (
	"strings"
	"testing"
)

func TestLoadIsValidAndComplete(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Rules) < 40 {
		t.Fatalf("catalog has %d rules, expected the full set", len(c.Rules))
	}
	seen := map[string]bool{}
	for _, r := range c.Rules {
		if seen[r.ID] {
			t.Errorf("duplicate id %s", r.ID)
		}
		seen[r.ID] = true
		if got, ok := c.Get(r.ID); !ok || got.ID != r.ID {
			t.Errorf("Get(%s) failed", r.ID)
		}
		if !strings.HasPrefix(r.ID, idPrefix(r.Class)) {
			t.Errorf("%s: id does not match class %s", r.ID, r.Class)
		}
		if r.IsReview() && r.Ring != "" {
			t.Errorf("%s: review rules have no ring", r.ID)
		}
	}
	if _, ok := c.Get("no.such-rule"); ok {
		t.Error("Get returned a rule that does not exist")
	}
	if len(c.IDs()) != len(c.Rules) {
		t.Error("IDs and Rules differ in length")
	}
}

func idPrefix(class Class) string {
	switch class {
	case Hygiene:
		return "code."
	default:
		return string(class) + "."
	}
}

func TestClassesAreOrderedAndCoverEveryRule(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	classes := c.Classes()
	if classes[0] != Hygiene || classes[len(classes)-1] != Review {
		t.Errorf("unexpected class order: %v", classes)
	}
	total := 0
	for _, class := range classes {
		total += len(c.ByClass(class))
	}
	if total != len(c.Rules) {
		t.Errorf("classes cover %d of %d rules", total, len(c.Rules))
	}
}

func TestValidateRejectsBrokenRules(t *testing.T) {
	cases := map[string]Rule{
		"needs an id":           {ID: "nodot", Class: Hygiene, Ring: Check, Title: "t", Statement: "s"},
		"needs a title":         {ID: "code.x", Class: Hygiene, Ring: Check, Statement: "s"},
		"needs a ring":          {ID: "code.x", Class: Hygiene, Title: "t", Statement: "s"},
		"needs guidance":        {ID: "review.x", Class: Review, Title: "t", Statement: "s"},
		"incomplete source":     {ID: "review.x", Class: Review, Title: "t", Statement: "s", Guidance: "g", Source: []Source{{Repo: "a/b"}}},
		"unknown class":         {ID: "other.x", Class: "other", Title: "t", Statement: "s"},
	}
	for want, rule := range cases {
		err := validate(rule)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("validate(%s): got %v, want an error containing %q", rule.ID, err, want)
		}
	}
	ok := Rule{ID: "review.x", Class: Review, Title: "t", Statement: "s", Guidance: "g", Source: []Source{{Repo: "a/b", Ref: "abc", Path: "p", License: "MIT", Modified: true}}}
	if err := validate(ok); err != nil {
		t.Errorf("validate(valid review rule): %v", err)
	}
}
