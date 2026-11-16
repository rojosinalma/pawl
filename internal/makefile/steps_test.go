package makefile

import (
	"reflect"
	"strings"
	"testing"
)

func TestStepNamesFollowTheTool(t *testing.T) {
	got := stepNames("lint", []string{
		"ast-grep scan -c .pawl/sgconfig.yml",
		"gitleaks git . --exit-code 1",
		"gitleaks dir . --exit-code 1",
		"node_modules/.bin/tsc --noEmit",
		"bundle exec rubocop",
		"uv run pyright",
		"@[ -z \"$(X)\" ] || true",
	})
	want := []string{"lint-ast-grep", "lint-gitleaks", "lint-gitleaks-2", "lint-tsc", "lint-rubocop", "lint-pyright", "lint-true"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stepNames = %v, want %v", got, want)
	}
}

func TestMergeSkipsStepsOfAnExistingLint(t *testing.T) {
	generated := "lint: fmt-check lint-ruff  ## lint\n\t@true\n\nlint-ruff:  ## lint step\n\truff check .\n\ntest:  ## test\n\tpytest\n"
	merged := Merge("lint:\n\tmy-linter\n", generated)
	if strings.Contains(merged, "lint-ruff:") {
		t.Errorf("merge added a step only the project's own lint would use:\n%s", merged)
	}
	if !strings.Contains(merged, "test:") {
		t.Errorf("merge lacks the missing test target:\n%s", merged)
	}
	if merged = Merge("test:\n\tgo test ./...\n", generated); !strings.Contains(merged, "lint-ruff:") {
		t.Errorf("merge dropped the step of the lint target it adds:\n%s", merged)
	}
}
