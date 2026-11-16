// Package runnertest fakes runner.Runner for tests: a script of command lines and their replies.
package runnertest

import (
	"fmt"
	"strings"
)

// Reply is what a scripted command prints and the error it ends with.
type Reply struct {
	Out string
	Err error
}

// Script answers a command by its name and arguments joined with spaces ("git remote get-url origin"); a command
// it does not list fails, so a test notices a call it did not expect.
type Script map[string]Reply

// Output returns the scripted reply for the command; dir is ignored, since the scripts key on the command alone.
func (s Script) Output(_, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	r, ok := s[key]
	if !ok {
		return nil, fmt.Errorf("unexpected command %q", key)
	}
	return []byte(r.Out), r.Err
}
