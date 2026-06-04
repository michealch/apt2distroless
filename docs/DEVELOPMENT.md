# Development

> All `make` targets run inside Docker. Host prerequisites: **Docker + GNU make only** —
> no Go toolchain, no apt/dpkg.

```bash
# First-time setup: build the dev image (~2 min, cached after)
make build

# Run all hermetic tests (unit + integration; no live apt/dpkg)
make test

# Tier-2 smoke test: installs a real package, runs the real binary against
# live apt-cache/dpkg (root + network, inside Docker)
make e2e

# Interactive shell inside the dev container (delve available)
make shell

# Lint
make lint

# Reproducibility check
make reproducibility-check

# Build the release image locally
make image

# Remove build artefacts
make clean
```

The first `make test` downloads Go module dependencies into a named Docker volume (`gomod-cache`). Subsequent runs use the warm cache and are fast.

## Project structure

```
apt2distroless/
├── cmd/apt2distroless/      # cobra entrypoint
├── internal/
│   ├── config/                 # flag parsing, §5.1 argument grammar
│   ├── dpkg/                   # status reader, FileLister interface, arch, depends parser
│   ├── resolver/               # Resolver interface + apt-cache implementation
│   ├── blacklist/              # built-in list, file override, broken-edge warnings
│   ├── exclude/                # path-prefix matcher with copyright carve-out
│   ├── copier/                 # parallel fidelity copy (mode/uid/gid/xattr/mtime)
│   ├── dedup/                  # size→sha256 hardlink dedup
│   ├── metadata/               # lean status.d emission
│   ├── manifest/               # append-only JSONL outside the rootfs
│   ├── summary/                # Markdown build summary
│   ├── sbom/                   # SPDX 2.3 + CycloneDX 1.5
│   ├── reproducible/           # mtime normalisation, xattr fingerprint
│   ├── run/                    # Pipeline orchestrator + exit codes
│   └── log/                    # structured text/JSON logger
├── test/
│   ├── fixtures/source-root/   # synthetic dpkg filesystem for hermetic tests
│   ├── integration/            # real run.Pipeline + fakes: full flag matrix
│   └── e2e/                    # Tier-2 smoke test (//go:build e2e, real apt/dpkg)
├── docs/                       # HOW_IT_WORKS, USAGE, DEVELOPMENT
├── Dockerfile                  # dev / builder / release (FROM scratch) stages
├── docker-compose.yml          # dev service with bind-mount + gomod volume
├── Makefile                    # Docker-wrapped build targets
├── cliff.toml                  # git-cliff changelog configuration
└── release-please-config.json  # semantic release configuration
```

See [HOW_IT_WORKS.md](HOW_IT_WORKS.md) for a step-by-step walk through the pipeline internals, and [AGENTS.md](../AGENTS.md) for the conventions AI agents (and humans) should follow in this repo.

## Release process

This project uses [Conventional Commits](https://www.conventionalcommits.org/) and an automated release pipeline:

1. Commit to `main` using the conventional format:
   ```
   feat: add --include-suggests flag
   fix: handle dpkg diversions on arm64
   feat!: rename --dedup-strategy skip to none
   ```
2. [`release-please`](https://github.com/googleapis/release-please) automatically opens a Release PR that bumps the version in `.release-please-manifest.json` and updates `CHANGELOG.md`.
3. Merge the Release PR → `release-please` creates a `vX.Y.Z` tag and, in the same workflow run, cuts the release:
   - [`git-cliff`](https://git-cliff.org) generates formatted release notes.
   - Multi-arch image (`linux/amd64`, `linux/arm64`, `linux/386`, `linux/arm/v7`,
     `linux/s390x`) pushed to **both** GHCR (`ghcr.io/michealch/apt2distroless`) and
     Docker Hub (`michealchoudhary/apt2distroless`).
   - Raw binaries (one per arch) + `SHA256SUMS` + `THIRD_PARTY_LICENSES.txt` attached
     to the GitHub Release.

> Docker Hub publishing is optional — it runs only when the repository secrets
> `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` are set; otherwise the image is pushed
> to GHCR only. GHCR uses the built-in `GITHUB_TOKEN`.

### Version bump rules

| Commit type | Version bump |
|---|---|
| `fix:` | Patch (0.0.**X**) |
| `feat:` | Minor (0.**X**.0) |
| `feat!:` or `BREAKING CHANGE:` | Major (**X**.0.0) |
| `chore:` `ci:` `docs:` `style:` | No bump |
