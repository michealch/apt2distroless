# How `apt2distroless` works

This document explains what the binary actually does when you run it — the data it
reads, the subprocesses it calls, the order of operations, and why each step exists.
For quick-start usage and the flag reference, see the [README](../README.md).

---

## 1. The mental model

`apt2distroless` does **not** install anything and does **not** build a container.
It is a *file extractor*: given one or more already-installed Debian packages, it
copies that package's **transitive runtime closure** — every file of the package and
all of its dependencies — out of a source filesystem and into a target directory laid
out as a container rootfs.

You run it inside a normal Debian **builder stage**; the target directory is then
`COPY`'d into a `scratch`/distroless final image:

```
┌─ debian:13-slim (builder) ─────────────┐      ┌─ distroless (final) ─┐
│  apt-get install curl                  │      │                      │
│  apt2distroless --source-root / curl X │ ───► │  COPY X / →  /        │
│      │                                 │      │  (just the closure)  │
│      └─ reads /var/lib/dpkg, copies     │      └──────────────────────┘
│         files into X = /export          │
└─────────────────────────────────────────┘
```

Because the closure (glibc, libssl, …) is ABI-bound to the builder's Debian release,
**the final image base must be the same Debian release as the builder** — see the
[release-match note in the README](../README.md#quick-start).

---

## 2. The two subprocess seams

The tool reimplements almost everything in Go, with exactly **two** exceptions where
the Debian tooling is the source of truth and is hard to reproduce correctly:

| Seam | Command | Why not reimplement it |
|---|---|---|
| **Resolver** | `apt-cache depends --recurse …` | Correct handling of alternatives (`a \| b`), virtual packages, and version constraints lives in apt. |
| **File lister** | `dpkg -L <pkg>` | Applies `dpkg-divert` reroutes correctly. One `fork` per package; negligible. |

Everything else — reading `/var/lib/dpkg/status`, copying files with full fidelity,
dedup, metadata, SBOM, reproducibility — is native Go. Both seams are defined as Go
interfaces (`resolver.Resolver`, `dpkg.FileLister`) so tests can inject fakes and run
the whole pipeline without a live Debian host.

---

## 3. The pipeline, end to end

When you run `apt2distroless [flags] <roots…> <target>`, this happens in order:

```
Pre-flight ─► Resolve ─► Filter ─► Phase A ─► Phase B ─► Checks ─► Phase C ─► Phase D
              (apt-cache)          (copy +    (dedup)             (manifest/  (dir
                                    metadata)                      summary/    mtimes)
                                                                   SBOM)
```

### Pre-flight
1. **Target check.** If the target exists and is non-empty, the run aborts (exit 4)
   unless `--overwrite` (wipe first) or `--merge` (append) is given.
2. **Build the blacklist** (built-in list, or `--blacklist-file`, then removes, then adds).
3. **Read `/var/lib/dpkg/status`** from `--source-root` and index every *installed*
   package (name → version, architecture, `Depends`/`Pre-Depends`, the verbatim
   control stanza).
4. **Verify each root is installed** (exit 3 if not).
5. **Resolve the target architecture.** Defaults to the host arch; an explicit
   `--arch` requires at least one *native* (non-`Architecture: all`) package of that
   arch to be installed (exit 7 otherwise).

### Resolution
`apt-cache depends --recurse` is run **once per root** (with `-o Dir=<source-root>`
when not exporting the host `/`). Recommends and Suggests are excluded by default
(`--no-recommends`/`--no-suggests`) unless `--include-recommends`/`--include-suggests`
are set. The per-root results are unioned, virtual `<…>` packages are dropped, and the
set is **intersected with the installed packages** — anything not actually installed is
silently ignored (it was an optional/alternative dependency satisfied elsewhere).

### Filter
Each resolved package is dropped if it is **blacklisted** (roots are always exempt) or
if its architecture is neither `all` nor the target arch. Blacklisted packages are
recorded so they can be reported.

### Phase A — copy + metadata (parallel, `--jobs`)
For every surviving package, `dpkg -L` gives its file list, and each path is copied
from `<source-root>` into `<target>` preserving full fidelity:

| Kind | How it is copied |
|---|---|
| Regular file | `io.Copy` + `chmod` + `Lchown` (if root) + xattrs (`security.*`, `user.*`) + mtime normalised to the epoch |
| Symlink | **Verbatim** — the link text is read and recreated as-is, never chased/resolved |
| Directory | `MkdirAll` + `chmod` + `Lchown`; its mtime is deferred to Phase D |

A path that `dpkg -L` lists but which is **absent on disk** (common on `-slim` bases
where dpkg `path-exclude` strips changelogs/manpages/lintian files) is **skipped with a
warning** — it is *not* a failure. A *genuine* copy error (permission, I/O, a path
conflict) is recorded as a failure.

In the same phase, **lean dpkg metadata** is emitted for each package (see §4).

### Phase B — deduplication (`--deduplicate`, default on)
Identical files are collapsed into hardlinks (see §5).

### Checks (warnings, never fatal)
- **Dangling symlinks** inside the target are reported.
- **Broken dependency edges** — a copied package whose `Depends`/`Pre-Depends` point at
  a blacklisted/excluded package — are reported so you know the closure may be incomplete.

### Phase C — provenance & SBOM (sequential, sorted)
- **Manifest** (`<target>.manifest.jsonl`): an append-only JSONL record written
  **outside** the rootfs (see §6).
- **Build summary** (Markdown): human-readable, includes the detected source distro.
- **SBOM** (optional): SPDX 2.3 JSON and/or CycloneDX 1.5 JSON (see §7).

### Phase D — directory mtime fixup (must be last)
The target is walked **bottom-up** and every directory's mtime is normalised to the
epoch. This is last on purpose: writing anything into a directory updates that
directory's mtime, so any earlier ordering would re-dirty it and break byte-identical
output.

---

## 4. dpkg metadata in the output

The tool writes a deliberately **lean** metadata set — enough for container scanners
(Trivy, Grype, Syft, Docker Scout) to enumerate packages, nothing more:

```
<target>/
  var/lib/dpkg/status.d/<pkg>           # the verbatim control stanza
  var/lib/dpkg/status.d/<pkg>.md5sums   # file checksums (if the source had them)
  usr/share/doc/<pkg>/copyright         # licence text (always kept, even with excludes)
```

It does **not** write maintainer scripts (`.postinst`, `.prerm`, …), `.list`, or
`.conffiles` — these are never used in a distroless image and would only add attack
surface. The `status.d/` layout (one file per package) is the convention distroless
images use instead of a single monolithic `status` file.

---

## 5. Deduplication

Dedup runs in three steps and is content- *and* metadata-aware, so it never merges two
files that merely happen to share bytes:

1. **Bucket by size.** Files with a unique size can't have a duplicate — skip them.
2. **Hash collisions.** For each size-collision group, compute the SHA-256 (in parallel).
3. **Group by a strict key** `(sha256, mode, uid, gid, xattr-fingerprint)`. Within each
   group of ≥2, the **winner is the lexicographically smallest destination path**
   (deterministic, not "whichever goroutine finished first"); the rest are replaced by
   hardlinks to the winner. If the target spans filesystems (`EXDEV`), it falls back to
   a plain copy.

`--dedup-strategy=none` (or `--deduplicate=false`) turns this off entirely.

---

## 6. The manifest (provenance, outside the rootfs)

Each run appends one JSON line to `<target>.manifest.jsonl`, which lives **beside** the
target, never inside it:

```json
{"schema":1,"timestamp":"…","roots":["curl"],"arch":"amd64",
 "source_distro":"debian 13 (trixie)","packages":[{"name":"curl","version":"8.5.0-1"}],
 "blacklisted_skipped":[…],"broken_edges":[…],"dangling_symlinks":[…],
 "total_files_copied":42,"total_bytes":123456}
```

It carries a wall-clock `timestamp` (and `source_distro`), so it is **intentionally not
reproducible** — that's fine because it is provenance metadata that never enters the
image.

---

## 7. SBOM

With `--sbom-spdx <path>` and/or `--sbom-cyclonedx <path>`, the tool emits a
package-level SBOM:

- **Formats:** SPDX 2.3 JSON and CycloneDX 1.5 JSON.
- **PURL:** built from `/etc/os-release`, e.g. `pkg:deb/debian/curl@8.5.0-1?arch=amd64&distro=trixie`.
- **Licence:** read only from a dep5 machine-readable `copyright` file (mapped to SPDX
  identifiers); never guessed from prose — falls back to `NOASSERTION`.
- **Deterministic:** the document namespace is derived from a hash of the package set,
  and the `created` timestamp is pinned to `SOURCE_DATE_EPOCH`, so the SBOM is
  byte-identical across runs. Write it *inside* `${EXPORT_DIR}` to ship it in the image.

---

## 8. Reproducibility

For the same `(dpkg DB state, flags, SOURCE_DATE_EPOCH)`, the **rootfs tree is
byte-identical** across any number of runs and independent of `--jobs`. This covers
file contents, modes, uid/gid, xattrs, normalised mtimes, hardlink topology,
`status.d` metadata, and any in-image SBOM. Four rules make this hold:

1. `time.Now()` is never read on any path that writes into the rootfs (only the manifest
   and logger use wall-clock time).
2. Every map is sorted before its contents are emitted.
3. Nothing is written into a directory after Phase D stamps it.
4. The dedup winner is always the lexicographically smallest path.

Set a fixed epoch for fully reproducible builds: `SOURCE_DATE_EPOCH=0 apt2distroless …`.

---

## 9. Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Usage / invalid arguments |
| 2 | Unexpected internal error |
| 3 | A root package is not installed |
| 4 | Target not creatable / non-empty without `--overwrite`/`--merge` |
| 5 | Resolver failure (`apt-cache`/`dpkg` missing or non-zero) |
| 6 | Partial copy failure (strict) — use `--keep-going` to downgrade to warnings |
| 7 | Requested `--arch` has no native installed package |

---

## 10. Where each step lives in the code

| Package | Responsibility |
|---|---|
| `internal/config` | Flag parsing + the argument grammar (`<roots…> <target>` vs `--root/--target`) |
| `internal/dpkg` | `status` reader, the `FileLister` interface (`dpkg -L`), arch handling, `Depends` parser |
| `internal/resolver` | The `Resolver` interface + the `apt-cache` implementation |
| `internal/blacklist` | Built-in list, file/add/remove precedence, broken-edge detection |
| `internal/exclude` | Path-prefix matcher with the copyright / `var/lib/dpkg` carve-outs |
| `internal/copier` | Phase A fidelity copy + Phase D directory mtime fixup |
| `internal/dedup` | The size → sha256 → metadata-key hardlink dedup |
| `internal/metadata` | `status.d` emission |
| `internal/manifest`, `internal/summary` | Provenance JSONL + Markdown summary |
| `internal/sbom` | SPDX + CycloneDX writers, os-release parsing, PURL/licence |
| `internal/reproducible` | mtime normalisation + xattr fingerprinting |
| `internal/run` | The pipeline orchestrator (`Pipeline`) + exit-code table |

The pipeline is wired with a `run.Deps{Resolver, FileLister}` seam: both are `nil` in
production (the real apt-cache/dpkg implementations are constructed), and tests inject
fakes to run the full pipeline hermetically against a synthetic source root.
