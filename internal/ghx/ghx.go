// Package ghx reads pull request state through the gh command line for `pawl status`.
package ghx

import (
	"errors"
	"io"

	"github.com/rojosinalma/pawl/internal/project"
)

// Status prints the state of the current branch's pull request: each failing check, the first file and line of its
// failure when the log has one, and the command that fixes it. failed is true when a required check failed.
// Without gh, a remote or a pull request it explains what is missing and returns failed=false, err=nil.
func Status(p *project.Project, w io.Writer) (failed bool, err error) {
	return false, errors.New("ghx.Status: not implemented")
}
