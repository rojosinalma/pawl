# syntax=docker/dockerfile:1
# The pawl toolbox: pawl plus every tool its stack adapters name, at the versions pinned in toolbox/mise.toml and
# toolbox/mise.lock. Build from the repository root: docker build -f toolbox/Dockerfile -t pawl-toolbox .

ARG MISE_IMAGE=ghcr.io/jdx/mise:2026.11.2-debian@sha256:84bb97455fe918f3b805520f9a7b1fd3060085a4ccc757a3ab17fa2e946df46e
ARG GO_IMAGE=golang:1.25-bookworm@sha256:3b4a11519ad929d1e1d261a12cff056f0c85b735253d7d861346b9c6f8b36437

FROM ${GO_IMAGE} AS pawl
ARG PAWL_VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${PAWL_VERSION}" -o /out/pawl ./cmd/pawl

FROM ${MISE_IMAGE} AS tools
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
ENV CI=1 \
    MISE_TRUSTED_CONFIG_PATHS=/toolbox \
    RUSTUP_HOME=/opt/rustup \
    CARGO_HOME=/opt/cargo
WORKDIR /toolbox
COPY toolbox/mise.toml toolbox/mise.lock ./
COPY toolbox/.mise/ .mise/
RUN mise install --locked \
    && mise env -s bash | grep -E '^export PATH=' | sed 's#/mise/shims:##' > /toolbox/env.sh \
    && mise cache clear

FROM ${MISE_IMAGE}
ARG PAWL_VERSION=dev
LABEL org.opencontainers.image.title="pawl toolbox" \
      org.opencontainers.image.description="pawl and the pinned tools of its stack adapters" \
      org.opencontainers.image.source="https://github.com/rojosinalma/pawl" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${PAWL_VERSION}"
# A compiler and linker for native gems, Go's race detector and Rust; make and git for the rings.
# hadolint ignore=DL3008
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential ca-certificates git libssl-dev libyaml-dev make pkg-config xz-utils zlib1g-dev \
    && rm -rf /var/lib/apt/lists/*
COPY --from=tools /mise /mise
COPY --from=tools /opt/rustup /opt/rustup
COPY --from=tools /opt/cargo /opt/cargo
COPY --from=tools /toolbox/env.sh /toolbox/env.sh
COPY --from=pawl /out/pawl /usr/local/bin/pawl
COPY toolbox/entrypoint /usr/local/bin/toolbox-entrypoint
# Writable homes and caches for any UID, all under /tmp so no tool writes a cache into the mounted project
ENV HOME=/tmp/toolbox/home \
    XDG_CACHE_HOME=/tmp/toolbox/cache \
    XDG_CONFIG_HOME=/tmp/toolbox/config \
    XDG_DATA_HOME=/tmp/toolbox/data \
    GOCACHE=/tmp/toolbox/cache/go \
    GOPATH=/tmp/toolbox/go \
    GOTOOLCHAIN=local \
    GOFLAGS=-buildvcs=false \
    RUSTUP_HOME=/opt/rustup \
    CARGO_HOME=/tmp/toolbox/cargo \
    CARGO_TARGET_DIR=/tmp/toolbox/cargo-target \
    RUFF_CACHE_DIR=/tmp/toolbox/cache/ruff \
    BUNDLE_PATH=/tmp/toolbox/bundle \
    UV_CACHE_DIR=/tmp/toolbox/cache/uv \
    UV_PROJECT_ENVIRONMENT=/tmp/toolbox/venv \
    npm_config_cache=/tmp/toolbox/cache/npm
WORKDIR /work
# nobody by default; `docker run -u "$(id -u):$(id -g)"` matches the owner of the mounted tree instead
USER 65534:65534
ENTRYPOINT ["toolbox-entrypoint"]
CMD ["--help"]
