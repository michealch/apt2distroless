# Usage

`apt2distroless` is designed to run inside a **builder stage** of a multi-stage
build: a normal Debian image installs your package(s), `apt2distroless` extracts
the closure, and a final **distroless** stage copies it in.

## Dockerfile recipes

### Minimal example — a distroless `curl`

```dockerfile
ARG EXPORT_DIR=/export

# Stage 1: the apt2distroless tool image (FROM scratch — just the static binary).
FROM ghcr.io/michealch/apt2distroless:latest AS tool

# Stage 2: a normal Debian builder that installs the package and runs the export.
FROM debian:13-slim AS builder
ARG EXPORT_DIR=/export
COPY --from=tool / /                       # brings in /usr/local/bin/apt2distroless
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl
RUN /usr/local/bin/apt2distroless --source-root / curl ${EXPORT_DIR}

# Stage 3: the distroless final image — same Debian release as the builder.
FROM gcr.io/distroless/base-debian13:nonroot
ARG EXPORT_DIR=/export
COPY --from=builder ${EXPORT_DIR} /
USER nonroot:nonroot
ENTRYPOINT ["curl"]
```

```bash
docker build -t my-distroless-curl .
docker run --rm my-distroless-curl --version
```

> [!IMPORTANT]
> **The builder base and the distroless final base must be the same Debian release.**
> The exported closure (glibc, libssl, …) is ABI-tied to the release it was built
> against. Mixing releases causes `version 'GLIBC_2.xx' not found` at runtime.
> Use **version-pinned** distroless tags — never the floating `gcr.io/distroless/base:nonroot`.
>
> | Builder base | Final distroless base |
> |---|---|
> | `debian:13-slim` (trixie) | `gcr.io/distroless/base-debian13` |
> | `debian:12-slim` (bookworm) | `gcr.io/distroless/base-debian12` |
>
> `apt2distroless` detects the source distro from `/etc/os-release` and prints it at
> the end of every run (and in the build summary) so you can confirm the match.

### Advanced example — excludes + SBOM in the image

```dockerfile
ARG EXPORT_DIR=/export
FROM ghcr.io/michealch/apt2distroless:latest AS tool
FROM debian:13-slim AS builder
ARG EXPORT_DIR=/export
COPY --from=tool / /
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl
# Strip docs/man/locale/etc, keep Recommends, and ship an SPDX SBOM inside the image.
RUN /usr/local/bin/apt2distroless \
      --source-root / \
      --exclude-all \
      --include-recommends \
      --sbom-spdx ${EXPORT_DIR}/usr/share/sbom/curl.spdx.json \
      curl ${EXPORT_DIR}

FROM gcr.io/distroless/base-debian13:nonroot
ARG EXPORT_DIR=/export
COPY --from=builder ${EXPORT_DIR} /
USER nonroot:nonroot
ENTRYPOINT ["curl"]
```

> [!NOTE]
> On `-slim` bases, dpkg `path-exclude` rules strip changelogs/manpages/lintian
> files, so `dpkg -L` lists paths that aren't on disk. `apt2distroless` skips these
> automatically (a one-line warning), so the build does not fail.

## Command line

```
apt2distroless [FLAGS] <pkg1> [pkg2 ...] <target_folder>
apt2distroless [FLAGS] --root pkg1 --root pkg2 --target /export
```

### Argument grammar

| Form | Meaning |
|---|---|
| `apt2distroless curl /export` | Root = `curl`, target = `/export` |
| `apt2distroless curl nginx /export` | Roots = `{curl, nginx}` (union closure), target = `/export` |
| `--root curl --root nginx --target /export` | Same as above, explicit flags |
| `--root curl /export` | `--root` flag + positional target |

If the last positional looks like an installed package name (contains no `/`), the tool errors with a "looks like a package, not a path" guard rather than silently using it as the target.

### Flags

**Source & target**

| Flag | Default | Description |
|---|---|---|
| `--root` | — | Root package (repeatable; combined with positional roots) |
| `--target` | — | Output directory (alternative to the positional target) |
| `--source-root` | `/` | Read the dpkg DB and copy files from this prefix. Enables export from a mounted image or chroot. |

**Dependency resolution**

| Flag | Default | Description |
|---|---|---|
| `--include-recommends` | `false` | Include `Recommends:` dependencies in the closure |
| `--include-suggests` | `false` | Include `Suggests:` dependencies in the closure |
| `--arch` | host arch | Target architecture; exits 7 if no native package of that arch is installed |

**Blacklist** (never copied; roots are always exempt)

| Flag | Default | Description |
|---|---|---|
| `--blacklist-file` | built-in | Newline-delimited file of packages to skip. **Replaces** the built-in list. |
| `--blacklist-add` | — | Add packages to the blacklist (repeatable; wins ties) |
| `--blacklist-remove` | — | Remove packages from the blacklist (repeatable) |

**Excludes** (path-prefix; `/usr/share/doc/<pkg>/copyright` and `/var/lib/dpkg/**` are always kept)

| Flag | Default | Description |
|---|---|---|
| `--exclude-docs` | `false` | Skip `/usr/share/doc` (except `copyright`) |
| `--exclude-man` | `false` | Skip `/usr/share/man` |
| `--exclude-info` | `false` | Skip `/usr/share/info` |
| `--exclude-locale` | `false` | Skip `/usr/share/locale` |
| `--exclude-icons` | `false` | Skip `/usr/share/{icons,pixmaps,applications,mime}` |
| `--exclude-fonts` | `false` | Skip `/usr/share/fonts` |
| `--exclude-cache` | `false` | Skip `/var/cache` and `/tmp` |
| `--exclude-all` | `false` | Enable all of the above excludes |
| `--exclude-path` | — | Custom exclude prefix (repeatable) |

**Output & SBOM**

| Flag | Default | Description |
|---|---|---|
| `--manifest` | `<target>.manifest.jsonl` | Append-only JSONL provenance log (**outside** the rootfs) |
| `--summary` | `/tmp/build-summary.md` | Markdown build summary (includes the detected source distro) |
| `--sbom-spdx` | — | Emit SPDX 2.3 JSON SBOM to this path |
| `--sbom-cyclonedx` | — | Emit CycloneDX 1.5 JSON SBOM to this path |

**Dedup, reproducibility & behaviour**

| Flag | Default | Description |
|---|---|---|
| `--deduplicate` | `true` | Hardlink identical files (content + metadata key) |
| `--dedup-strategy` | `hardlink` | `hardlink` or `none` |
| `--source-date-epoch` | `$SOURCE_DATE_EPOCH` or `0` | Fixed mtime for reproducibility |
| `--overwrite` | `false` | Wipe non-empty target before export (clean, reproducible) |
| `--merge` | `false` | Merge into a non-empty target (opts into non-determinism) |
| `--keep-going` | `false` | Downgrade genuine per-file copy failures to warnings |
| `--dry-run` | `false` | Print a JSONL plan to stdout, write nothing |
| `--jobs` | NumCPU | Parallel copy/hash workers |
| `--log-level` | `info` | `debug` \| `info` \| `warning` \| `error` |
| `--log-format` | `text` | `text` or `json` |

> Paths that `dpkg -L` lists but which are absent on disk (e.g. changelogs stripped
> by dpkg `path-exclude` on `-slim` bases) are **skipped**, not treated as failures.
> `--keep-going` only affects *genuine* copy errors (permission/I/O/conflict).

### Built-in blacklist

The following package categories are skipped by default and never copied into the output rootfs (shells, compilers, package managers, init systems):

`apt` `dpkg` `bash` `dash` `sh` `zsh` `fish` `ksh` `csh` `tcsh` `mksh` `ash` `busybox` `login` `passwd` `adduser` `mount` `coreutils` `findutils` `grep` `sed` `gawk` `tar` `gzip` `bzip2` `xz-utils` `wget` `curl` `perl-base` `python3` `gcc` `g++` `make` `sudo` `su` `apt-utils` `debconf` `init-system-helpers` `sysvinit-utils` `systemd` `systemd-sysv`

Use `--blacklist-remove <pkg>` to allow a specific package through, or `--blacklist-file` to replace the list entirely.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Usage / invalid arguments |
| 2 | Unexpected internal error |
| 3 | Root package not installed |
| 4 | Target not creatable / not writable |
| 5 | Resolver failure (`apt-cache` or `dpkg` missing / non-zero) |
| 6 | Partial copy failure (strict mode) — use `--keep-going` to downgrade to warnings |
| 7 | Requested `--arch` not installed |

## Reproducibility

The **rootfs tree** is byte-identical for the same `(dpkg DB state, flags, SOURCE_DATE_EPOCH)`, independent of `--jobs`. This covers file contents, modes, uid/gid, xattrs, normalised mtimes, hardlink topology, `status.d` metadata, and any in-image SBOM.

**Not** reproducible by design: the manifest (`<target>.manifest.jsonl`) and build summary carry wall-clock timestamps and live outside the rootfs.

Set `SOURCE_DATE_EPOCH` to a fixed value for fully reproducible builds:

```bash
SOURCE_DATE_EPOCH=0 apt2distroless curl /export
```

## dpkg metadata in the output

The tool writes a lean set of metadata compatible with container image scanners:

```
<target>/
  var/lib/dpkg/status.d/<pkg>           # control stanza (Package: … to blank line)
  var/lib/dpkg/status.d/<pkg>.md5sums   # file checksums
  usr/share/doc/<pkg>/copyright         # licence text (always included)
```

Maintainer scripts (`.postinst`, `.prerm`, …), `.list`, and `.conffiles` are **not** written — they are never used in a distroless image and would only increase attack surface.

## SBOM

Generate an SBOM alongside the export:

```bash
apt2distroless curl /export \
  --sbom-spdx /export-sbom.spdx.json \
  --sbom-cyclonedx /export-sbom.cdx.json
```

- **Format:** package-level SPDX 2.3 JSON + CycloneDX 1.5 JSON.
- **PURL:** derived from `/etc/os-release` → `pkg:deb/debian/curl@8.5.0-1?arch=amd64&distro=trixie`.
- **Licence:** dep5-only mapping (never guesses from prose); falls back to `NOASSERTION`.
- **Deterministic:** `created` timestamp pinned to `SOURCE_DATE_EPOCH`.
