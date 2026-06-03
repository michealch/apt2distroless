// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package run

import (
	"fmt"
	"os"
	"strings"

	"github.com/michealch/apt2distroless/internal/blacklist"
	"github.com/michealch/apt2distroless/internal/config"
	"github.com/michealch/apt2distroless/internal/copier"
	"github.com/michealch/apt2distroless/internal/dedup"
	"github.com/michealch/apt2distroless/internal/dpkg"
	"github.com/michealch/apt2distroless/internal/exclude"
	"github.com/michealch/apt2distroless/internal/log"
	"github.com/michealch/apt2distroless/internal/manifest"
	"github.com/michealch/apt2distroless/internal/metadata"
	"github.com/michealch/apt2distroless/internal/resolver"
	"github.com/michealch/apt2distroless/internal/sbom"
	"github.com/michealch/apt2distroless/internal/summary"
)

// Exit codes (Plan §11).
const (
	ExitSuccess       = 0
	ExitUsage         = 1
	ExitInternal      = 2
	ExitNotInstalled  = 3
	ExitTargetError   = 4
	ExitResolverError = 5
	ExitPartialCopy   = 6
	ExitArchNotFound  = 7
)

// Deps holds the injectable subprocess seams. Both fields are nil in production
// (the real apt-cache / dpkg implementations are constructed from cfg). Tests
// inject fakes so the full pipeline can run hermetically against a fixture.
type Deps struct {
	Resolver resolver.Resolver // nil → &resolver.AptCache{…}
	Lister   dpkg.FileLister   // nil → &dpkg.DpkgLister{…}
}

// resolverFor returns the injected resolver or the real apt-cache one.
func resolverFor(cfg *config.Config, ix *dpkg.Index, deps Deps) resolver.Resolver {
	if deps.Resolver != nil {
		return deps.Resolver
	}
	return &resolver.AptCache{
		SourceRoot:        cfg.SourceRoot,
		IncludeRecommends: cfg.IncludeRecommends,
		IncludeSuggests:   cfg.IncludeSuggests,
		Index:             ix,
	}
}

// listerFor returns the injected file lister or the real dpkg one.
func listerFor(cfg *config.Config, deps Deps) dpkg.FileLister {
	if deps.Lister != nil {
		return deps.Lister
	}
	return &dpkg.DpkgLister{SourceRoot: cfg.SourceRoot}
}

// Pipeline executes the full export pipeline and returns an exit code.
func Pipeline(cfg *config.Config, logger *log.Logger, deps Deps) int {
	// --- Pre-flight ---

	// Non-empty target check.
	if err := checkTarget(cfg); err != nil {
		logger.Error("%v", err)
		return ExitTargetError
	}

	// Build blacklist.
	bl, err := blacklist.Build(cfg.Blacklist)
	if err != nil {
		logger.Error("blacklist: %v", err)
		return ExitUsage
	}

	// Build exclude matcher.
	excl := exclude.Build(cfg.Exclude)

	// Read dpkg index.
	ix, err := dpkg.ReadStatus(cfg.SourceRoot)
	if err != nil {
		logger.Error("read dpkg status: %v", err)
		return ExitResolverError
	}

	// Verify root packages are installed.
	for _, root := range cfg.Roots {
		if !ix.Installed(root) {
			logger.Error("package %q is not installed", root)
			return ExitNotInstalled
		}
	}

	// Determine target arch.
	targetArch := cfg.Arch
	if targetArch == "" {
		targetArch = dpkg.HostArch()
	} else {
		// Verify at least one *native* (non-"all") package with that arch is
		// installed. "all" packages match any arch, so counting them would make
		// every --arch value pass; a real target arch needs arch-specific files.
		found := false
		for _, p := range ix.All() {
			if p.Architecture == targetArch {
				found = true
				break
			}
		}
		if !found {
			logger.Error("no native packages with architecture %q are installed", targetArch)
			return ExitArchNotFound
		}
	}

	// Dry-run: resolve and print plan, nothing written.
	if cfg.DryRun {
		return dryRun(cfg, bl, ix, logger, deps)
	}

	// Ensure target exists / handle overwrite.
	if err := prepareTarget(cfg); err != nil {
		logger.Error("prepare target: %v", err)
		return ExitTargetError
	}

	// --- Resolution ---
	res := resolverFor(cfg, ix, deps)

	closure, err := res.Closure(cfg.Roots)
	if err != nil {
		logger.Error("resolver: %v", err)
		return ExitResolverError
	}
	logger.Info("resolved %d packages", len(closure))

	// Separate into copied/blacklisted.
	var toCopy []*dpkg.Package
	var blacklisted []string
	for _, name := range closure {
		if bl.Blocked(name, cfg.Roots) {
			blacklisted = append(blacklisted, name)
			logger.Warn("BLACKLISTED: %s (skipped)", name)
			continue
		}
		p, ok := ix.Get(name)
		if !ok {
			continue
		}
		// Skip packages that don't match target arch (unless Architecture: all).
		if p.Architecture != "all" && p.Architecture != targetArch {
			continue
		}
		toCopy = append(toCopy, p)
	}
	logger.Info("%d packages to copy, %d blacklisted", len(toCopy), len(blacklisted))

	// --- Phase A: parallel copy + metadata ---
	cp := copier.New(cfg.SourceRoot, cfg.Target, cfg.SourceDateEpoch, excl, cfg.Jobs)
	if !cp.IsRoot {
		logger.Warn("not running as root: file ownership and xattrs will not be preserved")
	}

	lister := listerFor(cfg, deps)
	results := cp.CopyPackages(toCopy, lister)

	// Emit metadata (status.d + copyright) — also in Phase A.
	for _, p := range toCopy {
		if err := metadata.Emit(cfg.Target, p, cfg.SourceRoot); err != nil {
			logger.Warn("metadata emit %s: %v", p.Name, err)
		}
	}

	// Aggregate results.
	var allEntries []dpkg.Entry
	var allFailures []dpkg.FileError
	totalFiles := 0
	totalBytes := int64(0)
	totalMissing := 0
	for _, r := range results {
		allEntries = append(allEntries, r.Entries...)
		allFailures = append(allFailures, r.Failures...)
		totalFiles += r.FilesCopied
		totalBytes += r.BytesCopied
		totalMissing += len(r.MissingSources)
	}
	logger.Info("copied %d files (%d bytes)", totalFiles, totalBytes)
	if totalMissing > 0 {
		logger.Warn("%d dpkg-listed path(s) were absent on disk (likely dpkg path-excludes on a slim base) and were skipped", totalMissing)
	}

	// --- Phase B: dedup ---
	if cfg.Deduplicate && cfg.DedupStrategy != "none" {
		dd := &dedup.Deduper{Strategy: cfg.DedupStrategy, Jobs: cfg.Jobs}
		linked, warns, err := dd.Run(allEntries)
		if err != nil {
			logger.Warn("dedup error: %v", err)
		}
		for _, w := range warns {
			logger.Warn("dedup: %s", w)
		}
		if linked > 0 {
			logger.Info("dedup: hardlinked %d files", linked)
		}
	}

	// --- Dangling symlink check ---
	dangling := copier.CheckDanglingSymlinks(cfg.Target)
	for _, d := range dangling {
		logger.Warn("dangling symlink: %s", d)
	}

	// --- Broken-edge warning ---
	copiedNames := make([]string, len(toCopy))
	for i, p := range toCopy {
		copiedNames[i] = p.Name
	}
	brokenEdges := blacklist.BrokenEdges(copiedNames, bl, cfg.Roots, ix)
	for _, e := range brokenEdges {
		logger.Warn("broken dependency: %s depends on blacklisted/excluded %s; may not function", e.From, e.To)
	}

	// --- Phase C: emit manifest, summary, SBOM ---
	copiedPkgs := make([]dpkg.Package, len(toCopy))
	for i, p := range toCopy {
		copiedPkgs[i] = *p
	}

	// Source-distro guardrail: the exported closure is glibc/ABI-tied to the
	// source Debian release, so the final image base MUST be the same release.
	distro := sbom.ReadOSRelease(cfg.SourceRoot)
	if base := distro.RecommendedBase(); base != "" {
		logger.Info("closure built against %s — your final image base MUST be the same Debian release (e.g. %s:nonroot)", distro.Label(), base)
	} else {
		logger.Info("closure built against %s — your final image base MUST be the same Debian release", distro.Label())
	}

	runResult := &manifest.RunResult{
		Roots:              cfg.Roots,
		Arch:               targetArch,
		SourceDistro:       distro.Label(),
		Packages:           copiedPkgs,
		BlacklistedSkipped: blacklisted,
		BrokenEdges:        brokenEdges,
		DanglingSymlinks:   dangling,
		TotalFilesCopied:   totalFiles,
		TotalBytes:         totalBytes,
	}

	if err := manifest.Append(cfg.ManifestPath, runResult); err != nil {
		logger.Warn("manifest: %v", err)
	} else {
		logger.Info("manifest written: %s", cfg.ManifestPath)
	}

	if err := summary.Write(cfg.SummaryPath, runResult); err != nil {
		logger.Warn("summary: %v", err)
	} else {
		logger.Info("summary written: %s", cfg.SummaryPath)
	}

	if cfg.SBOMSpdx != "" {
		if err := sbom.WriteSPDX(cfg.SBOMSpdx, runResult, distro, cfg.SourceDateEpoch); err != nil {
			logger.Warn("SPDX: %v", err)
		} else {
			logger.Info("SPDX SBOM written: %s", cfg.SBOMSpdx)
		}
	}
	if cfg.SBOMCycloneDX != "" {
		if err := sbom.WriteCycloneDX(cfg.SBOMCycloneDX, runResult, distro, cfg.SourceDateEpoch); err != nil {
			logger.Warn("CycloneDX: %v", err)
		} else {
			logger.Info("CycloneDX SBOM written: %s", cfg.SBOMCycloneDX)
		}
	}

	// --- Phase D: directory mtime fixup (bottom-up) ---
	if err := copier.StampDirMTimes(cfg.Target, cfg.SourceDateEpoch); err != nil {
		logger.Warn("dir mtime fixup: %v", err)
	}

	// --- Failure policy ---
	if len(allFailures) > 0 {
		for _, f := range allFailures {
			logger.Error("copy failed: %s: %v", f.Path, f.Err)
		}
		if !cfg.KeepGoing {
			logger.Error("%d file(s) failed to copy (use --keep-going to continue anyway)", len(allFailures))
			return ExitPartialCopy
		}
		logger.Warn("%d file(s) failed (--keep-going: continuing)", len(allFailures))
	}

	return ExitSuccess
}

// dryRun prints a JSON plan to stdout without writing anything.
func dryRun(cfg *config.Config, bl blacklist.Set, ix *dpkg.Index, logger *log.Logger, deps Deps) int {
	res := resolverFor(cfg, ix, deps)
	closure, err := res.Closure(cfg.Roots)
	if err != nil {
		logger.Error("resolver: %v", err)
		return ExitResolverError
	}

	var willCopy, willSkip []string
	for _, name := range closure {
		if bl.Blocked(name, cfg.Roots) {
			willSkip = append(willSkip, name)
		} else {
			willCopy = append(willCopy, name)
		}
	}

	// Emit JSONL plan to stdout.
	fmt.Printf(`{"schema":1,"dry_run":true,"roots":%s,"will_copy":%s,"blacklisted":%s,"total":%d}`+"\n",
		jsonStringArray(cfg.Roots),
		jsonStringArray(willCopy),
		jsonStringArray(willSkip),
		len(closure),
	)
	// Human summary to stderr.
	fmt.Fprintf(os.Stderr, "\n=== Dry Run Plan ===\n")
	fmt.Fprintf(os.Stderr, "Roots:      %s\n", strings.Join(cfg.Roots, ", "))
	fmt.Fprintf(os.Stderr, "To copy:    %d packages\n", len(willCopy))
	fmt.Fprintf(os.Stderr, "Blacklisted: %d packages\n", len(willSkip))
	return ExitSuccess
}

// checkTarget verifies the target isn't non-empty when neither --overwrite nor --merge is set.
func checkTarget(cfg *config.Config) error {
	fi, err := os.Stat(cfg.Target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // target doesn't exist yet — fine
		}
		return fmt.Errorf("stat target %s: %w", cfg.Target, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("target %s exists but is not a directory", cfg.Target)
	}
	entries, err := os.ReadDir(cfg.Target)
	if err != nil {
		return fmt.Errorf("read target %s: %w", cfg.Target, err)
	}
	if len(entries) == 0 || cfg.Merge || cfg.Overwrite {
		return nil
	}
	return fmt.Errorf("target %s is non-empty; use --overwrite (clean) or --merge (append)", cfg.Target)
}

// prepareTarget creates or clears the target directory.
func prepareTarget(cfg *config.Config) error {
	if cfg.Overwrite {
		if err := os.RemoveAll(cfg.Target); err != nil {
			return fmt.Errorf("remove target: %w", err)
		}
	}
	return os.MkdirAll(cfg.Target, 0o755)
}

func jsonStringArray(ss []string) string {
	if len(ss) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range ss {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%q", s)
	}
	b.WriteByte(']')
	return b.String()
}
