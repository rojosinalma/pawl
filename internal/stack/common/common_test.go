package common

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/rojosinalma/pawl/internal/stack"
)

// decodeAll parses every document of a multi-document YAML file.
func decodeAll(t *testing.T, path, content string) []map[string]any {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader([]byte(content)))
	var docs []map[string]any
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs
		}
		if err != nil {
			t.Fatalf("%s: invalid YAML: %v\n%s", path, err, content)
		}
		if doc != nil {
			docs = append(docs, doc)
		}
	}
}

func ids(t *testing.T, rp ruleParams) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, f := range RulePack(rp) {
		for _, doc := range decodeAll(t, f.Path, f.Content) {
			id, _ := doc["id"].(string)
			if id == "" || doc["language"] == nil || doc["rule"] == nil {
				t.Fatalf("%s: a document lacks id, language or rule: %v", f.Path, doc)
			}
			if _, dup := out[id]; dup {
				t.Fatalf("rule id %s appears twice", id)
			}
			out[id] = doc
		}
	}
	return out
}

func TestRulePackRendersValidYAML(t *testing.T) {
	for _, rp := range []ruleParams{
		{},
		{ConfigModule: "internal/config", ConfigModuleOrHint: "internal/config", OwnModule: "greeter", TestDirs: []string{"test", "tests"}},
	} {
		got := ids(t, rp)
		for _, s := range []string{"typescript", "python", "ruby"} {
			for rule, list := range RuleIDs(s) {
				for _, id := range list {
					id = strings.TrimPrefix(id, "ast-grep/")
					if _, ok := got[id]; !ok && !(rp.OwnModule == "" && rule == "tests.mock-edges" && s != "typescript") {
						t.Errorf("%s: RuleIDs names %s for %s, the pack lacks it (own module %q)", s, id, rule, rp.OwnModule)
					}
				}
			}
		}
	}
}

func TestRulePackUsesTheParams(t *testing.T) {
	without := ids(t, ruleParams{ConfigModuleOrHint: "set project.config_module in pawl.toml"})
	if _, ok := without["mock-own-module-py"]; ok {
		t.Error("mock-own-module-py rendered without an own module")
	}
	with := ids(t, ruleParams{ConfigModule: "src/acme/config", ConfigModuleOrHint: "src/acme/config", OwnModule: "acme"})
	py, ok := with["mock-own-module-py"]
	if !ok {
		t.Fatal("mock-own-module-py missing with an own module")
	}
	if re := py["constraints"].(map[string]any)["T"].(map[string]any)["regex"]; re != `^["']acme` {
		t.Errorf("mock-own-module-py regex = %v", re)
	}
	ignores, _ := with["config-owner-ts"]["ignores"].([]any)
	found := false
	for _, ig := range ignores {
		found = found || ig == "src/acme/config/**"
	}
	if !found {
		t.Errorf("config-owner-ts ignores lack the configuration module: %v", ignores)
	}
}

func TestDefaultsPassTheirVerifiers(t *testing.T) {
	dir := t.TempDir()
	p := stack.Params{Complexity: 15}
	for _, c := range (Adapter{}).Configs(p) {
		path := filepath.Join(dir, c.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(c.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range (Adapter{}).Verifiers(p) {
		if got := v.Check(dir); len(got) > 0 {
			t.Errorf("%s: %v", v.RuleID, got)
		}
	}
	if err := os.Remove(filepath.Join(dir, ".jscpd.json")); err != nil {
		t.Fatal(err)
	}
	for _, v := range (Adapter{}).Verifiers(p) {
		if v.RuleID == "escape.duplication" && len(v.Check(dir)) == 0 {
			t.Error("no escape.duplication finding without .jscpd.json")
		}
	}
}

func TestLintWritesTheDuplicationReport(t *testing.T) {
	lint := strings.Join((Adapter{}).Make(stack.Params{}).Lint, "\n")
	if !strings.Contains(lint, "jscpd --silent --reporters json --output $(REPORTS) .") {
		t.Errorf("lint does not write $(REPORTS)/jscpd-report.json:\n%s", lint)
	}
	for _, l := range strings.Split(lint, "\n") {
		if strings.HasPrefix(l, "jscpd") && (strings.Contains(l, "--exit-code") || strings.Contains(l, "--threshold")) {
			t.Errorf("jscpd must exit 0, the ratchet judges duplicates: %s", l)
		}
	}
}
