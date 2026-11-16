// Package cli wires the pawl commands.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/rojosinalma/pawl/internal/affected"
	"github.com/rojosinalma/pawl/internal/catalog"
	"github.com/rojosinalma/pawl/internal/checks"
	"github.com/rojosinalma/pawl/internal/doctor"
	"github.com/rojosinalma/pawl/internal/ghx"
	"github.com/rojosinalma/pawl/internal/install"
	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/ratchet"
	"github.com/rojosinalma/pawl/internal/report"
	"github.com/rojosinalma/pawl/internal/ringrun"
	"github.com/rojosinalma/pawl/internal/rings"
	"github.com/rojosinalma/pawl/internal/scaffold"
	"github.com/rojosinalma/pawl/internal/toolenv"
)

// errFindings makes the process exit 1 without printing an error: the findings are already on the output.
var errFindings = errors.New("findings")

// ringNames are the rings `pawl run` accepts, from the fastest to the slowest.
var ringNames = []string{string(catalog.Precommit), string(catalog.Check), string(catalog.Nightly)}

// errCheckRemoved is what the removed `pawl check` says: the gate and the verification each have one command now.
var errCheckRemoved = errors.New(`pawl check was removed: run "pawl run check" (the gate, through make) or "pawl verify" (the verification)`)

// Build identifies the pawl that runs: what `--version` prints and what the generated workflows pin.
type Build struct {
	Version string // the release tag, the module version of a `go install`, or "dev"
	Commit  string // the full commit SHA of a release build, else ""
}

// Execute runs the command line and returns the exit code.
func Execute(build Build, args []string, stdout, stderr io.Writer) int {
	// a cancelled context stops the ring or the install that is running, so Ctrl-C does not leave make behind
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := newRoot(build, stdout, stderr)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		if !errors.Is(err, errFindings) {
			fmt.Fprintln(stderr, "pawl:", err)
		}
		return 1
	}
	return 0
}

// app is what every command shares: the project root from --dir, the output streams and the version.
type app struct {
	dir     string
	version string
	commit  string
	stdout  io.Writer
	stderr  io.Writer
}

func newRoot(build Build, stdout, stderr io.Writer) *cobra.Command {
	a := &app{version: build.Version, commit: build.Commit, stdout: stdout, stderr: stderr}
	root := &cobra.Command{
		Use:           "pawl",
		Short:         "Make a repository's own tooling enforce a quality catalog, one way only",
		Version:       build.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().StringVarP(&a.dir, "dir", "C", ".", "project root")
	root.AddCommand(a.initCmd(), a.installCmd(), a.runCmd(), a.checkCmd(), a.verifyCmd(), a.doctorCmd(), a.statusCmd(), a.baselineCmd(), a.rulesCmd(), a.affectedCmd(), a.budgetCmd(), a.recordCmd())
	return root
}

// emit writes the report in the chosen format and turns error findings into exit code 1.
func (a *app) emit(r *report.Report, format string) error {
	f, err := report.ParseFormat(format)
	if err != nil {
		return err
	}
	r.Tool, r.Version = "pawl", a.version
	if err := r.Write(a.stdout, f); err != nil {
		return err
	}
	if r.Failed() {
		return errFindings
	}
	return nil
}

func (a *app) initCmd() *cobra.Command {
	var stacks []string
	var dry bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write the manifest, the missing tool configurations, the make targets, the hook and the workflows",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			res, err := scaffold.Init(a.dir, scaffold.Options{Stacks: stacks, DryRun: dry, Version: a.version, Commit: a.commit})
			if err != nil {
				return err
			}
			a.printInit(res)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&stacks, "stack", nil, "stacks to set up instead of detecting them (go, typescript, ruby, python, shell, rust)")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "print what would be written")
	return cmd
}

func (a *app) printInit(res *scaffold.Result) {
	fmt.Fprintf(a.stdout, "stacks: %s\n", strings.Join(res.Stacks, ", "))
	for _, f := range res.Created {
		fmt.Fprintf(a.stdout, "  created  %s\n", f)
	}
	for _, f := range res.Merged {
		fmt.Fprintf(a.stdout, "  extended %s\n", f)
	}
	fmt.Fprintf(a.stdout, "  kept     %d existing files\n", len(res.Kept))
	if len(res.Waived) > 0 {
		fmt.Fprintf(a.stdout, "\n%d rules are waived by default; each is a decision for this project (`pawl rules --waived`).\n", len(res.Waived))
	}
	if len(res.Hints) > 0 {
		fmt.Fprintln(a.stdout, "\nstill to run:")
		for _, h := range res.Hints {
			fmt.Fprintf(a.stdout, "  %s\n", h)
		}
	}
	fmt.Fprintln(a.stdout, "\nnext: `pawl install` (make and mise when missing, the pinned tools), `pawl run lint`, `pawl baseline` (records the debt of today), `pawl run check`, then commit.")
}

func (a *app) installCmd() *cobra.Command {
	var opts install.Options
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install mise and make when they are missing, the tools pinned in mise.toml and each stack's packages, then run doctor",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project.Load(a.dir)
			if err != nil {
				return err
			}
			opts.Stdout, opts.Stderr = a.stdout, a.stderr
			res, err := install.Run(cmd.Context(), p, opts)
			if err != nil {
				return err
			}
			a.printSteps(res)
			fmt.Fprintln(a.stdout)
			r, err := doctor.Run(p)
			if err != nil {
				return err
			}
			if err := a.emit(r, "text"); err != nil {
				return err
			}
			if res.Failed() {
				return errFindings
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.SkipMise, "no-mise", false, "leave a machine without mise alone; run only the stacks' own steps")
	cmd.Flags().BoolVar(&opts.SkipMake, "no-make", false, "leave a machine without make alone; do not install make through mise")
	return cmd
}

// printSteps prints one line per install step; a failed step also shows the command that failed.
func (a *app) printSteps(res *install.Result) {
	for _, s := range res.Steps {
		switch {
		case s.Err != nil:
			fmt.Fprintf(a.stdout, "FAILED  %s: %v\n", s.Name, s.Err)
			if len(s.Command) > 0 {
				fmt.Fprintf(a.stdout, "        $ %s\n", strings.Join(s.Command, " "))
			}
		case s.Skipped != "":
			fmt.Fprintf(a.stdout, "skipped %s: %s\n", s.Name, s.Skipped)
		default:
			fmt.Fprintf(a.stdout, "ok      %s\n", s.Name)
		}
		if s.Note != "" {
			fmt.Fprintf(a.stdout, "        %s\n", s.Note)
		}
	}
}

func (a *app) runCmd() *cobra.Command {
	var opts ringrun.Options
	cmd := &cobra.Command{
		Use:   "run <target>",
		Short: "Run a make target (the rings " + strings.Join(ringNames, ", ") + ", or lint, fmt, test, …) with the pinned tools on PATH and quiet output",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("pawl run takes one make target: a ring (%s) or any other target of the Makefile", strings.Join(ringNames, ", "))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runRing(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().BoolVar(&opts.Verbose, "verbose", false, "stream make's own output instead of the summary")
	cmd.Flags().BoolVar(&opts.NoToolbox, "no-toolbox", false, "run make on this machine even when git config pawl.toolbox names an image")
	return cmd
}

// checkCmd is the removed `pawl check`, hidden: it only says which command replaces it, whatever the arguments.
func (a *app) checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "check",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE:               func(*cobra.Command, []string) error { return errCheckRemoved },
	}
}

// runRing runs one ring with the project's pinned tools on PATH; a failing ring is exit code 1 without an error
// line, since the runner already printed what failed.
func (a *app) runRing(ctx context.Context, ring string, opts ringrun.Options) error {
	p, err := project.Load(a.dir)
	if err != nil {
		return err
	}
	env, err := toolenv.Environ(ctx, p.Root)
	if err != nil {
		return err
	}
	opts.Environ, opts.Stdout, opts.Stderr = env, a.stdout, a.stderr
	passed, err := ringrun.Run(ctx, p, ring, opts)
	if err != nil {
		return err
	}
	if !passed {
		return errFindings
	}
	return nil
}

func (a *app) verifyCmd() *cobra.Command {
	var ring, format string
	var only []string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify the catalog: manifest, targets, tool configurations, text checks and the ratchet",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.verify(ring, only, format)
		},
	}
	cmd.Flags().StringVar(&ring, "ring", "check", "precommit (staged files, fast), check, or nightly")
	cmd.Flags().StringSliceVar(&only, "only", nil, "limit the run to these rule ids")
	cmd.Flags().StringVar(&format, "format", "text", "text, json, sarif or github")
	return cmd
}

// verify runs the catalog verification and writes its report.
func (a *app) verify(ring string, only []string, format string) error {
	p, err := project.Load(a.dir)
	if err != nil {
		return err
	}
	r, err := checks.Run(p, checks.Options{Ring: ring, Only: only})
	if err != nil {
		return err
	}
	return a.emit(r, format)
}

func (a *app) doctorCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Verify that every tool is installed at its pinned version and the hook is active",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			p, err := project.Load(a.dir)
			if err != nil {
				return err
			}
			r, err := doctor.Run(p)
			if err != nil {
				return err
			}
			return a.emit(r, format)
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "text, json, sarif or github")
	return cmd
}

func (a *app) statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the failing checks of this branch's pull request and the command that fixes each",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			p, err := project.Load(a.dir)
			if err != nil {
				return err
			}
			failed, err := ghx.Status(p, a.stdout)
			if err != nil {
				return err
			}
			if failed {
				return errFindings
			}
			return nil
		},
	}
}

func (a *app) baselineCmd() *cobra.Command {
	var grow string
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Record the current ratchet values; they may only shrink (--grow \"reason\" is the one way up)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			p, err := project.Load(a.dir)
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
				if err := a.recordGrowth(next, prev, cur, grow); err != nil {
					return err
				}
			}
			if err := ratchet.Write(p.Root, next); err != nil {
				return err
			}
			a.printBaseline(next)
			return nil
		},
	}
	cmd.Flags().StringVar(&grow, "grow", "", "reason for raising a value (kept in the baseline)")
	return cmd
}

// recordGrowth refuses the values that rose since prev unless grow gives a reason; with one, it records the growth
// in next.
func (a *app) recordGrowth(next, prev *ratchet.Baseline, cur *ratchet.Current, grow string) error {
	up := ratchet.Regressions(prev, cur)
	if len(up) == 0 {
		return nil
	}
	if len(strings.Fields(grow)) < 3 {
		for _, d := range up {
			fmt.Fprintf(a.stderr, "%s rose from %d to %d\n", d.Metric, d.Baseline, d.Current)
		}
		return errors.New("the baseline only shrinks; fix the regressions, or record them with --grow \"reason\"")
	}
	g := ratchet.Growth{Date: time.Now().UTC().Format("2006-01-02"), Reason: grow, From: map[string]int{}, To: map[string]int{}}
	for _, d := range up {
		g.From[d.Metric], g.To[d.Metric] = d.Baseline, d.Current
	}
	next.Growth = append(next.Growth, g)
	return nil
}

func (a *app) printBaseline(next *ratchet.Baseline) {
	keys := make([]string, 0, len(next.Metrics))
	for k := range next.Metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(a.stdout, "%-24s %d\n", k, next.Metrics[k])
	}
	fmt.Fprintf(a.stdout, "wrote %s\n", ratchet.FileName)
}

func (a *app) rulesCmd() *cobra.Command {
	var waivedOnly bool
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "List the catalog with what this project does about each rule",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			p, err := project.Load(a.dir)
			if err != nil {
				return err
			}
			for _, class := range p.Catalog.Classes() {
				a.printClass(p, class, waivedOnly)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&waivedOnly, "waived", false, "only the waived rules a tool could enforce")
	cmd.AddCommand(a.rulesShowCmd(), a.rulesSyncCmd())
	return cmd
}

// printClass lists the rules of one class with their state; the class heading is printed only above a listed rule.
func (a *app) printClass(p *project.Project, class catalog.Class, waivedOnly bool) {
	printed := false
	for _, r := range p.Catalog.ByClass(class) {
		e, ok := p.Manifest.Rules[r.ID]
		if waivedOnly && (!ok || e.Waived == "" || r.IsReview()) {
			continue
		}
		if !printed {
			fmt.Fprintf(a.stdout, "\n%s\n", strings.ToUpper(string(class)))
			printed = true
		}
		fmt.Fprintf(a.stdout, "  %-28s %s\n  %-28s %s\n", r.ID, r.Title, "", ruleState(e, ok))
		for _, g := range e.Gaps {
			fmt.Fprintf(a.stdout, "  %-28s gap: %s\n", "", g)
		}
	}
}

// ruleState says what the project does about a rule; ok is false when the manifest has no entry for it.
func ruleState(e manifest.Rule, ok bool) string {
	switch {
	case ok && e.Step != "":
		state := "make " + e.Step
		if len(e.Via) > 0 {
			state += "  (" + strings.Join(e.Via, ", ") + ")"
		}
		return state
	case ok && e.Waived != "":
		return "waived: " + e.Waived
	}
	return "MISSING"
}

func (a *app) rulesShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <rule-id>",
		Short: "Explain one rule: statement, class, ring, and this project's enforcement",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p, err := project.Load(a.dir)
			if err != nil {
				return err
			}
			r, ok := p.Catalog.Get(args[0])
			if !ok {
				return fmt.Errorf("no rule %q; `pawl rules` lists them", args[0])
			}
			a.printRule(r)
			if e, ok := p.Manifest.Rules[r.ID]; ok {
				if e.Step != "" {
					fmt.Fprintf(a.stdout, "\nhere: make %s via %s\n", e.Step, strings.Join(e.Via, ", "))
				} else {
					fmt.Fprintf(a.stdout, "\nhere: waived: %s\n", e.Waived)
				}
			}
			return nil
		},
	}
}

// printRule prints what the catalog says about a rule, before what this project does about it.
func (a *app) printRule(r catalog.Rule) {
	fmt.Fprintf(a.stdout, "%s: %s\nclass: %s", r.ID, r.Title, r.Class)
	if r.Ring != "" {
		fmt.Fprintf(a.stdout, "   ring: %s", r.Ring)
	}
	fmt.Fprintf(a.stdout, "\n\n%s\n", r.Statement)
	if r.Guidance != "" {
		fmt.Fprintf(a.stdout, "\n%s\n", r.Guidance)
	}
	for _, s := range r.Source {
		fmt.Fprintf(a.stdout, "\nsource: %s@%s %s (%s, modified)\n", s.Repo, s.Ref[:min(7, len(s.Ref))], s.Path, s.License)
	}
}

func (a *app) rulesSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Refresh the ast-grep rule pack and the AGENTS.md block from the manifest",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			p, err := project.Load(a.dir)
			if err != nil {
				return err
			}
			updated, err := scaffold.SyncRules(p)
			if err != nil {
				return err
			}
			for _, f := range updated {
				fmt.Fprintf(a.stdout, "  wrote %s\n", f)
			}
			changed, err := scaffold.SyncAgents(p)
			if err != nil {
				return err
			}
			if changed {
				fmt.Fprintln(a.stdout, "  wrote AGENTS.md")
			}
			if len(updated) == 0 && !changed {
				fmt.Fprintln(a.stdout, "up to date")
			}
			return nil
		},
	}
}

func (a *app) affectedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "affected <stack>",
		Short: "Print the test scope of the change against the base for one stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p, err := project.Load(a.dir)
			if err != nil {
				return err
			}
			scope, err := affected.Scope(p, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, scope)
			return nil
		},
	}
}

func (a *app) budgetCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "budget",
		Short: "Compare the median time of each ring with its target and limit",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			p, err := project.Load(a.dir)
			if err != nil {
				return err
			}
			r, err := rings.Budget(p)
			if err != nil {
				return err
			}
			return a.emit(r, format)
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "text, json, sarif or github")
	return cmd
}

func (a *app) recordCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "record <ring> <seconds>",
		Short:  "Store one ring run's wall time (pawl run records its own rings; this is for hooks of your own)",
		Args:   cobra.ExactArgs(2),
		Hidden: true,
		RunE: func(_ *cobra.Command, args []string) error {
			secs, err := strconv.ParseFloat(args[1], 64)
			if err != nil || secs < 0 {
				return fmt.Errorf("seconds must be a non-negative number, got %q", args[1])
			}
			if !manifest.Exists(a.dir) {
				return manifest.ErrNoManifest
			}
			return rings.Record(a.dir, args[0], time.Duration(secs*float64(time.Second)))
		},
	}
}

// ResolveVersion returns the version to report: the one the release build stamped, else the module version that
// `go install module@version` records, else "dev".
func ResolveVersion(stamped, module string) string {
	if stamped != "" && stamped != "dev" {
		return stamped
	}
	if module != "" && module != "(devel)" {
		return module
	}
	return "dev"
}
