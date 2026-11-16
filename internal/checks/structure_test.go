package checks

import (
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/report"
)

const minimalMakefile = `PAWL ?= pawl

check: lint test
	$(PAWL) check

fmt:
	gofmt -w .

fmt-check:
	gofmt -l .

lint: fmt-check
	golangci-lint run

test:
	go test $(SCOPE)

precommit:
	$(PAWL) check --ring precommit

coverage:
	go test -cover ./...

audit:
	govulncheck ./...

mutation:
	gremlins unleash

nightly: check coverage audit mutation
	$(PAWL) check --ring nightly
`

func TestTargets(t *testing.T) {
	for _, tc := range []struct {
		name     string
		makefile string
		edit     func(m *manifest.Manifest)
		rule     string
		sev      report.Severity
		want     string
	}{
		{"missing standard target", strings.Replace(minimalMakefile, "fmt:\n\tgofmt -w .\n", "", 1), nil,
			"repo.targets", report.Error, "target `fmt` is missing"},
		{"check without pawl check", strings.Replace(minimalMakefile, "\t$(PAWL) check\n", "\t@true\n", 1), nil,
			"repo.targets", report.Error, "does not run `pawl check`"},
		{"step is no target", minimalMakefile, func(m *manifest.Manifest) {
			m.Rules["code.lint"] = manifest.Rule{Step: "vet", Via: []string{"golangci-lint"}}
		}, "repo.targets", report.Error, "step `vet` (code.lint) is not a make target"},
		{"unreachable step", minimalMakefile + "\nvet:\n\tgo vet ./...\n", func(m *manifest.Manifest) {
			m.Rules["code.lint"] = manifest.Rule{Step: "vet", Via: []string{"golangci-lint"}}
		}, "repo.targets", report.Error, "step `vet` (code.lint) is not reached"},
		{"test without affected scope", minimalMakefile, nil, "speed.affected", report.Warning, "runs every test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, false)
			f.write("Makefile", tc.makefile)
			if tc.edit != nil {
				f.editManifest(tc.edit)
			}
			requireFinding(t, f.check("check"), tc.rule, tc.sev, tc.want)
		})
	}
}

func TestTargetsAffectedThroughVariables(t *testing.T) {
	f := newFixture(t, false)
	f.write("Makefile", strings.Replace(minimalMakefile, "PAWL ?= pawl\n", "PAWL ?= pawl\nSCOPE ?= $(INNER)\nINNER = $(shell $(PAWL) affected go)\n", 1))
	requireClean(t, f.check("check", "speed.affected", "repo.targets"))
}

func TestNoMakefile(t *testing.T) {
	f := newFixture(t, false)
	f.remove("Makefile")
	rep := f.check("check")
	requireFinding(t, rep, "repo.targets", report.Error, "no Makefile")
	if got := find(rep, "speed.affected", ""); len(got) > 0 {
		t.Errorf("speed.affected should leave a missing Makefile to repo.targets: %v", got)
	}
}

func TestVerifiers(t *testing.T) {
	disabled := "version: \"2\"\nlinters:\n  default: standard\n  disable: [errcheck]\n"
	for _, tc := range []struct {
		name   string
		config string
		edit   func(m *manifest.Manifest)
		want   bool
	}{
		{"disabled errcheck fails", disabled, nil, true},
		{"waived rule is not verified", disabled, func(m *manifest.Manifest) {
			m.Rules["code.dropped-results"] = manifest.Rule{Waived: "legacy code drops errors on purpose"}
		}, false},
		{"replaced tool is not verified", disabled, func(m *manifest.Manifest) {
			m.Rules["code.dropped-results"] = manifest.Rule{Step: "lint", Via: []string{"semgrep"}}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, false)
			f.write(".golangci.yml", tc.config)
			if tc.edit != nil {
				f.editManifest(tc.edit)
			}
			got := find(f.check("check"), "code.dropped-results", "errcheck")
			if tc.want && len(got) == 0 {
				t.Fatal("expected a code.dropped-results finding")
			}
			if !tc.want && len(got) > 0 {
				t.Fatalf("unexpected finding: %v", got)
			}
		})
	}
}
