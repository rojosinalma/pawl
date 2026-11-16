// Package affected computes the test scope of the change against the base: what `make test` passes to the stack's
// test runner in the pull request ring.
package affected

import (
	"errors"

	"github.com/rojosinalma/pawl/internal/project"
)

// Scope returns the arguments for the stack's test runner, as one line for a Makefile `$(shell ...)`:
// Go: the packages that depend on a changed package ("./internal/a/... ./cmd/x"); Rust: "-p crate" per affected
// workspace crate; Python, Ruby, TypeScript: the test files that changed or that match a changed source file by
// name. It returns the stack's "everything" scope when PAWL_FULL=1, when there is no base, or when a
// configuration, lockfile, Makefile, workflow or pawl.toml changed. An empty change returns the smallest valid
// scope (for Go "./..." is replaced by nothing only when no package is affected: then Scope returns "" and
// exit status 0, and the Makefile's test recipe must cope with an empty scope).
func Scope(p *project.Project, stackName string) (string, error) {
	return "", errors.New("affected.Scope: not implemented")
}
