# apt2distroless

**Build distroless container images straight from `apt` packages.** Give it a
package name and `apt2distroless` computes that package's complete transitive
**runtime dependency closure** from the dpkg database, then copies exactly those
files into a `FROM scratch`-ready rootfs — no shells, no package managers,
nothing you didn't ask for.

*Why it isn't trivial:* a binary's real runtime dependencies are dynamically
linked and transitive, they drift as packages are updated, and the source of
truth is spread across dpkg's `status` database and per-package file lists.

## See it in action

A distroless `curl` image, built entirely from apt:

```dockerfile
# 1. the tool — FROM scratch, just the static binary
FROM michealchoudhary/apt2distroless:latest AS tool

# 2. a normal Debian builder — install the package, extract its runtime closure
FROM debian:13-slim AS builder
COPY --from=tool / /
RUN apt-get update && apt-get install -y --no-install-recommends curl
RUN /usr/local/bin/apt2distroless --source-root / curl /export

# 3. the final image — ship only the closure on a matching distroless base
FROM gcr.io/distroless/base-debian13:nonroot
COPY --from=builder /export /
ENTRYPOINT ["curl"]
```

```bash
docker build -t distroless-curl . && docker run --rm distroless-curl --version
```

> The builder base and the final distroless base must be the **same Debian
> release** — the closure is ABI-tied to it. See [Usage](docs/USAGE.md) for the
> details and more recipes (excludes, in-image SBOMs).

## Highlights

- **Reproducible** — byte-identical rootfs for the same inputs (pin `SOURCE_DATE_EPOCH`).
- **Scanner-ready** — emits lean `var/lib/dpkg/status.d/` so Trivy, Grype, Syft and Docker Scout can enumerate packages.
- **SBOMs** — optional SPDX 2.3 and CycloneDX 1.5 output.
- **Minimal & safe** — shells, compilers and package managers are blacklisted by default; identical files are hardlink-deduplicated.
- **Faithful** — preserves mode bits, uid/gid, xattrs (e.g. `security.capability`) and verbatim symlinks.
- **Multi-arch** — amd64, arm64, 386, armv7, s390x.

It does **not** install packages (they must already be present on the source
root) and does **not** reimplement Debian's resolver — it delegates to `apt-cache`.

## Install

```bash
docker pull michealchoudhary/apt2distroless:latest
```

The image is `FROM scratch` — nothing but the single static binary, which runs
inside your Debian builder stage (where `apt`/`dpkg` already live). Prefer a raw
binary? Static Linux builds for amd64, arm64, 386, armv7 and s390x are attached
to every [release](https://github.com/michealch/apt2distroless/releases).

## Documentation

- [**How it works**](docs/HOW_IT_WORKS.md) — the pipeline step by step: resolution, copy, dedup, metadata, SBOM, reproducibility.
- [**Usage**](docs/USAGE.md) — Dockerfile recipes, the full CLI flag reference, the built-in blacklist, and exit codes.
- [**Development**](docs/DEVELOPMENT.md) — building and testing (Docker + make only) and the release process.

## Licence

MIT — see [LICENSE](LICENSE). The repo is [REUSE](https://reuse.software)-compliant.
The published image statically links permissive Go dependencies, so it bundles
their notices under `/licenses` and is labelled with the compound expression
`MIT AND Apache-2.0 AND BSD-3-Clause`.
