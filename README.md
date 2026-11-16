# pawl

A pawl is the small catch that lets a ratchet turn one way only. `pawl` makes a repository's own tooling enforce a
catalog of quality rules, and makes the numbers that matter move one way only.

It is built for repositories where coding agents do much of the work: rules that live in prose are followed some of
the time; rules that live in tooling are followed every time, at no cost in tokens.

## What it does

1. **A catalog of abstract rules** (formatting, strict types, complexity, dead code, dropped errors, suppressions,
   timeouts, test quality, dependency audits and more). A rule says what must hold; it never names a tool.
2. **Adapters per stack** that name the tool for each rule and write its configuration: Go, TypeScript, Ruby, Python,
   Rust, and Shell with Docker and GitHub Actions. Each stack keeps its own native tools.
3. **A manifest**, `pawl.toml`: every rule is enforced by a named `make` step or waived with a reason. `pawl check`
   fails when a rule has neither, and when a tool's configuration no longer enforces what the manifest claims.
4. **A ratchet**: suppressions, oversized files, duplicated blocks and open TODOs are counted in a baseline that may
   only shrink.
5. **make as the entry point**: `make check`, `make fmt`, `make lint`, `make test`, `make precommit`. The targets
   belong to the project; pawl writes the ones that are missing and verifies the rest.
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

```sh
cd your-project
pawl init        # detect the stacks; write pawl.toml, missing tool configs, make targets, the hook, the workflows
mise install     # the pinned tools of the rings (or install them your own way; `pawl doctor` verifies)
pawl doctor      # every tool present at its pinned version, hook active
make lint        # the first run shows what the code owes; fix what you want to fix now
pawl baseline    # record the rest (suppressions, duplicated blocks, oversized files): from here it only shrinks
make check       # the merge gate: lint, tests, pawl check
```

The generated `mise.toml` pins the tools without a native pin file; the runtime of each stack stays where the
stack keeps it (`go.mod`, `.ruby-version`, `.python-version`, `rust-toolchain.toml`).

`pawl init` never overwrites a file. An existing `Makefile` gains only the targets it lacks; existing linter
configurations stay as they are, and `pawl check` then reports which catalog rules they do not enforce.

Day to day:

| Command | Purpose |
|---|---|
| `make check` | lint, the affected tests, and the catalog verification; the same command CI runs |
| `make fmt` | rewrite files with the stack's formatters |
| `pawl status` | why the pull request of this branch fails: the check, the first file and line, the fix |
| `pawl rules` | the catalog with what this project does about each rule; `pawl rules show <id>` explains one |
| `pawl baseline` | record improved ratchet values; `--grow "reason"` is the one way up |
| `pawl budget` | the median time of each ring against its target and limit |

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

A waiver is a decision, written down. The waived rules a tool could enforce are listed by `pawl rules --waived`.

## Stacks

| | Go | TypeScript | Ruby | Python | Shell, Docker, Actions | Rust |
|---|---|---|---|---|---|---|
| Format and lint | golangci-lint | Biome, oxlint | RuboCop | Ruff | shfmt, ShellCheck, hadolint, actionlint, zizmor | rustfmt, clippy |
| Types | compiler | tsc strict | opt-in | pyright strict | n/a | compiler |
| Tests | go test, shuffled | bun test | minitest or RSpec, random | pytest, random | bats (opt-in) | cargo test |
| Audit | govulncheck | bun audit | bundler-audit | pip-audit | n/a | cargo-audit, cargo-deny |

Every stack also gets ast-grep (pattern rules: the configuration read in one module, bounded collections, skipped
tests without a reason, test-only branches), jscpd (duplication), typos, gitleaks and diff-cover.

## The toolbox image

`ghcr.io/rojosinalma/pawl-toolbox` holds pawl and every tool the adapters name, at pinned versions, for CI and for
machines without the tools:

```sh
docker run --rm -u "$(id -u):$(id -g)" -v "$PWD":/work \
  ghcr.io/rojosinalma/pawl-toolbox:latest make check
```

A machine without the tools can also run the git hook in the image; set it once per clone:

```sh
git config pawl.toolbox ghcr.io/rojosinalma/pawl-toolbox:latest
```

## Design

[`docs/design.md`](docs/design.md) has the catalog, the manifest format, the ratchet, the rings and the decisions
behind them.

## Development

```sh
make check       # pawl against its own catalog
make test        # unit tests
make toolbox     # build the toolbox image locally
make e2e         # every stack's fixtures with the real tools, inside the toolbox
```

## License

MIT
