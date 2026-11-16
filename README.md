# Pawl

### What is Pawl?

Pawl is a multi-stack tool for code quality and structure through configurable abstract rules that are enforced through tooling and automation native to the corresponding [stacks](#stacks).

It uses `mise` to keep tooling updated and `make` to wrap tool calls and workflow control, so you can completely dispose of Pawl and continue working without it, but maintaining stacks with Pawl gives you a central place to control your tooling and rules without worrying about updating every single repo every time a new update comes or if you want to apply the same set of rules across multiple projects.

### Why?

Agents and AI Models take Skills and rules as suggestions if these are not enforced (kinda like humans), so Pawl was born to stop wasting tokens that need to load at every session turn and have quality gates that won't let agents run wild or circumvent what was told to them in prose.

There is an extent that can't be covered by tooling, this should be added to Skills or simple instructions. These are mostly good practices and conventions that need some reasoning evaluator (human or agent), but everything that can be checked and enforced through tooling should be done as such.

### What is a _real_ Pawl?

A [pawl](https://en.wikipedia.org/wiki/Pawl) is the small catch with a gear that lets it turn only one way.

Pawl makes a project own tooling enforce a catalog of rules, and have them only move one way, the positive way.

## What it does

1. **A catalog of abstract rules** (formatting, strict types, complexity, dead code, dropped errors, suppressions,
   timeouts, test quality, dependency audits and more). A rule says what must hold; it never names a tool.
2. **Adapters per stack** that name the tool for each rule and write its configuration: Go, TypeScript, Ruby, Python,
   Rust, and Shell with Docker and GitHub Actions. Each stack keeps its own native tools.
3. **A manifest**, `pawl.toml`: every rule is enforced by a named `make` step or waived with a reason. `pawl verify`
   fails when a rule has neither, and when a tool's configuration no longer enforces what the manifest claims.
4. **A ratchet**: suppressions, oversized files, duplicated blocks and open TODOs are counted in a baseline that may
   only shrink.
5. **make as the entry point**: `make check`, `make fmt`, `make lint`, `make test`, `make precommit`. The targets
   belong to the project; Pawl writes the ones that are missing and verifies the rest.
6. **Three rings with time budgets**: the git hook (staged files, seconds), the pull request (the affected scope,
   minutes), nightly (everything slow).
7. **A short checklist for what no tool reaches**, generated into `AGENTS.md` from the waived rules.

## Install

```sh
brew install rojosinalma/tap/pawl
# or
go install github.com/rojosinalma/pawl/cmd/pawl@latest
```

Binaries for Linux and macOS are on the [releases page](https://github.com/rojosinalma/pawl/releases).

## Use

With Pawl:

```sh
cd your-project
pawl init        # detect the stacks; write pawl.toml, missing tool configs, make targets, the hook, the workflows
pawl install     # mise when the machine lacks it, the pinned tools, each stack's packages; then `pawl doctor`
pawl run lint    # the first run shows what the code owes; fix what you want to fix now
pawl baseline    # record the rest (suppressions, duplicated blocks, oversized files): from here it only shrinks
pawl check       # the merge gate, quietly: one line when it passes, only the failing steps when it does not
```

The same with `mise` and `make`, which is what CI runs:

```sh
mise install     # the pinned tools of the rings (or install them your own way; `pawl doctor` verifies)
make check       # the merge gate: lint, tests, `pawl verify`
```

`make` is the real contract, Pawl is a frontend over it for ease of use.

`pawl run <target>` runs any `make` target (`lint`, `fmt`, `test`, a single `lint-<tool>`) with the pinned tools on
PATH, so a shell without mise activated still works.

`pawl check` is a shorthand for `pawl run check`: it runs `make check` with the pinned tools on PATH,
prints only what failed, records the ring's time, and runs the ring in the toolbox image when
the clone names one.

The generated `mise.toml` pins the tools without a native pin file, the runtime of each stack stays where the
stack keeps it (`go.mod`, `.ruby-version`, `.python-version`, `rust-toolchain.toml`).

`pawl init` never overwrites a file. An existing `Makefile` gains only the targets it lacks; existing linter
configurations stay as they are, and `pawl verify` then reports which catalog rules they do not enforce.

Day to day:

| Command | Purpose |
|---|---|
| `pawl install` | mise when it is missing, the tools pinned in `mise.toml`, each stack's packages; then `pawl doctor` |
| `pawl check` | Shorthand for `make check` through `pawl run`: quiet output, the pinned tools on PATH, the toolbox fallback, timing |
| `pawl run <target>` | a `make` target through Pawl: a complete ring check (`precommit`, `check`, `nightly`) or any other task (`lint`, `fmt`, `test`); `--verbose` streams `make` output, `--no-toolbox` stays local |
| `pawl verify` | the Pawl catalog verification alone (manifest, targets, tool configurations, text checks, ratchet); `make check` ends with it |
| `pawl status` | why the pull request of this branch fails: the check, the first file and line, the fix |
| `pawl rules` | the catalog with what this project does about each rule; `pawl rules show <id>` explains one |
| `pawl baseline` | record improved ratchet values; `--grow "reason"` is the one way up |
| `pawl budget` | the median time of each ring against its target and limit |
| `make check` | lint, the affected tests, and the catalog verification; the same command CI runs |
| `make fmt` | rewrite files with the stack's formatters |

## The manifest

```toml
[rules."code.dropped-results"]
step = "lint"
via = ["golangci-lint/errcheck"]

[rules."code.types"]
step = "lint"
via = ["golangci-lint/typecheck"]
gaps = ["shell: shell has no type checker; ShellCheck covers quoting and expansion"]

[rules."code.boundaries"]
waived = "go: no layer boundaries declared yet; declare them under depguard and set step = \"lint\""
```

A waiver is a decision, explicitly written down. Waived rules that a tool could enforce are listed by `pawl rules --waived`, so you never loose track of the reasons behind waived rules.

## Stacks

| | Go | TypeScript | Ruby | Python | Shell, Docker, Actions | Rust |
|---|---|---|---|---|---|---|
| Format and lint | golangci-lint | Biome, oxlint | RuboCop | Ruff | shfmt, ShellCheck, hadolint, actionlint, zizmor | rustfmt, clippy |
| Types | compiler | tsc strict | opt-in | pyright strict | n/a | compiler |
| Tests | go test, shuffled | bun test | minitest or RSpec, random | pytest, random | bats (opt-in) | cargo test |
| Audit | govulncheck | bun audit | bundler-audit | pip-audit | n/a | cargo-audit, cargo-deny |

Every stack also gets `ast-grep` (pattern rules: the configuration read in one module, bounded collections, skipped
tests without a reason, test-only branches), jscpd (duplication), typos, gitleaks and diff-cover.

## The toolbox image

`ghcr.io/rojosinalma/pawl-toolbox` holds Pawl and every tool the adapters name, at pinned versions, for CI and for
machines without the tools:

```sh
docker run --rm -u "$(id -u):$(id -g)" -v "$PWD":/work ghcr.io/rojosinalma/pawl-toolbox:latest make check
```

A machine without the tools can also run the git hook in the image, set it once per clone:

```sh
git config pawl.toolbox ghcr.io/rojosinalma/pawl-toolbox:latest
```

## Design

[`docs/design.md`](docs/design.md) has the catalog, the manifest format, the ratchet, the rings and the decisions
behind them.

## Development

```sh
make check       # Pawl against its own catalog
make test        # unit tests
make toolbox     # build the toolbox image locally
make e2e         # every stack's fixtures with the real tools, inside the toolbox
```

## License

MIT
