// Command pawl makes a repository's own tooling enforce a quality catalog, and lets the numbers move one way only.
package main

import "github.com/rojosinalma/pawl/internal/cli"

// version is set by the release build (-ldflags "-X main.version=...").
var version = "dev"

func main() { cli.Main(version) }
