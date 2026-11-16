// Package cli wires the pawl commands.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rojosinalma/pawl/internal/affected"
	"github.com/rojosinalma/pawl/internal/checks"
	"github.com/rojosinalma/pawl/internal/doctor"
	"github.com/rojosinalma/pawl/internal/ghx"
	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/ratchet"
	"github.com/rojosinalma/pawl/internal/report"
	"github.com/rojosinalma/pawl/internal/rings"
	"github.com/rojosinalma/pawl/internal/scaffold"
)

// errFindings makes the process exit 1 without printing an error: the findings are already on the output.
var errFindings = errors.New("findings")

// Execute runs the command line and returns the exit code.
func Execute(version string, args []string, stdout, stderr io.Writer) int {
	root := newRoot(version, stdout, stderr)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		if !errors.Is(err, errFindings) {
			fmt.Fprintln(stderr, "pawl:", err)
		}
		return 1
	}
	return 0
}

func newRoot(version string, stdout, stderr io.Writer) *cobra.Command {
	var dir string
	root := &cobra.Command{
		Use:           "pawl",
		Short:         "Make a repository's own tooling enforce a quality catalog, one way only",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().StringVarP(&dir, "dir", "C", ".", "project root")

	emit := func(r *report.Report, format string) error {
		f, err := report.ParseFormat(format)
		if err != nil {
			return err
		}
		r.Tool, r.Version = "pawl", version
		if err := r.Write(stdout, f); err != nil {
			return err
		}
		if r.Failed() {
			return errFindings
		}
		return nil
	}

	// init
	var initStacks []string
	var initDry bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write the manifest, the missing tool configurations, the make targets, the hook and the workflows",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := scaffold.Init(dir, scaffold.Options{Stacks: initStacks, DryRun: initDry, Version: version})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "stacks: %s\n", strings.Join(res.Stacks, ", "))
			for _, f := range res.Created {
				fmt.Fprintf(stdout, "  created  %s\n", f)
			}
			for _, f := range res.Merged {
				fmt.Fprintf(stdout, "  extended %s\n", f)
			}
			fmt.Fprintf(stdout, "  kept     %d existing files\n", len(res.Kept))
			if len(res.Waived) > 0 {
				fmt.Fprintf(stdout, "\n%d rules are waived by default; each is a decision for this project (`pawl rules --waived`).\n", len(res.Waived))
			}
			if len(res.Hints) > 0 {
				fmt.Fprintln(stdout, "\nstill to run:")
				for _, h := range res.Hints {
					fmt.Fprintf(stdout, "  %s\n", h)
				}
			}
			fmt.Fprintln(stdout, "\nnext: `mise install` (the pinned tools), `pawl doctor`, `make check`, then commit.")
			return nil
		},
	}
	initCmd.Flags().StringSliceVar(&initStacks, "stack", nil, "stacks to set up instead of detecting them (go, typescript, ruby, python, shell, rust)")
	initCmd.Flags().BoolVar(&initDry, "dry-run", false, "print what would be written")

	// check
	var ring, format string
	var only []string
	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Verify the catalog: manifest, targets, tool configurations, text checks and the ratchet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project.Load(dir)
			if err != nil {
				return err
			}
			r, err := checks.Run(p, checks.Options{Ring: ring, Only: only})
			if err != nil {
				return err
			}
			return emit(r, format)
		},
	}
	checkCmd.Flags().StringVar(&ring, "ring", "check", "precommit (staged files, fast), check, or nightly")
	checkCmd.Flags().StringSliceVar(&only, "only", nil, "limit the run to these rule ids")
	checkCmd.Flags().StringVar(&format, "format", "text", "text, json, sarif or github")

	// doctor
	var doctorFormat string
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Verify that every tool is installed at its pinned version and the hook is active",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project.Load(dir)
			if err != nil {
				return err
			}
			r, err := doctor.Run(p)
			if err != nil {
				return err
			}
			return emit(r, doctorFormat)
		},
	}
	doctorCmd.Flags().StringVar(&doctorFormat, "format", "text", "text, json, sarif or github")

	// status
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show the failing checks of this branch's pull request and the command that fixes each",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project.Load(dir)
			if err != nil {
				return err
			}
			failed, err := ghx.Status(p, stdout)
			if err != nil {
				return err
			}
			if failed {
				return errFindings
			}
			return nil
		},
	}

	// baseline
	var grow string
	baselineCmd := &cobra.Command{
		Use:   "baseline",
		Short: "Record the current ratchet values; they may only shrink (--grow \"reason\" is the one way up)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project.Load(dir)
			if err != nil {
				return err
			}
			cur, err := checks.Measure(p)
			if err != nil {
				return err
			}
			prev, had, err := ratchet.Load(p.Root)
			if err != nil {
				return err
			}
			next := ratchet.FromCurrent(cur, prev)
			if had {
				if up := ratchet.Regressions(prev, cur); len(up) > 0 {
					if len(strings.Fields(grow)) < 3 {
						for _, d := range up {
							fmt.Fprintf(stderr, "%s rose from %d to %d\n", d.Metric, d.Baseline, d.Current)
						}
						return errors.New("the baseline only shrinks; fix the regressions, or record them with --grow \"reason\"")
					}
					g := ratchet.Growth{Date: time.Now().UTC().Format("2006-01-02"), Reason: grow, From: map[string]int{}, To: map[string]int{}}
					for _, d := range up {
						g.From[d.Metric], g.To[d.Metric] = d.Baseline, d.Current
					}
					next.Growth = append(next.Growth, g)
				}
			}
			if err := ratchet.Write(p.Root, next); err != nil {
				return err
			}
			keys := make([]string, 0, len(next.Metrics))
			for k := range next.Metrics {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(stdout, "%-24s %d\n", k, next.Metrics[k])
			}
			fmt.Fprintf(stdout, "wrote %s\n", ratchet.FileName)
			return nil
		},
	}
	baselineCmd.Flags().StringVar(&grow, "grow", "", "reason for raising a value (kept in the baseline)")

	// rules
	var waivedOnly bool
	rulesCmd := &cobra.Command{
		Use:   "rules",
		Short: "List the catalog with what this project does about each rule",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project.Load(dir)
			if err != nil {
				return err
			}
			for _, class := range p.Catalog.Classes() {
				printed := false
				for _, r := range p.Catalog.ByClass(class) {
					e, ok := p.Manifest.Rules[r.ID]
					state := "MISSING"
					switch {
					case ok && e.Step != "":
						state = "make " + e.Step
						if len(e.Via) > 0 {
							state += "  (" + strings.Join(e.Via, ", ") + ")"
						}
					case ok && e.Waived != "":
						state = "waived: " + e.Waived
					}
					if waivedOnly && (!ok || e.Waived == "" || r.IsReview()) {
						continue
					}
					if !printed {
						fmt.Fprintf(stdout, "\n%s\n", strings.ToUpper(string(class)))
						printed = true
					}
					fmt.Fprintf(stdout, "  %-28s %s\n  %-28s %s\n", r.ID, r.Title, "", state)
					for _, g := range e.Gaps {
						fmt.Fprintf(stdout, "  %-28s gap: %s\n", "", g)
					}
				}
			}
			return nil
		},
	}
	rulesCmd.Flags().BoolVar(&waivedOnly, "waived", false, "only the waived rules a tool could enforce")
	rulesCmd.AddCommand(&cobra.Command{
		Use:   "show <rule-id>",
		Short: "Explain one rule: statement, class, ring, and this project's enforcement",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := project.Load(dir)
			if err != nil {
				return err
			}
			r, ok := p.Catalog.Get(args[0])
			if !ok {
				return fmt.Errorf("no rule %q; `pawl rules` lists them", args[0])
			}
			fmt.Fprintf(stdout, "%s: %s\nclass: %s", r.ID, r.Title, r.Class)
			if r.Ring != "" {
				fmt.Fprintf(stdout, "   ring: %s", r.Ring)
			}
			fmt.Fprintf(stdout, "\n\n%s\n", r.Statement)
			if r.Guidance != "" {
				fmt.Fprintf(stdout, "\n%s\n", r.Guidance)
			}
			for _, s := range r.Source {
				fmt.Fprintf(stdout, "\nsource: %s@%s %s (%s, modified)\n", s.Repo, s.Ref[:min(7, len(s.Ref))], s.Path, s.License)
			}
			if e, ok := p.Manifest.Rules[r.ID]; ok {
				if e.Step != "" {
					fmt.Fprintf(stdout, "\nhere: make %s via %s\n", e.Step, strings.Join(e.Via, ", "))
				} else {
					fmt.Fprintf(stdout, "\nhere: waived: %s\n", e.Waived)
				}
			}
			return nil
		},
	}, &cobra.Command{
		Use:   "sync",
		Short: "Refresh the ast-grep rule pack and the AGENTS.md block from the manifest",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project.Load(dir)
			if err != nil {
				return err
			}
			updated, err := scaffold.SyncRules(p)
			if err != nil {
				return err
			}
			for _, f := range updated {
				fmt.Fprintf(stdout, "  wrote %s\n", f)
			}
			changed, err := scaffold.SyncAgents(p)
			if err != nil {
				return err
			}
			if changed {
				fmt.Fprintln(stdout, "  wrote AGENTS.md")
			}
			if len(updated) == 0 && !changed {
				fmt.Fprintln(stdout, "up to date")
			}
			return nil
		},
	})

	// affected
	affectedCmd := &cobra.Command{
		Use:   "affected <stack>",
		Short: "Print the test scope of the change against the base for one stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := project.Load(dir)
			if err != nil {
				return err
			}
			scope, err := affected.Scope(p, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(stdout, scope)
			return nil
		},
	}

	// budget
	var budgetFormat string
	budgetCmd := &cobra.Command{
		Use:   "budget",
		Short: "Compare the median time of each ring with its target and limit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project.Load(dir)
			if err != nil {
				return err
			}
			r, err := rings.Budget(p)
			if err != nil {
				return err
			}
			return emit(r, budgetFormat)
		},
	}
	budgetCmd.Flags().StringVar(&budgetFormat, "format", "text", "text, json, sarif or github")

	// record
	recordCmd := &cobra.Command{
		Use:    "record <ring> <seconds>",
		Short:  "Store one ring run's wall time (called by the git hook)",
		Args:   cobra.ExactArgs(2),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			secs, err := strconv.ParseFloat(args[1], 64)
			if err != nil || secs < 0 {
				return fmt.Errorf("seconds must be a non-negative number, got %q", args[1])
			}
			if !manifest.Exists(dir) {
				return manifest.ErrNoManifest
			}
			return rings.Record(dir, args[0], time.Duration(secs*float64(time.Second)))
		},
	}

	root.AddCommand(initCmd, checkCmd, doctorCmd, statusCmd, baselineCmd, rulesCmd, affectedCmd, budgetCmd, recordCmd)
	return root
}

// Main is the entry point used by cmd/pawl.
func Main(version string) {
	os.Exit(Execute(version, os.Args[1:], os.Stdout, os.Stderr))
}
