package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/manifest"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func goShellProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/app\n\ngo 1.25\n")
	write(t, root, "main.go", "package main\n\nfunc main() {}\n")
	write(t, root, "main_test.go", "package main\n")
	write(t, root, "bin/run.sh", "#!/usr/bin/env bash\nset -euo pipefail\n")
	write(t, root, "node_modules/x/index.js", "x\n")
	write(t, root, "gen/big.go", "package gen\n")
	return root
}

func TestDetectFindsStacksAndFacts(t *testing.T) {
	root := goShellProject(t)
	p, err := Detect(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Manifest.Stacks, ","); got != "go,shell" {
		t.Errorf("stacks = %s", got)
	}
	if p.Adapters[0].Name() != CommonStack || len(p.Adapters) != 3 || len(p.StackAdapters()) != 2 {
		t.Errorf("adapters = %d", len(p.Adapters))
	}
	if p.Params.Facts["module"] != "example.com/app" {
		t.Errorf("facts = %v", p.Params.Facts)
	}
	for _, f := range p.Files {
		if strings.HasPrefix(f, "node_modules/") {
			t.Errorf("excluded directory listed: %s", f)
		}
	}
	if p.Repo != nil || p.BaseRef() != "" {
		t.Error("a plain directory has no repository and no base")
	}
	if !p.IsTest("main_test.go") || p.IsTest("main.go") {
		t.Error("IsTest is wrong")
	}
	src := strings.Join(p.SourceFiles(), ",")
	if !strings.Contains(src, "main.go") || !strings.Contains(src, "bin/run.sh") {
		t.Errorf("SourceFiles = %s", src)
	}
}

func TestDetectHonoursGivenStacksAndRejectsUnknown(t *testing.T) {
	root := goShellProject(t)
	p, err := Detect(root, []string{"go"})
	if err != nil || strings.Join(p.Manifest.Stacks, ",") != "go" {
		t.Fatalf("Detect with stacks: %v %v", p, err)
	}
	if _, err := Detect(root, []string{"cobol"}); err == nil || !strings.Contains(err.Error(), "unknown stack") {
		t.Errorf("unknown stack: %v", err)
	}
	if _, err := Detect(root, []string{CommonStack}); err == nil {
		t.Error("the common stack must not be listed")
	}
}

func TestDefaultRulesCoverTheCatalogAndRecordGaps(t *testing.T) {
	root := goShellProject(t)
	p, err := Detect(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	rules := p.DefaultRules()
	p.Manifest.Rules = rules
	if problems := p.Manifest.Validate(p.Catalog); len(problems) != 0 {
		t.Fatalf("default rules are not a valid manifest: %v", problems)
	}
	types := rules["code.types"]
	if types.Step != "lint" || len(types.Gaps) != 1 || !strings.HasPrefix(types.Gaps[0], "shell:") {
		t.Errorf("code.types = %+v", types)
	}
	if dead := rules["code.dead"]; len(dead.Via) != 2 {
		t.Errorf("code.dead should join the tools of both stacks: %+v", dead)
	}
	if rules["review.simplest"].Waived == "" || rules["escape.suppressions"].Step != "check" {
		t.Errorf("review or generic rule wrong: %+v %+v", rules["review.simplest"], rules["escape.suppressions"])
	}
}

func TestLoadNeedsAManifestAndAppliesExcludes(t *testing.T) {
	root := goShellProject(t)
	if _, err := Load(root); err != manifest.ErrNoManifest {
		t.Fatalf("Load without a manifest: %v", err)
	}
	p, err := Detect(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.Manifest.Rules = p.DefaultRules()
	p.Manifest.Project.Exclude = []string{"gen/"}
	if err := manifest.Write(root, p.Manifest, p.Catalog); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range loaded.Files {
		if strings.HasPrefix(f, "gen/") {
			t.Errorf("excluded path listed: %s", f)
		}
	}
	if len(StackNames()) != 6 {
		t.Errorf("StackNames = %v", StackNames())
	}
}
