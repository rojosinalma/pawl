# Standard targets written by pawl (https://github.com/rojosinalma/pawl). Project-owned: edit freely.
# `make check` is the merge gate; `make precommit` runs from .githooks/pre-commit on staged files.
.DEFAULT_GOAL := check
SHELL := /bin/sh
.PHONY: build toolbox toolbox-lock e2e e2e-project e2e-project-check check fmt fmt-check lint test precommit coverage audit mutation nightly lint-ast-grep lint-jscpd lint-typos lint-gitleaks lint-golangci-lint lint-mod-tidy lint-shellcheck lint-hadolint lint-actionlint lint-zizmor lint-docker-user

# Paths the file-list tools skip, as an extended regular expression; keep it in step with project.exclude in pawl.toml.
PAWL_EXCLUDE ?= ^(test/fixtures/)
PAWL_BASE ?= origin/main
DIFF_COVERAGE ?= 80
# this repository checks itself with the pawl it builds; ./pawl is rebuilt when a source changed (see below)
PAWL ?= ./pawl
STAGED := $(shell git diff --cached --name-only --diff-filter=ACMR)
REPORTS := .pawl/reports
# extra ast-grep flags, for example --globs '!gen/**' to skip generated code
AST_GREP_FLAGS ?= --globs '!test/fixtures/**'
GO_PKGS ?= ./...
GO_TEST_SCOPE ?= $(shell $(PAWL) affected go 2>/dev/null || echo ./...)
STAGED_GO := $(filter %.go,$(STAGED))
# the share of mutants the tests must kill; raise it as the tests improve, never lower it
MUTATION_SCORE ?= 80
SH_FILES := $(shell git ls-files -co --exclude-standard | grep -vE '$(PAWL_EXCLUDE)' | xargs -r shfmt -f 2>/dev/null)
DOCKERFILES := $(shell git ls-files -co --exclude-standard | grep -vE '$(PAWL_EXCLUDE)' | grep -E '(^|/)(Dockerfile(\..*)?|Containerfile|.*\.dockerfile)$$')
WORKFLOWS := $(wildcard .github/workflows/*.yml .github/workflows/*.yaml)
BATS_FILES := $(filter %.bats,$(SH_FILES))
STAGED_SH := $(filter $(SH_FILES),$(STAGED))

check: lint test  ## the merge gate: lint, tests, then the catalog verification
	$(PAWL) check

fmt:  ## rewrite files with the formatters
	golangci-lint fmt $(GO_PKGS)
	@[ -z "$(SH_FILES)" ] || shfmt -w $(SH_FILES)

fmt-check:  ## fail when a file is not formatted
	golangci-lint fmt --diff $(GO_PKGS)
	@[ -z "$(SH_FILES)" ] || shfmt -d $(SH_FILES)

lint: fmt-check lint-ast-grep lint-jscpd lint-typos lint-gitleaks lint-golangci-lint lint-mod-tidy lint-shellcheck lint-hadolint lint-actionlint lint-zizmor lint-docker-user  ## every linter, type checker and pattern rule on the whole tree; `make -k lint` reports every step
	@true

lint-ast-grep:  ## one step of lint
	ast-grep scan -c .pawl/sgconfig.yml $(AST_GREP_FLAGS)

lint-jscpd:  ## one step of lint
	jscpd --silent --reporters json --output $(REPORTS) .

lint-typos:  ## one step of lint
	typos

lint-gitleaks:  ## one step of lint
	gitleaks git . --redact --no-banner --exit-code 1

lint-golangci-lint:  ## one step of lint
	golangci-lint run $(GO_PKGS)

lint-mod-tidy:  ## one step of lint
	go mod tidy -diff

lint-shellcheck:  ## one step of lint
	@[ -z "$(SH_FILES)" ] || shellcheck $(SH_FILES)

lint-hadolint:  ## one step of lint
	@[ -z "$(DOCKERFILES)" ] || hadolint $(DOCKERFILES)

lint-actionlint:  ## one step of lint
	@[ -z "$(WORKFLOWS)" ] || actionlint $(WORKFLOWS)

lint-zizmor:  ## one step of lint
	@[ -z "$(WORKFLOWS)" ] || zizmor --no-progress --min-severity low .github/workflows

lint-docker-user:  ## one step of lint
	@s=0; for f in $(DOCKERFILES); do grep -qiE '^[[:space:]]*USER[[:space:]]' "$$f" || { echo "$$f: no USER instruction, the container runs as root (docker-user)"; s=1; }; done; exit $$s

test:  ## the test suites; PAWL_FULL=1 runs everything, otherwise the affected scope
	go test -shuffle=on -race -count=1 $(GO_TEST_SCOPE)
	@[ -z "$(BATS_FILES)" ] || bats $(BATS_FILES)

precommit:  ## staged files only, under ten seconds; run by the git hook
	@[ -z "$(STAGED)" ] || git diff --cached | gitleaks stdin --redact --no-banner --exit-code 1
	@[ -z "$(STAGED_GO)" ] || golangci-lint fmt --diff $(STAGED_GO)
	@[ -z "$(STAGED_GO)" ] || golangci-lint run --fast-only $(addprefix ./,$(sort $(dir $(STAGED_GO))))
	@[ -z "$(STAGED_SH)" ] || shfmt -d $(STAGED_SH)
	@[ -z "$(STAGED_SH)" ] || shellcheck $(STAGED_SH)
	$(PAWL) check --ring precommit

coverage:  ## tests with coverage; changed lines must reach DIFF_COVERAGE percent
	@mkdir -p $(REPORTS)
	go test -shuffle=on -count=1 -coverpkg=$(GO_PKGS) -coverprofile=$(REPORTS)/go-coverage.out $(GO_PKGS)
	go tool gcov2lcov -infile $(REPORTS)/go-coverage.out -outfile $(REPORTS)/go-coverage.lcov
	diff-cover $(REPORTS)/go-coverage.lcov --compare-branch=$(PAWL_BASE) --fail-under=$(DIFF_COVERAGE)

audit:  ## dependency vulnerability audits
	go tool govulncheck ./...

mutation:  ## mutation testing on the change (nightly)
	gremlins unleash --threshold-efficacy $(MUTATION_SCORE)

nightly: check coverage audit mutation  ## everything, including the slow rings
	$(PAWL) check --ring nightly
	$(PAWL) budget

# --- this repository's own targets ---
TOOLBOX_IMAGE ?= pawl-toolbox:dev
MISE_IMAGE := $(shell sed -n 's/^ARG MISE_IMAGE=//p' toolbox/Dockerfile)

PAWL_SOURCES := $(shell git ls-files cmd internal go.mod go.sum)

pawl: $(PAWL_SOURCES)
	CGO_ENABLED=0 go build -trimpath -o pawl ./cmd/pawl

build: pawl  ## compile pawl into ./pawl

# the rings run the pawl of this commit
check precommit nightly: pawl

toolbox:  ## build the toolbox image (pawl plus every adapter's tools)
	docker build -f toolbox/Dockerfile -t $(TOOLBOX_IMAGE) .

toolbox-lock:  ## regenerate toolbox/mise.lock after editing toolbox/mise.toml
	docker run --rm --entrypoint sh -v "$(CURDIR)/toolbox":/toolbox -w /toolbox -e CI=1 -e RUSTUP_HOME=/opt/rustup -e CARGO_HOME=/opt/cargo $(MISE_IMAGE) -c 'mise install uv python node && mise lock --platform linux-x64,linux-arm64 && chown -R $(shell id -u):$(shell id -g) /toolbox'

e2e:  ## every stack's fixtures with the real tools, inside the toolbox image (run `make toolbox` first)
	docker run --rm -u "$(shell id -u):$(shell id -g)" -e PAWL_E2E_CHECK=1 -v "$(CURDIR)":/work $(TOOLBOX_IMAGE) go test -tags e2e -count=1 ./test/e2e/

E2E_PROJECT ?= .pawl/reports/e2e-project
GIT_FIXTURE := git -c user.name=fixture -c user.email=fixture@example.com -c commit.gpgsign=false

e2e-project: pawl  ## copy the Go fixture into E2E_PROJECT as a new repository and run `pawl init` there
	rm -rf "$(E2E_PROJECT)" && mkdir -p "$(dir $(E2E_PROJECT))" && cp -a test/fixtures/go/pass "$(E2E_PROJECT)"
	cd "$(E2E_PROJECT)" && git init -q -b main && git add -A && $(GIT_FIXTURE) commit -q -m fixture
	cd "$(E2E_PROJECT)" && "$(CURDIR)/pawl" init

e2e-project-check:  ## run the new project's own gate with the tools its mise.toml pins
	cd "$(E2E_PROJECT)" && sh -ec "$$(cat "$(CURDIR)/test/fixtures/go/setup")"
	cd "$(E2E_PROJECT)" && $(MAKE) lint PAWL="$(CURDIR)/pawl"
	cd "$(E2E_PROJECT)" && git add -A && "$(CURDIR)/pawl" baseline
	cd "$(E2E_PROJECT)" && $(MAKE) check PAWL="$(CURDIR)/pawl"
