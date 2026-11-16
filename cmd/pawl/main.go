// Command pawl makes a repository's own tooling enforce a quality catalog, and lets the numbers move one way only.
package main

import (
	"io"
	"os"
	"runtime/debug"

	"github.com/rojosinalma/pawl/internal/cli"
)

// version and commit are set by the release build (-ldflags "-X main.version=... -X main.commit=...").
var (
	version = "dev"
	commit  = ""
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run executes the command line with the version of this build and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	module := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		module = info.Main.Version
	}
	return cli.Execute(cli.Build{Version: cli.ResolveVersion(version, module), Commit: commit}, args, stdout, stderr)
}
