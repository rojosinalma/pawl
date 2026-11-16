package checks

import (
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/report"
)

const minimalMakefile = `PAWL ?= pawl

check: lint test
	@$(call pawl_run,verify)

fmt:
	gofmt -w .

fmt-check:
	gofmt -l .

lint: fmt-check
	golangci-lint run

test:
	go test $(SCOPE)

precommit:
	@$(call pawl_run,verify --ring precommit)

coverage:
	go test -cover ./...

audit:
	govulncheck ./...

mutation:
	gremlins unleash

nightly: check coverage audit mutation
	@$(call pawl_run,verify --ring nightly)
	@$(call pawl_run,budget)
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
		{"check without pawl verify", strings.Replace(minimalMakefile, "\t@$(call pawl_run,verify)\n", "\t@true\n", 1), nil,
			"repo.targets", report.Error, "does not run `pawl verify`"},
		{"check calls the old pawl check", strings.Replace(minimalMakefile, "\t@$(call pawl_run,verify)\n", "\t$(PAWL) check\n", 1), nil,
			"repo.targets", report.Error, "target `check` calls `pawl check`"},
		{"precommit calls the old pawl check", strings.Replace(minimalMakefile, "@$(call pawl_run,verify --ring precommit)", "$(PAWL) check --ring precommit", 1), nil,
			"repo.targets", report.Error, "target `precommit` calls `pawl check`"},
		{"nightly calls the old pawl check", strings.Replace(minimalMakefile, "@$(call pawl_run,verify --ring nightly)", "pawl check --ring nightly", 1), nil,
			"repo.targets", report.Error, "target `nightly` calls `pawl check`"},
		{"check calls pawl without the guard", strings.Replace(minimalMakefile, "\t@$(call pawl_run,verify)\n", "\t$(PAWL) verify\n", 1), nil,
			"repo.targets", report.Warning, "`make check` fails on a machine without pawl"},
		{"nightly calls budget without the guard", strings.Replace(minimalMakefile, "@$(call pawl_run,budget)", "pawl budget", 1), nil,
			"repo.targets", report.Warning, "`make nightly` fails on a machine without pawl"},
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

func TestOldCheckRecipeIsOneFindingWithTheFix(t *testing.T) {
	f := newFixture(t, false)
	f.write("Makefile", strings.Replace(minimalMakefile, "@$(call pawl_run,verify --ring precommit)", "$(PAWL) check --ring precommit", 1))
	got := find(f.check("check", "repo.targets"), "repo.targets", "")
	want := "use the recipe line `@$(call pawl_run,verify --ring precommit)`; pawl_run and pawl_guard are defined at the top of the Makefile `pawl init` writes"
	if len(got) != 1 || got[0].Severity != report.Error || got[0].Fix != want {
		t.Errorf("an old check recipe: %+v", got)
	}
}

// The plain call still reaches the verification: the guard is a warning with the guarded line, not an error.
func TestUnguardedVerifyCountsAndWarnsWithTheGuardedLine(t *testing.T) {
	f := newFixture(t, false)
	f.write("Makefile", strings.Replace(minimalMakefile, "@$(call pawl_run,verify --ring nightly)", "$(PAWL) verify --ring nightly", 1))
	got := find(f.check("check", "repo.targets"), "repo.targets", "")
	want := "use the recipe line `@$(call pawl_run,verify --ring nightly)`; pawl_run and pawl_guard are defined at the top of the Makefile `pawl init` writes"
	if len(got) != 1 || got[0].Severity != report.Warning || got[0].Fix != want ||
		got[0].Message != "`make nightly` fails on a machine without pawl: the recipe runs `$(PAWL) verify --ring nightly` without the guard" {
		t.Errorf("an unguarded nightly recipe: %+v", got)
	}
	f.write("Makefile", strings.Replace(minimalMakefile, "\t@$(call pawl_run,verify)\n", "\t$(PAWL) verify\n", 1))
	if got := find(f.check("check", "repo.targets"), "repo.targets", "does not run"); len(got) != 0 {
		t.Errorf("the plain `$(PAWL) verify` must still reach the verification: %+v", got)
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
