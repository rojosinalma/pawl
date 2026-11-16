// Package config is the one place pawl reads its environment. Every variable is optional.
//
//	PAWL_BASE       the ref the change is compared with; default: the manifest's base (origin/main)
//	PAWL_FULL       "1" runs every test instead of the affected scope; default: affected
//	PAWL_VERBOSE    "1" makes doctor list every healthy tool; default: problems only
//	PAWL_WORK_TYPE  feature, bugfix, hotfix or refactor, for the pull request size budget; default: from the branch name
//	MAKELEVEL       set by make in its recipes; pawl reads it to tell a recipe's call from a person's
package config

import (
	"os"
	"strings"
)

// Base returns the ref override for the change comparison, or "".
func Base() string { return strings.TrimSpace(os.Getenv("PAWL_BASE")) }

// Full reports whether every test must run.
func Full() bool { return os.Getenv("PAWL_FULL") == "1" }

// Verbose reports whether doctor lists healthy tools too.
func Verbose() bool { return os.Getenv("PAWL_VERBOSE") == "1" }

// WorkType returns the declared work type of the change, or "".
func WorkType() string { return strings.TrimSpace(os.Getenv("PAWL_WORK_TYPE")) }

// InsideMake reports whether pawl runs from a make recipe: make sets MAKELEVEL for every command it starts.
func InsideMake() bool { return os.Getenv("MAKELEVEL") != "" }
