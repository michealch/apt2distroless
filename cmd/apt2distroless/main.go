// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/michealch/apt2distroless/internal/config"
	"github.com/michealch/apt2distroless/internal/log"
	"github.com/michealch/apt2distroless/internal/run"
	"github.com/spf13/cobra"
)

// set by goreleaser via -ldflags
var (
	version = "dev"
	commit  = "none"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cfg := &config.Config{
		SourceRoot:    "/",
		LogLevel:      "info",
		LogFormat:     "text",
		SummaryPath:   "/tmp/build-summary.md",
		Deduplicate:   true,
		DedupStrategy: "hardlink",
		Jobs:          runtime.NumCPU(),
	}

	cmd := &cobra.Command{
		Use:   "apt2distroless [FLAGS] <pkg1> [pkg2 ...] <target_folder>",
		Short: "Extract a Debian package closure into a distroless container rootfs",
		Long: `apt2distroless extracts a Debian package and all its transitive
runtime dependencies from a host or mounted Debian/Ubuntu system into a
target directory suitable for use as a distroless container image rootfs.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       fmt.Sprintf("%s (commit %s)", version, commit),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.Apply(args); err != nil {
				return err
			}
			lvl, err := log.ParseLevel(cfg.LogLevel)
			if err != nil {
				return err
			}
			logger := log.New(lvl, cfg.LogFormat)

			code := run.Pipeline(cfg, logger, run.Deps{})
			if code != run.ExitSuccess {
				os.Exit(code)
			}
			return nil
		},
	}

	// Root/target flags
	cmd.Flags().StringArrayVar(&cfg.Roots, "root", nil, "Root package (repeatable; combined with positional roots)")
	cmd.Flags().StringVar(&cfg.Target, "target", "", "Output directory")
	cmd.Flags().StringVar(&cfg.SourceRoot, "source-root", cfg.SourceRoot, "Read dpkg DB and copy files from this prefix (enables export from mounted image)")

	// Run control
	cmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Plan only; emit JSONL plan to stdout, nothing written")
	cmd.Flags().StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "Log verbosity: debug|info|warning|error")
	cmd.Flags().StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "Log format: text|json")

	// Blacklist
	cmd.Flags().StringVar(&cfg.Blacklist.File, "blacklist-file", "", "Newline-delimited file; replaces built-in list")
	cmd.Flags().StringArrayVar(&cfg.Blacklist.Add, "blacklist-add", nil, "Add packages to blacklist")
	cmd.Flags().StringArrayVar(&cfg.Blacklist.Remove, "blacklist-remove", nil, "Remove packages from blacklist")

	// Excludes
	cmd.Flags().BoolVar(&cfg.Exclude.Docs, "exclude-docs", false, "Skip /usr/share/doc (except copyright)")
	cmd.Flags().BoolVar(&cfg.Exclude.Man, "exclude-man", false, "Skip /usr/share/man")
	cmd.Flags().BoolVar(&cfg.Exclude.Info, "exclude-info", false, "Skip /usr/share/info")
	cmd.Flags().BoolVar(&cfg.Exclude.Locale, "exclude-locale", false, "Skip /usr/share/locale")
	cmd.Flags().BoolVar(&cfg.Exclude.Icons, "exclude-icons", false, "Skip /usr/share/{icons,pixmaps,applications,mime}")
	cmd.Flags().BoolVar(&cfg.Exclude.Fonts, "exclude-fonts", false, "Skip /usr/share/fonts")
	cmd.Flags().BoolVar(&cfg.Exclude.Cache, "exclude-cache", false, "Skip /var/cache and /tmp")
	cmd.Flags().BoolVar(&cfg.Exclude.All, "exclude-all", false, "Enable all excludes")
	cmd.Flags().StringArrayVar(&cfg.Exclude.Paths, "exclude-path", nil, "Custom exclude prefix (repeatable)")

	// Dependency options
	cmd.Flags().BoolVar(&cfg.IncludeRecommends, "include-recommends", false, "Include Recommends: dependencies")
	cmd.Flags().BoolVar(&cfg.IncludeSuggests, "include-suggests", false, "Include Suggests: dependencies")

	// Architecture
	cmd.Flags().StringVar(&cfg.Arch, "arch", "", "Target architecture (default: host); error if not installed")

	// Concurrency
	cmd.Flags().IntVar(&cfg.Jobs, "jobs", cfg.Jobs, "Parallel copy/hash workers")

	// Output paths
	cmd.Flags().StringVar(&cfg.ManifestPath, "manifest", "", "Manifest file path (default: <target>.manifest.jsonl)")
	cmd.Flags().StringVar(&cfg.SummaryPath, "summary", cfg.SummaryPath, "Build summary path")
	cmd.Flags().StringVar(&cfg.SBOMSpdx, "sbom-spdx", "", "Emit SPDX 2.3 JSON to this path")
	cmd.Flags().StringVar(&cfg.SBOMCycloneDX, "sbom-cyclonedx", "", "Emit CycloneDX 1.5 JSON to this path")

	// Reproducibility
	cmd.Flags().Int64Var(&cfg.SourceDateEpoch, "source-date-epoch", 0, "Fixed mtime for reproducibility (also reads $SOURCE_DATE_EPOCH)")

	// Dedup
	cmd.Flags().BoolVar(&cfg.Deduplicate, "deduplicate", cfg.Deduplicate, "Hardlink identical files")
	cmd.Flags().StringVar(&cfg.DedupStrategy, "dedup-strategy", cfg.DedupStrategy, "Dedup strategy: hardlink|none")

	// Failure policy
	cmd.Flags().BoolVar(&cfg.KeepGoing, "keep-going", false, "Downgrade per-file failures to warnings")
	cmd.Flags().BoolVar(&cfg.Overwrite, "overwrite", false, "Wipe non-empty target before export")
	cmd.Flags().BoolVar(&cfg.Merge, "merge", false, "Merge into non-empty target (opts into non-determinism)")

	// Recover panics and return exit code 2
	cobra.OnInitialize(func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "internal error: %v\n%s\n", r, debug.Stack())
				os.Exit(2)
			}
		}()
	})

	return cmd
}
