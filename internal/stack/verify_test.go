package stack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rojosinalma/pawl/internal/report"
)

func TestStripJSONC(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a": 1, // one` + "\n" + `"b": 2,}`, `{"a":1,"b":2}`},
		{`{/* block */"a": [1, 2,],}`, `{"a":[1,2]}`},
		{`{"url": "https://example.com/x", "s": "a,}"}`, `{"s":"a,}","url":"https://example.com/x"}`},
		{`{"q": "say \"//hi\"",` + "\n\t" + `}`, `{"q":"say \"//hi\""}`},
		{`{"a": "/* not a comment */"}`, `{"a":"/* not a comment */"}`},
	}
	for _, c := range cases {
		var v any
		if err := json.Unmarshal(StripJSONC([]byte(c.in)), &v); err != nil {
			t.Errorf("StripJSONC(%q) does not parse: %v (%s)", c.in, err, StripJSONC([]byte(c.in)))
			continue
		}
		got, _ := json.Marshal(v)
		if string(got) != c.want {
			t.Errorf("StripJSONC(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func load(t *testing.T, name, content string, format Format) *Config {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(root, name, format)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestGetAndContains(t *testing.T) {
	cfg := load(t, "c.yml", "linters:\n  default: standard\n  enable: [gocognit, nilerr]\n  settings:\n    gocognit:\n      min-complexity: 15\nrules:\n  a.b: true\nlist:\n  - name: x\n", YAML)
	cases := []struct {
		path string
		want any
		ok   bool
	}{
		{"linters.default", "standard", true},
		{"linters.settings.gocognit.min-complexity", 15, true},
		{"linters.enable.1", "nilerr", true},
		{"linters.enable.7", nil, false},
		{"list.0.name", "x", true},
		{"rules/a.b", true, true},
		{"linters.missing", nil, false},
		{"linters.default.deeper", nil, false},
	}
	for _, c := range cases {
		got, ok := cfg.Get(c.path)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("Get(%q) = (%v, %v), want (%v, %v)", c.path, got, ok, c.want, c.ok)
		}
	}
	if !cfg.Contains("linters.enable", "gocognit") || cfg.Contains("linters.enable", "gosec") || cfg.Contains("linters.default", "standard") {
		t.Error("Contains reports the wrong answer")
	}
	var missing *Config
	if _, ok := missing.Get("a"); ok {
		t.Error("Get on a nil config found a value")
	}
}

func TestLoadConfigFormats(t *testing.T) {
	if cfg := load(t, "c.toml", "[lints]\nthreshold = 3\n", TOML); cfg == nil {
		t.Fatal("TOML not loaded")
	} else if v, _ := cfg.Get("lints.threshold"); v != int64(3) {
		t.Errorf("TOML value = %#v", v)
	}
	if cfg := load(t, "c.jsonc", "{\n  // comment\n  \"a\": [1,],\n}\n", JSONC); !cfg.Contains("a", "1") {
		t.Error("JSONC list not loaded")
	}
	if cfg, err := LoadConfig(t.TempDir(), "absent.yml", YAML); cfg != nil || err != nil {
		t.Errorf("missing file = (%v, %v), want (nil, nil)", cfg, err)
	}
}

func TestDeclarativeChecks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "c.yml"), []byte("enable: [a]\nlevel: warning\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := Verify{Root: root, RuleID: "code.lint", Via: "tool"}
	cases := []struct {
		name  string
		check func(Verify) []report.Finding
		fail  bool
	}{
		{"Has present", Has("c.yml", YAML, "enable", "a"), false},
		{"Has absent", Has("c.yml", YAML, "enable", "b"), true},
		{"Lacks present", Lacks("c.yml", YAML, "enable", "a"), true},
		{"Equal same", Equal("c.yml", YAML, "level", "warning"), false},
		{"Equal other", Equal("c.yml", YAML, "level", "error"), true},
		{"NotEqual forbidden", NotEqual("c.yml", YAML, "level", "warning"), true},
		{"NotEqual missing key", NotEqual("c.yml", YAML, "absent", true), false},
		{"Exists", Exists("c.yml"), false},
		{"Exists missing", Exists("d.yml"), true},
		{"Has on missing file", Has("d.yml", YAML, "enable", "a"), true},
	}
	for _, c := range cases {
		if got := len(c.check(v)) > 0; got != c.fail {
			t.Errorf("%s: reported = %v, want %v", c.name, got, c.fail)
		}
	}
}
