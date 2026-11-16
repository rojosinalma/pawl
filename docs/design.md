# pawl: design

A pawl is the small catch that lets a ratchet turn one way only. pawl makes a repository's own tooling enforce an
abstract quality catalog, and makes the numbers that matter move one way only.

## What it is

1. A **catalog** of abstract rules, each with a check class. The rules say what must hold; they never name a tool.
2. **Adapters** per stack (Go, TypeScript, Ruby, Python, Shell with Docker and GitHub Actions, Rust) that know which
   native tool enforces each rule, how its configuration must look, and how to verify that configuration.
3. A **manifest** (`pawl.toml`) in every project: each catalog rule is either enforced by a named step or waived with a
   reason. `pawl verify` fails when a rule has neither. "Tools over prose" is itself a check.
4. A **ratchet**: a baseline of counts (suppressions, oversized files, duplicated blocks) that may only shrink.
5. **make** as the entry point. `make check`, `make fmt`, `make lint`, `make test`, `make precommit`: the same names in
   every repository, the body per stack, written by `pawl init` and owned by the project afterwards. pawl verifies
   that the targets exist, run, and together enforce the catalog. It never owns them.
6. **Three rings** with time budgets: the git hook (staged files, seconds), the pull request (affected, minutes),
   nightly (everything slow). The agent never waits for a ring above the first.
7. **Prose for what no tool reaches**, generated from the waived rules into the project's `AGENTS.md`, the
   [standard](https://agents.md) place agents read. The budget is small because the tooling carries the rest.

pawl is one static Go binary. The stack tools stay the stack's own (RuboCop, Biome, Ruff, golangci-lint, clippy,
ShellCheck). Cross-stack needs use common tools: ast-grep for patterns, jscpd for duplication, gitleaks for secrets,
typos for identifiers, diff-cover for changed-line coverage. pawl implements only what no tool does: the manifest, the
ratchet, the ring budgets, the configuration verifiers, and a handful of text checks (forbidden names, pins,
suppressions, file size, CI shape).

## The catalog

Every rule has an id (two words, dot separated), a class, a statement, and per stack an implementation or a reason
why none exists. The catalog is embedded in the binary; `pawl rules` prints it with each rule's status in the current
project. Classes:

| Class | Rules | Who checks |
|---|---|---|
| hygiene | `code.format`, `code.lint`, `code.types`, `code.complexity`, `code.file-size`, `code.boundaries`, `code.dead`, `code.dropped-results`, `code.swallowed-errors`, `code.debug-leftovers`, `code.naming` | stack tools; pawl for `file-size` |
| escape hatches | `escape.suppressions`, `escape.duplication`, `escape.test-branch` | pawl (suppressions, ratchet), jscpd, ast-grep |
| hygiene of the repo | `repo.private`, `repo.pinned`, `repo.hook`, `repo.targets`, `repo.ci-shape` | gitleaks + pawl, pawl, pawl, pawl, pawl + actionlint + zizmor |
| resilience | `resilience.timeouts`, `resilience.exhaustive`, `resilience.config-owner`, `resilience.import-cycles`, `resilience.bounded-growth` | stack tools, ast-grep |
| tests | `tests.runner`, `tests.smells`, `tests.diff-coverage`, `tests.mutation`, `tests.mock-edges`, `tests.principles` | stack runners, stack linters, diff-cover, mutation tools, ast-grep, pawl |
| drift | `drift.audit`, `drift.unused-deps`, `drift.contracts`, `drift.docs`, `drift.pr-size`, `drift.security` | stack audit tools, pawl, project extractors, pawl (gh), stack security linters |
| speed | `speed.rings`, `speed.affected` | pawl |
| review | `review.simplest`, `review.abstraction`, `review.naming`, `review.dependency`, `review.domain`, plus the upstream-sourced guidance (`review.consistency`, `review.small-changes`, `review.errors`, `review.global-state`, `review.comments`, and the stack ones) | nobody: these are the generated checklist |

A rule of class `review` is always waived; its reason is the checklist text and names the upstream guide it was
distilled from (repository, commit, section, license, marked as modified as CC BY requires).

## The manifest

`pawl.toml`, written by `pawl init`, owned by the project:

```toml
version = 1
stacks = ["go", "shell"]
base = "origin/main"

[project]
config_module = "internal/config"   # the one place that reads the environment
own_module = "github.com/acme/app"  # what tests must not mock

[budgets]
file_lines = 800
complexity = 15
diff_coverage = 80
[budgets.pr_lines]
feature = 800
bugfix = 200
hotfix = 50
refactor = 800

[rings.precommit]
target = "10s"
limit = "20s"
[rings.check]
target = "10m"
limit = "15m"
[rings.nightly]
target = "20m"
limit = "30m"

[rules."code.format"]
step = "fmt-check"
via = ["gofmt", "goimports"]

[rules."code.types"]
waived = "Ruby has no type checker in this project; Steep is an opt-in"
```

Each rule has exactly one of `step` (a make target reachable from `make check` or `make nightly`, with optional `via`:
the tools and rules that implement it) or `waived` (a reason). In a project with several stacks a rule is enforced
when any stack enforces it; the stacks without a tool for it are listed as `gaps`, each with its reason, so a partial
enforcement is never presented as a whole one.

`pawl verify` verifies: the manifest lists every catalog rule; every step is a make target; every `via` with a known
verifier is really enabled in the tool's configuration (a `.golangci.yml` that disables `errcheck` fails
`code.dropped-results` unless the rule is waived or the project names another tool in `via`); every waiver has a
reason.

## The ratchet

`.pawl/baseline.json` records counts per metric and, where it matters, the offenders:

```json
{ "version": 1, "metrics": { "escape.suppressions": 12, "code.file-size": 2, "escape.duplication": 3 },
  "offenders": { "code.file-size": ["internal/legacy/big.go"] } }
```

`pawl verify` computes the current values and compares them with the baseline at the base commit (the merge base with
`base`; the working-tree file when there is none). A value above the baseline fails ("regression"). A value below fails
too ("stale: run `pawl baseline` to record the improvement"), so the baseline only shrinks. `pawl baseline` writes the
current values; `pawl baseline --grow "<reason>"` is the only way up, and the reason is kept in the file.

Metrics in the first release: suppressions (every `nolint`, `biome-ignore`, `rubocop:disable`, `noqa`,
`type: ignore`, `@ts-ignore`, `@ts-expect-error`, `as any`, `#[allow(...)]`, `shellcheck disable`, `ast-grep-ignore`
counted; each needs a reason after the directive), oversized files (lines over `budgets.file_lines`), duplicated
blocks (jscpd), `TODO` without a link.

## Rings and budgets

| Ring | Runs | Where | Target / limit |
|---|---|---|---|
| precommit | `make precommit`: format staged files, lint staged files, forbidden names, suppressions on staged files | `.githooks/pre-commit` (`core.hooksPath`) | 10 s / 20 s |
| check | `make check`: lint, affected tests, `pawl verify` (without pawl: lint and every test, with a warning) | pull request workflow; `merge_group` when a queue exists | 10 min / 15 min |
| nightly | `make check` with everything, plus mutation, audits, docs drift | nightly workflow on `main`; failures open issues | 20 min / 30 min |

The hook records its wall time locally; the CI rings are read from the last ten completed runs on `main` through the
GitHub API (setup steps excluded). The median above the target warns; above the limit fails `pawl budget`, which the
nightly ring runs. A test that fails is never retried; `pawl` has no retry anywhere.

`pawl affected` prints the test scope for the change against `base`: exact for Go (the import graph of `go list -json`) and Rust (`cargo
metadata`), per changed file for the others, everything when a configuration, lockfile or workflow changed, or when
`PAWL_FULL=1`. The Makefile uses it, with the stack's full scope when pawl is missing or fails:
`GO_TEST_SCOPE ?= $(shell $(PAWL) affected go 2>/dev/null || echo ./...)`.

## Commands

| Command | Does |
|---|---|
| `pawl init` | detect stacks; write `pawl.toml`, the tool configurations that are missing, the make targets that are missing, `.githooks/pre-commit`, `.pawl/` (ast-grep pack, baseline, `generated.json`), the workflows, the `AGENTS.md` block, `.gitignore` lines; print the waived rules to decide on |
| `pawl install` | install mise when the machine lacks it (pinned version, checksum verified), make when the machine lacks it (pinned version, through mise, linked into `~/.local/bin`), the tools pinned in `mise.toml`, and each stack's packages through its own package manager; then the `pawl doctor` report; `--no-mise` and `--no-make` skip the first two |
| `pawl run <target>` | run `make <target>` with the pinned tools on PATH: one line when it passes, the failing steps when it does not; a ring (`precommit`, `check`, `nightly`) also gets its wall time recorded; runs in the toolbox image when `git config pawl.toolbox` names one; `--verbose`, `--no-toolbox` |
| `pawl verify` | the catalog verification: manifest, targets, verifiers, text checks, ratchet; `--ring precommit` limits it to the fast checks on staged files; `--format text\|json\|sarif\|github` |
| `pawl doctor` | every tool pinned and present at the pinned version; hook installed; `make` targets exist; `gh` present |
| `pawl status` | the state of the current branch's pull request: failing check, the first file and line, the command that fixes it |
| `pawl baseline` | record the current ratchet values (shrink); `--grow "reason"` to raise |
| `pawl rules` | the catalog with each rule's status here; `pawl rules show <id>`; `pawl rules sync` brings every rule file Pawl wrote (the tool configurations and the ast-grep pack) up to date where nobody edited it, lists the edited ones, and refreshes the `AGENTS.md` block; `--force <path>` takes Pawl's version of an edited file, `--dry-run` writes nothing (rules only: tool versions are mise's) |
| `pawl affected` | the test scope of the change |
| `pawl budget` | the ring medians against their budgets |
| `pawl record` | stores a ring's wall time (hidden; for hooks of one's own) |

Output: one line per finding, `file:line: rule-id: message`, then the command that fixes it. Exit 0 or 1.

## Adapters

Each adapter answers, for its stack: detection; tools (name, pin file, version command); the configuration files it
writes and the verifier for each rule; the make target bodies; the suppression patterns; the affected-set strategy.

| | Go | TypeScript (Bun) | Ruby | Python | Shell, Docker, Actions | Rust |
|---|---|---|---|---|---|---|
| format | gofmt, goimports | Biome | RuboCop layout | Ruff format | shfmt | rustfmt |
| lint | golangci-lint v2 | Biome, oxlint type-aware | RuboCop + performance | Ruff | ShellCheck, hadolint, actionlint, zizmor | clippy `-D warnings` |
| types | compiler | tsc `--strict` | waived (Steep opt-in) | pyright strict | waived | compiler |
| complexity | gocognit | Biome cognitive | RuboCop Metrics | Ruff C901 | waived | clippy cognitive |
| boundaries | depguard (opt-in) | waived (dependency-cruiser cannot parse TypeScript 7) | packwerk (opt-in) | import-linter (opt-in) | waived | crate layout |
| dead code | unused | knip | waived | vulture | SC2034 | `dead_code` deny |
| dropped results | errcheck | oxlint no-floating-promises | waived | RUF006 | `set -euo pipefail` (ast-grep) | `unused_must_use` deny |
| swallowed errors | errcheck, nilerr | noEmptyBlockStatements | Lint/SuppressedException | BLE001, S110, E722 | waived | `let_underscore_must_use` |
| debug leftovers | forbidigo | noConsole | Lint/Debugger | T20, ERA, TD003 | waived | print_stdout, dbg_macro |
| naming | revive var-naming, typos | useNamingConvention, typos | Naming/*, typos | N, typos | typos | rustc, typos |
| tests | go test -shuffle | bun test | minitest (random) | pytest-randomly | bats (opt-in) | cargo test |
| test smells | testifylint, ast-grep skip | noFocusedTests, noSkippedTests | rubocop-minitest | PT | waived | ast-grep ignore |
| coverage | -coverprofile → diff-cover | --coverage lcov → diff-cover | simplecov → diff-cover | coverage xml → diff-cover | waived | cargo-llvm-cov → diff-cover |
| mutation | gremlins | Stryker | mutant | mutmut | waived | cargo-mutants |
| audit | govulncheck | bun audit | bundler-audit | pip-audit | waived | cargo-audit |
| unused deps | go mod tidy -diff | knip | waived | deptry | waived | cargo-machete |
| security | gosec | ast-grep pack | ast-grep pack | S (bandit) | ShellCheck | cargo-geiger |
| pins | go.mod toolchain | .node-version or engines, bun.lock | .ruby-version, Gemfile.lock | .python-version, uv.lock | mise.toml or tool versions in the workflow | rust-toolchain.toml, Cargo.lock |

"waived" in this table is the adapter's default; the project can enforce it with a tool of its own by editing the
manifest.

## What `pawl init` writes

| File | Owner after init | Purpose |
|---|---|---|
| `pawl.toml` | project | the manifest |
| `Makefile` | project | the five targets plus `fmt-check`, `coverage`; existing targets are never touched |
| tool configurations (`.golangci.yml`, `biome.jsonc`, `.rubocop.yml`, `ruff.toml`, `pyrightconfig.json`, `.shellcheckrc`, `.editorconfig`, `.hadolint.yaml`, `.github/actionlint.yaml`, `.github/zizmor.yml`, `clippy.toml`, `.gitleaks.toml`, `.jscpd.json`, `.typos.toml`) | project; Pawl keeps a file current through `pawl rules sync` until someone edits it | written only when missing |
| `.pawl/sgconfig.yml`, `.pawl/rules/*.yml` | project; Pawl keeps a file current through `pawl rules sync` until someone edits it, and recreates a missing one | the ast-grep pack, templated with `config_module` and `own_module` |
| `.pawl/generated.json` | pawl, committed | the hash of each rule file as Pawl wrote it; how `pawl rules sync` tells an unedited file from an edited one |
| `.pawl/baseline.json` | project, through `pawl baseline` | the ratchet |
| `.githooks/pre-commit` | project | `pawl run precommit` (`make precommit`, timed); plain `make precommit` when pawl is missing |
| `.github/workflows/check.yml`, `nightly.yml`, `review.yml` | project | the rings, with the "Set up Pawl" action pinned and `PAWL_REQUIRED=1`; `review.yml` is `workflow_dispatch` only, off by default |
| `AGENTS.md` | project; one marked block is pawl's | the review checklist from the waived rules, and the three commands an agent needs |
| `.gitignore` | project | `.pawl/reports/`, `AGENTS.local.md` |

## Decisions

1. Go, one static binary, MIT. The common pattern for developer tooling; `go install`, goreleaser, a Homebrew tap.
2. make, not a shared Taskfile or a renderer. The targets are the project's; pawl verifies and never pushes.
3. ast-grep for patterns (one binary, YAML rules, MIT). Semgrep's registry rules are licensed for internal use only,
   which an open-source tool cannot depend on.
4. Each stack's native complexity tool; no meta-linter. jscpd for duplication because no native linter does it.
5. The runtime of a stack is pinned in the stack's native file (`go.mod`, `.ruby-version`, `rust-toolchain.toml`).
   The tools without a native pin (linters shipped as binaries, the cross-stack tools) are pinned in `mise.toml`,
   which `pawl init` writes and `pawl install` (or `mise install`) installs; `pawl doctor` compares what is installed with the pins.
   The toolbox image (`ghcr.io/rojosinalma/pawl-toolbox`) is an optional artifact with every adapter's tools, for
   CI and for machines without them.
6. Shrink-only baseline compared with the base commit, stale baselines fail: the ratchet is strict in both directions.
7. No retries anywhere. A flaky test goes on a quarantine list in the manifest with an issue link, and that list is a
   ratchet metric.
8. AI review is a separate, opt-in workflow template. Never a required check.
9. The agent gate (rules on first edit, lint after edit) belongs to the agent runtime, not to pawl.
10. Budgets: hook 10 s target / 20 s limit, check 10 / 15 min, nightly 20 / 30 min; medians of the last ten runs with
    setup excluded; target warns, limit fails.
11. The merge queue is a repository setting. The generated check workflow triggers on `merge_group` so a queue works
    where the repository has one.
12. `pawl init` never overwrites: a file that exists is kept, a Makefile gains only missing targets, an existing
    `core.hooksPath` stays. What the existing configuration does not enforce shows up in `pawl verify`.
13. This repository's own dependencies are kept current by Dependabot (Go modules, actions, the toolbox base images).
    Dependabot does not read `mise.toml`; those pins are bumped by hand and re-locked.
14. make is the contract, and it works alone: CI, the generated workflows and the Makefile recipes call make, and
    every target runs with mise and make, without pawl installed. pawl is a front end over it that adds quiet
    output, the toolbox fallback and timing; `pawl run check` is the gate through it, and `pawl verify` is the
    catalog verification that `make check` ends with. Every pawl call in a generated recipe goes through the
    `pawl_run` macro, which tests for pawl when the recipe runs (a project may build pawl as a prerequisite): without
    pawl the call is skipped with one warning line, and the test scopes fall back to every test. `PAWL_REQUIRED=1`,
    set by the generated CI workflows, makes a missing pawl an error instead. The hook runs `make precommit` itself
    when pawl is missing. `pawl check`, which named the verification in v0.1 and the gate later, was removed; it
    only names the two commands that replace it.
15. `pawl install` wraps mise and the stacks' package managers rather than installing tools itself. On a machine
    without mise it downloads a pinned mise release and verifies its checksum before it runs it. make and mise
    are Pawl's two hard dependencies, and the only things Pawl installs itself: make has no official binary, so a
    machine without make gets the pinned GNU Make of mise's registry (a conda-forge build; mise verifies it),
    linked into `~/.local/bin`. Every other tool is mise's business.
16. `drift.pr-size` is skipped on the base branch (`main` for `origin/main`): commits there are not a pull request.
17. `pawl run` takes any make target; only the three rings are timed for `pawl budget`.
18. CI gets Pawl through the action of this repository (`action.yml`, "Set up Pawl"): it downloads the release
    binary, checks it against the release's `checksums.txt` and puts it on the PATH. The workflows `pawl init`
    writes pin the action to the commit the running pawl was built from (`uses: rojosinalma/pawl@<sha> # vX.Y.Z`),
    so Dependabot keeps every project current; a build without that commit (a development build, `go install`)
    writes a verified download step instead. The checksum file comes from the same release: it catches a corrupt
    download, not a compromised release.
19. `pawl rules sync` updates a rule file only when nobody edited it, and the unit is the whole file: one edit makes
    the file the project's, and Pawl leaves all of it alone. Merging new settings into an edited YAML, TOML or
    JSONC file is fragile, and a wrong merge silently changes which lint rules run. `.pawl/generated.json`
    records the hash of each file as Pawl wrote it: a file whose hash matches the record is unedited and gets the
    current rendering; any other differing file is kept and listed. `--force <path>` takes Pawl's version of a
    named file anyway. A tool configuration Pawl recorded and the project deleted stays deleted; a missing file
    of the `.pawl/` pack is recreated, since the pack is Pawl's. A project from before the record gets one on its
    first sync: a file identical to the current rendering is adopted, any other counts as edited.

## Known limits

1. The toolbox image is 4.2 GB and built for linux/amd64 only (cargo-mutants has no arm64 binary in the lock).
2. `pawl affected` follows the real dependency graph for Go and Rust. For Python, Ruby and TypeScript it maps a
   changed file to tests by name, and falls back to the whole suite when a shared test helper or a configuration
   file changed; a test that depends on the changed file only indirectly runs in the nightly ring, not before.
   Go: a change to a file pulled in with `//go:embed` selects no package, so a change to embedded files alone
   runs no Go test before the nightly ring. Ruby with RSpec: a change that touches no Ruby file runs every spec.
3. The end-to-end fixtures cover each stack's default path. Not covered by a fixture: Ruby with RSpec, TypeScript on
   Node without Bun, Python without uv.
4. `drift.contracts` checks that a changed contract file comes with a version bump in `pawl.toml`. It does not run
   a breaking-change analysis; a project adds its own (oasdiff, cargo-semver-checks) as a make step.
5. `drift.docs` and `code.boundaries` are waived by default in every stack: both need project knowledge (what the
   documented surface is, which layers exist) that no default can supply.
6. A directive inside a multi-line string literal is counted as a suppression; the string check works per line.
7. Linux and macOS only. The generated recipes and the hook are POSIX shell.
8. The generated Makefile's guard for a missing pawl is tested with GNU make 4.4; make 3.81 (the macOS default)
   supports the constructs it uses, but no test runs it there.
9. `uses: rojosinalma/pawl@<sha>` in a generated workflow is tested through this repository's own e2e workflow
   (the action of the commit, `version: latest`), not from a second repository.
10. Go: a package whose files all carry a build tag stops golangci-lint ("build constraints exclude all Go
    files") when one of its files is staged or linted; list the tag under `run.build-tags` in `.golangci.yml`.
11. In a project without `.pawl/generated.json`, a rule file written by an older Pawl differs from the current
    rendering and has no record, so `pawl rules sync` counts it as edited until `--force <path>` adopts it.
