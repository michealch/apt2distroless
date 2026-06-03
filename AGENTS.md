# AGENTS.md — AI Agent Instructions for apt2distroless

This file tells AI coding agents (Claude Code, Copilot, Codex, etc.) how to work
effectively in this repository. Read it before making any changes.

---

## Project summary

`apt2distroless` is a Go CLI tool that extracts a Debian/Ubuntu package and its
transitive runtime dependency closure into a target directory suitable for use as a
distroless container image rootfs.

- **Language:** Go 1.25, `CGO_ENABLED=0` throughout.
- **Module:** `github.com/michealch/apt2distroless`
- **Entry point:** `cmd/apt2distroless/main.go`
- **No cgo. No C dependencies.**

---

## Environment — Docker-first

> **Nothing runs on the host directly.** All build, test, and lint commands run
> inside the Docker dev container. The host only needs Docker + GNU make.

```bash
make build              # compile inside Docker → bin/apt2distroless
make test               # run all hermetic tests inside Docker
make e2e                # Tier-2 smoke test: real apt/dpkg (root + network)
make lint               # golangci-lint inside Docker
make vet                # go vet inside Docker
make reproducibility-check  # assert byte-identical output across two runs
make shell              # interactive bash in the dev container (delve available)
make image              # build the local release image
make clean              # remove bin/ and .docker-build sentinel
```

**Inner targets** (prefixed `_`) are the real Go commands. Run them only when you
are already inside the container (e.g. during `make shell`):

```bash
make _test    # go test ./...
make _build   # go build ...
make _lint    # golangci-lint run ./...
make _vet     # go vet ./...
```

**The first `make test` takes ~2 min** because it builds the dev Docker image and
downloads Go modules into the named volume (`gomod-cache`). Subsequent runs are
fast (warm cache). Do not add a `sleep` or retry loop — just wait.

---

## Running tests

```bash
make test                          # all hermetic packages
make e2e                           # Tier-2 only (real apt/dpkg, root + network)
make shell                         # then inside: go test ./internal/dpkg/...
```

There are **two tiers**:

- **Tier 1 (hermetic, default `make test`).** Unit tests plus `test/integration/`,
  which drives the **real `run.Pipeline`** against the synthetic source root
  (`test/fixtures/source-root/`). The two subprocess seams are injected via
  `run.Deps` — a `resolver.FakeResolver` supplies the closure and a fixture-backed
  `dpkg.FileLister` supplies file lists. **No live `apt-cache`/`dpkg` runs.** This is
  where every flag is exercised end-to-end (`test/integration/flags_test.go`).
- **Tier 2 (`make e2e`, gated by `//go:build e2e`).** `test/e2e/` installs a real
  package and runs the built binary against the live dpkg DB, validating the actual
  `apt-cache`/`dpkg -L` seams that fakes can't. Requires root + network; runs only
  via `make e2e` and the CI Docker job.

Integration tests pin `cfg.Arch = "amd64"` so they behave identically on amd64 and
arm64 Docker hosts. Fixture changes are **additive** — existing packages/paths are
relied upon by tests, so add new ones rather than altering the current entries.

---

## Architecture

```
internal/
  config/       flag parsing + §5.1 argument grammar (pure fn, fully unit-tested)
  dpkg/         status reader, FileLister interface, arch suffix, depends parser
  resolver/     Resolver interface + AptCache implementation (shells to apt-cache)
  blacklist/     built-in list, file override, broken-edge warnings
  exclude/       path-prefix matcher with copyright carve-out
  copier/        parallel fidelity copy (mode/uid/gid/xattr/mtime via Lchown/Lsetxattr)
  dedup/         two-phase size→sha256 hardlink dedup with strict metadata key
  metadata/      lean status.d emission (no maintainer scripts)
  manifest/      append-only JSONL written OUTSIDE the rootfs
  summary/       Markdown build summary
  sbom/          SPDX 2.3 + CycloneDX 1.5 (package-level, deterministic)
  reproducible/  mtime normalisation (epoch), xattr fingerprint
  run/           Pipeline orchestrator + exit code table
  log/           structured text/JSON logger
```

**Pipeline phases** (ordering is critical for reproducibility):

| Phase | What | Parallel? |
|---|---|---|
| A | Copy files + emit metadata | ✅ (`--jobs`) |
| B | Content-dedup (hardlink) | Hashing parallel; winner selection sequential |
| C | Emit manifest / summary / SBOM | Sequential, sorted |
| D | Stamp directory mtimes (bottom-up) | Sequential |

**Phase D must always be last.** Any write into a directory after Phase D runs will
reset that directory's mtime to wall-clock and break byte-identical output.

---

## Key interfaces

Two seams exist specifically to allow testing without a live Debian host:

```go
// internal/resolver
type Resolver interface {
    Closure(roots []string) ([]string, error)
}
// Fake: resolver.FakeResolver{Result: []string{"pkga", "pkgb"}}

// internal/dpkg
type FileLister interface {
    List(pkg string) ([]string, error)
}
// Fake: dpkg.FakeLister{Paths: map[string][]string{"pkga": {"/usr/bin/pkga"}}}
```

These are injected into the pipeline via `run.Deps`:

```go
// internal/run
type Deps struct {
    Resolver resolver.Resolver // nil → real &resolver.AptCache{…}
    Lister   dpkg.FileLister   // nil → real &dpkg.DpkgLister{…}
}
func Pipeline(cfg *config.Config, logger *log.Logger, deps Deps) int

// Production: run.Pipeline(cfg, logger, run.Deps{})  // both nil → real impls
// Tests:      run.Pipeline(cfg, logger, run.Deps{Resolver: fake, Lister: fake})
```

`Pipeline` returns an `int` exit code (the `os.Exit` lives in `main.go`), so tests
assert the returned code directly. Always inject fakes in Tier-1 tests; never call
real `apt-cache`/`dpkg` outside the `e2e`-tagged Tier-2 test.

---

## Conventions

### Commit messages — Conventional Commits (enforced by CI)

```
feat: add --include-suggests flag
fix: handle dpkg diversions on arm64
feat!: rename --dedup-strategy skip to none
docs: update README install instructions
test: add golden test for symlink dedup
refactor: extract xattr copy into reproducible package
```

**Types:** `feat` `fix` `perf` `refactor` `docs` `test` `build` `ci` `chore` `style` `revert`

`ci:` and `chore:` commits do **not** trigger a version bump.
`feat!:` or a commit body containing `BREAKING CHANGE:` triggers a major bump.

### Code style

- Standard `gofmt` formatting — the linter (`golangci-lint`) enforces this.
- No `init()` functions.
- Errors are always propagated up; no `log.Fatal` inside packages.
- All public functions have a one-line doc comment.
- Tests use table-driven style (`[]struct{ name, input, want }`) where there are
  multiple cases.

### Reproducibility rules (do not violate)

1. **Never** read `time.Now()` inside any path that writes to the rootfs.
   `time.Now()` is allowed only in the manifest (provenance, outside the rootfs)
   and in the logger.
2. **Never** iterate over a `map` and emit results without sorting first.
3. **Never** write to a directory after Phase D (`copier.StampDirMTimes`) has run.
4. The dedup winner is always `min(Dst)` lexicographically — never the first
   goroutine to finish.

---

## Adding a new flag

1. Add the field to `internal/config/Config`.
2. Register the flag in `cmd/apt2distroless/main.go` (cobra) — and add it to the
   expected-flags map in `cmd/apt2distroless/main_test.go`.
3. Pass it through to the relevant subsystem in `internal/run/pipeline.go`.
4. **Make it have an effect** — every flag must change output. A flag that is parsed
   but never read is a bug (see how `--sbom-format` was removed). If it changes
   argument parsing, add a case in `internal/config/args_test.go`; if it changes
   `Validate()`, add one in `internal/config/validate_test.go`.
5. Add a behavioural test in `test/integration/flags_test.go` driving the real
   `run.Pipeline` and asserting the on-disk effect / exit code.
6. Update `README.md` (Flags section).

---

## Adding a new internal package

- Place it under `internal/` (not `pkg/` — nothing here is a public library).
- Export the primary type and constructor; keep helpers unexported.
- Add a `_test.go` file with at minimum one table-driven test.
- Wire it into `internal/run/pipeline.go` in the correct phase.

---

## Files agents must NOT modify

| File / pattern | Reason |
|---|---|
| `test/fixtures/source-root/**` (existing entries) | Tests rely on the current packages/paths. **Add** new fixtures; don't alter or remove existing ones. |
| `go.sum` | Updated only by `go mod tidy` inside the container |
| `CHANGELOG.md` | Managed by release-please + git-cliff; do not hand-edit |
| `.release-please-manifest.json` | Managed by release-please; do not hand-edit |
| `cliff.toml` | Only change if the changelog format needs deliberate redesign |

---

## Files that are deliberately generated / ephemeral

| File | What creates it |
|---|---|
| `bin/apt2distroless` | `make build` |
| `.docker-build` | `make build/test/…` (sentinel, gitignored) |
| `RELEASE_NOTES.md` | `release-please.yml` workflow via git-cliff (gitignored) |
| `<target>.manifest.jsonl` | `apt2distroless` at runtime |
| `/tmp/build-summary.md` | `apt2distroless` at runtime |

---

## CI

| Workflow | Trigger | What it checks |
|---|---|---|
| `ci.yml` | Push / PR to `main` | Docker build, vet, tests, reproducibility, **e2e**, lint, cross-compile (amd64, arm64, 386, armv7, s390x) |
| `release-please.yml` | Push to `main` | Opens / updates the Release PR; on merge (when `release_created`) cuts the release: git-cliff notes, **GHCR (+ Docker Hub if configured)** image push, binary release |

> The release jobs run in `release-please.yml` (gated on its `release_created`
> output) rather than a separate `v*`-tag-triggered workflow, because a tag
> pushed by release-please with `GITHUB_TOKEN` does not trigger other workflows.

**Supported architectures:** the raw binary and the container image are built for
`linux/amd64`, `linux/arm64`, `linux/386` (x86), `linux/arm/v7` (ARM 32-bit) and
`linux/s390x`. The Dockerfile `builder` stage cross-compiles via
`--platform=$BUILDPLATFORM` + `TARGETARCH`/`TARGETVARIANT` (the Go compiler runs
natively, no QEMU), and `ci.yml` `go build`-checks every arch on each PR.

> ⚠️ **32-bit gotcha:** `unix.Timespec`/`Stat_t` time fields are `int32` on `386`
> and `arm` but `int64` on 64-bit arches. Use portable helpers (e.g.
> `unix.NsecToTimespec`) instead of `int64` struct literals, or the build breaks on
> 32-bit targets only — see `internal/reproducible`.

CI runs all checks **inside Docker** (same `dev` image used locally). If tests pass
locally with `make test` (and `make e2e`), they will pass in CI.

**Release secrets:** GHCR uses the built-in `GITHUB_TOKEN` (no setup needed).
Docker Hub mirroring is optional — it only runs when the repo secrets
`DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` are set; if absent, the image is
pushed to GHCR only.

**Third-party license notices:** the binary statically links Apache-2.0 (cobra)
and BSD-3-Clause (pflag, `x/sys`) code plus the Go runtime, whose licenses
require attribution in binary redistributions. The Dockerfile `builder` stage
collects them with pinned `go-licenses` into `/licenses` (shipped in the scratch
image), and `release-please.yml`'s `licenses` job attaches `THIRD_PARTY_LICENSES.txt`
to each GitHub Release. The image label is `MIT AND Apache-2.0 AND BSD-3-Clause`.
`ci.yml` runs `go-licenses check` (allow-list MIT/Apache-2.0/BSD-3-Clause/
BSD-2-Clause/ISC) so a copyleft/unknown dependency fails CI.

> **Known issue:** the `golangci-lint` step is currently red because the pinned
> `golangci-lint` (built with go1.24) predates the module's `go 1.25.0` target.
> `make vet` is clean; the lint pin needs bumping (tracked separately).

---

## Debugging inside the container

```bash
make shell
# Now inside the container:
dlv debug ./cmd/apt2distroless -- curl /tmp/test-export
```

The `dev` container has `dlv` (delve) on PATH. The repo is bind-mounted at `/src`.
