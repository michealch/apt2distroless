# SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
# SPDX-License-Identifier: MIT

# ──────────────────────────────────────────────────────────────────────────────
# apt2distroless Makefile
#
# OUTER targets (run on the host) delegate to Docker.
#   Prerequisites: Docker + GNU make only — no Go required on the host.
#
# INNER targets (prefixed _) run inside the container and are called by the
#   outer targets via $(RUN_DEV).
# ──────────────────────────────────────────────────────────────────────────────

BIN     := apt2distroless
IMAGE   := apt2distroless-dev

# Mirror host UID/GID so bind-mounted files aren't owned by root on the host.
UID     := $(shell id -u)
GID     := $(shell id -g)

# Versioning — injected into the binary via ldflags.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

# Docker Compose run helper — removes the container when done.
RUN_DEV := docker compose run --rm -u $(UID):$(GID) dev

# ── Sentinel file: rebuild the dev image when Dockerfile or compose changes ──
.docker-build: Dockerfile docker-compose.yml
	docker compose build dev
	@touch .docker-build

# ──────────────────────────────────────────────────────────────────────────────
# OUTER targets — host entry points
# ──────────────────────────────────────────────────────────────────────────────

.PHONY: build test e2e lint vet reproducibility-check shell clean image release-image

## build: Compile the binary inside Docker → bin/apt2distroless
build: .docker-build
	$(RUN_DEV) make _build

## test: Run all (hermetic) tests inside Docker
test: .docker-build
	$(RUN_DEV) make _test

## e2e: Run the Tier-2 smoke test (real apt/dpkg) inside Docker as root
##      Installs a real package and runs the binary against the live dpkg DB.
e2e: .docker-build
	docker compose run --rm -u 0:0 dev make _e2e

## lint: Run golangci-lint inside Docker
lint: .docker-build
	$(RUN_DEV) make _lint

## vet: Run go vet inside Docker
vet: .docker-build
	$(RUN_DEV) make _vet

## reproducibility-check: Assert byte-identical output across two runs
reproducibility-check: .docker-build
	$(RUN_DEV) make _reproducibility-check

## shell: Drop into an interactive bash session in the dev container
##        (repo bind-mounted, delve available, go toolchain ready)
shell: .docker-build
	docker compose run --rm -u $(UID):$(GID) dev bash

## image: Build the multi-stage release Docker image locally (for testing)
image:
	docker build \
		--target release \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		-t ghcr.io/michealch/$(BIN):$(VERSION) \
		-t ghcr.io/michealch/$(BIN):latest \
		.

## clean: Remove build artifacts and the sentinel file
clean:
	rm -rf bin/ .docker-build

# ──────────────────────────────────────────────────────────────────────────────
# INNER targets — run inside the container (called by outer targets via RUN_DEV)
# These can also be invoked directly when already inside the container.
# ──────────────────────────────────────────────────────────────────────────────

.PHONY: _build _test _e2e _lint _vet _reproducibility-check

_build:
	@mkdir -p bin
	CGO_ENABLED=0 go build \
		-buildvcs=false \
		-ldflags "$(LDFLAGS)" \
		-o bin/$(BIN) \
		./cmd/apt2distroless

_test:
	go test ./...

_e2e:
	go test -tags e2e -count=1 ./test/e2e/...

_lint:
	golangci-lint run ./...

_vet:
	go vet ./...

_reproducibility-check:
	@echo "=== Reproducibility check ==="
	go test -run TestReproducibility ./test/integration/... -v
	@echo "PASS: reproducibility check passed"

# ──────────────────────────────────────────────────────────────────────────────
# Help — list documented targets
# ──────────────────────────────────────────────────────────────────────────────

.PHONY: help
help:
	@grep -E '^##' Makefile | sed 's/^## //'
