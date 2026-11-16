package makefile_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/makefile"
	"github.com/rojosinalma/pawl/internal/stack"
	"github.com/rojosinalma/pawl/internal/stack/golang"
	"github.com/rojosinalma/pawl/internal/stack/python"
	"github.com/rojosinalma/pawl/internal/stack/ruby"
	"github.com/rojosinalma/pawl/internal/stack/rust"
	"github.com/rojosinalma/pawl/internal/testrepo"
)

// Without pawl each stack's test scope is the stack's full run: what `pawl affected` prints for everything, or the
// empty scope where the runner itself collects every test.
func TestScopesWithoutPawlRunEveryTest(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	for framework, want := range map[string]string{
		"minitest": "go=[./...] rust=[--workspace] python=[] ruby=[test/a_test.rb]",
		"rspec":    "go=[./...] rust=[--workspace] python=[] ruby=[]",
	} {
		t.Run(framework, func(t *testing.T) {
			p := stack.Params{Facts: map[string]string{"test_framework": framework}}
			adapters := []stack.Adapter{golang.Adapter{}, rust.Adapter{}, python.Adapter{}, ruby.Adapter{}}
			root := testrepo.New(t, map[string]string{
				"Makefile": makefile.Generate(adapters, p, 80, "origin/main") +
					"scopes:\n\t@echo 'go=[$(GO_TEST_SCOPE)] rust=[$(CARGO_TEST_SCOPE)] python=[$(PY_TEST_SCOPE)] ruby=[$(RUBY_TEST_SCOPE)]'\n",
				"test/a_test.rb": "",
			})
			cmd := exec.CommandContext(t.Context(), "make", "--no-print-directory", "PAWL=/nonexistent/pawl", "scopes")
			cmd.Dir = root
			out, err := cmd.Output()
			if got := strings.TrimSpace(string(out)); err != nil || got != want {
				t.Errorf("scopes without pawl = %q (%v), want %q", got, err, want)
			}
		})
	}
}
