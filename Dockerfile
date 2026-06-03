# SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
# SPDX-License-Identifier: MIT

# ── Stage 1: dev ──────────────────────────────────────────────────────────────
# Used both as the interactive developer shell and as the CI build environment.
# Pinned to the build platform: it's a toolchain image (it installs Go for the
# host arch and cross-compiles from there), never a runtime artifact. This also
# keeps the Go-download `case` below seeing only the build arch (amd64/arm64),
# so multi-arch target builds (386, armv7, s390x) don't hit it.
FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS dev

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        make \
        gcc \
        apt \
        dpkg \
    && rm -rf /var/lib/apt/lists/*

# Install Go — pin to a specific version for reproducibility.
ARG GO_VERSION=1.26.4
RUN set -eux; \
    ARCH="$(dpkg --print-architecture)"; \
    case "${ARCH}" in \
        amd64) GOARCH=amd64 ;; \
        arm64) GOARCH=arm64 ;; \
        *) echo "unsupported arch: ${ARCH}" && exit 1 ;; \
    esac; \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" \
        | tar -C /usr/local -xz

ENV PATH="/usr/local/go/bin:/go/bin:$PATH" \
    GOPATH="/go" \
    GOMODCACHE="/go/pkg/mod" \
    CGO_ENABLED=0
# GOCACHE is intentionally NOT set here so `go install` (run as root during
# build) uses /root/.cache/go-build and doesn't pollute /tmp. The runtime
# GOCACHE=/tmp/go-build is injected by docker-compose / CI instead.

# Pre-create Go module cache dir with open permissions so any UID can populate
# it when the named volume is mounted at /go/pkg/mod.
RUN mkdir -p /go/pkg/mod /go/bin && chmod -R 777 /go

# Install delve (Go debugger) — useful inside `make shell`.
RUN go install github.com/go-delve/delve/cmd/dlv@latest

# Install golangci-lint — pinned version for reproducible linting.
ARG LINT_VERSION=v2.11.4
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
        | sh -s -- -b /usr/local/bin "${LINT_VERSION}"

# Install go-licenses — pinned. Used by the builder stage to collect the
# dependency license notices bundled into the image, and by CI to gate the
# dependency license set (permissive-only).
ARG GO_LICENSES_VERSION=v1.6.0
RUN go install github.com/google/go-licenses@${GO_LICENSES_VERSION}

# Make all Go caches world-writable AFTER all go tooling is installed,
# so the named volume (gomod-cache → /go/pkg/mod) is initialised with
# open permissions and any UID (e.g. host user 1000) can write to it.
RUN chmod -R 777 /go

WORKDIR /src

# ── Stage 2: builder ───────────────────────────────────────────────────────────
# Cross-compiles the static binary. Pinned to the build platform via
# `--platform=$BUILDPLATFORM` and handed TARGETOS/TARGETARCH/TARGETVARIANT by
# buildx, so the Go toolchain runs natively and cross-compiles to the target.
# That avoids emulating the compiler under QEMU — essential for fast (and even
# feasible) multi-arch builds like linux/s390x.
FROM --platform=$BUILDPLATFORM dev AS builder

# Cache module downloads as a separate layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none

# Provided automatically by buildx. Default to the build platform's values so a
# plain `docker build` (no buildx target platform) still works natively.
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

RUN set -eux; \
    case "${TARGETVARIANT}" in \
        v7) export GOARM=7 ;; \
        v6) export GOARM=6 ;; \
    esac; \
    mkdir -p /out; \
    CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-amd64}" \
        go build \
            -buildvcs=false \
            -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
            -o /out/apt2distroless \
            ./cmd/apt2distroless

# Collect third-party license notices for the linked dependencies so the scratch
# image (and the raw binaries) can satisfy their Apache-2.0 / BSD-3-Clause
# attribution terms. The set is arch-independent and deterministic (versions are
# pinned by go.sum). go-licenses skips the Go std lib, so add GOROOT/LICENSE too.
RUN set -eux; \
    mkdir -p /out/licenses; \
    cp LICENSE /out/licenses/LICENSE; \
    go-licenses save ./cmd/apt2distroless \
        --save_path=/out/licenses/third-party \
        --ignore github.com/michealch/apt2distroless; \
    cp "$(go env GOROOT)/LICENSE" /out/licenses/third-party/go-LICENSE

# ── Stage: bin-export ─────────────────────────────────────────────────────────
# Minimal stage for extracting the compiled binary via
# `docker build --target bin-export --output type=local,dest=...`. Built on
# scratch so the export is a single file. Exporting the `builder` stage instead
# would also dump the read-only Go module cache (/go/pkg/mod), which breaks
# `type=local` with "mkdir: permission denied".
FROM scratch AS bin-export
COPY --from=builder /out/apt2distroless /apt2distroless

# ── Stage: licenses-export ────────────────────────────────────────────────────
# Exposes only the collected license bundle for
# `docker build --target licenses-export --output type=local,dest=...`, used by
# the release workflow to attach third-party notices to the GitHub Release.
FROM scratch AS licenses-export
COPY --from=builder /out/licenses /

# ── Stage 3: release ──────────────────────────────────────────────────────────
# Final image: the static apt2distroless binary plus the license notices for
# everything bundled into it (project + Go deps + Go runtime).
FROM scratch AS release

COPY --from=builder /out/apt2distroless /usr/local/bin/apt2distroless
# Ship the dependency license notices (OCI/Red Hat convention path) so the image
# satisfies the BSD-3-Clause / Apache-2.0 binary-redistribution attribution terms.
COPY --from=builder /out/licenses /licenses

# Run as non-root by default; users can override with --user root for uid/gid
# preservation (see Plan §7.4).
USER nobody

ENTRYPOINT ["/usr/local/bin/apt2distroless"]
CMD ["--help"]

# The image bundles the static binary, which links code under all three licenses.
LABEL org.opencontainers.image.source="https://github.com/michealch/apt2distroless" \
      org.opencontainers.image.description="Extract a Debian package closure into a distroless container rootfs" \
      org.opencontainers.image.licenses="MIT AND Apache-2.0 AND BSD-3-Clause"
