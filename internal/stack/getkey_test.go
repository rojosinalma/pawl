package stack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetKeyReadsKeysWithSlashesAndDots(t *testing.T) {
	dir := t.TempDir()
	yml := "Lint/Debugger:\n  Enabled: false\nLayout:\n  Enabled: true\nper-file:\n  \"tests/*.py\": [S101]\n"
	if err := os.WriteFile(filepath.Join(dir, "c.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir, "c.yml", YAML)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		keys []string
		want any
		ok   bool
	}{
		{[]string{"Lint/Debugger", "Enabled"}, false, true},
		{[]string{"Layout", "Enabled"}, true, true},
		{[]string{"per-file", "tests/*.py", "0"}, "S101", true},
		{[]string{"Lint", "Debugger"}, nil, false},
		{[]string{"Layout", "Enabled", "deeper"}, nil, false},
		{[]string{"per-file", "tests/*.py", "3"}, nil, false},
	}
	for _, c := range cases {
		got, ok := cfg.GetKey(c.keys...)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("GetKey(%q) = %v, %v; want %v, %v", c.keys, got, ok, c.want, c.ok)
		}
	}
	if v, ok := cfg.Get("Layout.Enabled"); !ok || v != true {
		t.Errorf("Get(Layout.Enabled) = %v, %v", v, ok)
	}
	var none *Config
	if _, ok := none.GetKey("a"); ok {
		t.Error("GetKey on a nil config found a value")
	}
}
