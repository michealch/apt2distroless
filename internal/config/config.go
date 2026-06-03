// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// BlacklistConfig holds the raw user inputs for blacklist construction.
type BlacklistConfig struct {
	File   string
	Add    []string
	Remove []string
}

// ExcludeConfig holds all the exclude-related flags.
type ExcludeConfig struct {
	Docs, Man, Info, Locale, Icons, Fonts, Cache, All bool
	Paths                                             []string
}

// Config is the fully-parsed runtime configuration.
type Config struct {
	Roots      []string
	Target     string
	SourceRoot string

	DryRun    bool
	LogLevel  string
	LogFormat string

	Blacklist BlacklistConfig
	Exclude   ExcludeConfig

	IncludeRecommends bool
	IncludeSuggests   bool

	Arch string

	Jobs int

	ManifestPath  string
	SummaryPath   string
	SBOMSpdx      string
	SBOMCycloneDX string

	SourceDateEpoch int64

	Deduplicate   bool
	DedupStrategy string // "hardlink" | "none"

	KeepGoing bool
	Overwrite bool
	Merge     bool
}

// resolveArgs applies the §5.1 argument grammar. isInstalled is injected so
// tests can run this without a live dpkg.
func resolveArgs(
	rootFlags []string,
	positionals []string,
	targetFlag string,
	isInstalled func(string) bool,
) (roots []string, target string, err error) {

	switch {
	case targetFlag != "" && len(positionals) == 0 && len(rootFlags) == 0:
		return nil, "", fmt.Errorf("no root packages specified")

	case targetFlag != "":
		// --target given → every positional is a root
		target = targetFlag
		roots = union(rootFlags, positionals)

	case len(positionals) == 0:
		return nil, "", fmt.Errorf("missing required argument: target folder (or use --target)")

	default:
		// last positional is the target
		target = positionals[len(positionals)-1]
		roots = union(rootFlags, positionals[:len(positionals)-1])

		// footgun guard: target contains no "/" AND is an installed package name
		if !strings.Contains(target, "/") && isInstalled != nil && isInstalled(target) {
			return nil, "", fmt.Errorf(
				"%q looks like a package, not a path — did you forget --target? "+
					"pass --target <dir> to disambiguate", target)
		}
	}

	if len(roots) == 0 {
		return nil, "", fmt.Errorf("no root packages specified")
	}

	return roots, target, nil
}

// union deduplicates and sorts two string slices.
func union(a, b []string) []string {
	seen := make(map[string]struct{})
	for _, s := range a {
		seen[s] = struct{}{}
	}
	for _, s := range b {
		seen[s] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sortStrings(out)
	return out
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

// sourceEpoch reads SOURCE_DATE_EPOCH from the environment.
func sourceEpoch(envVal string, flag int64) int64 {
	if flag != 0 {
		return flag
	}
	if envVal != "" {
		if v, err := strconv.ParseInt(strings.TrimSpace(envVal), 10, 64); err == nil {
			return v
		}
	}
	return 0
}

// Validate checks the config after flag parsing and arg resolution.
func (c *Config) Validate() error {
	switch c.DedupStrategy {
	case "hardlink", "none":
	default:
		return fmt.Errorf("unknown --dedup-strategy %q: use hardlink|none", c.DedupStrategy)
	}
	if c.Overwrite && c.Merge {
		return fmt.Errorf("--overwrite and --merge are mutually exclusive")
	}
	return nil
}

// resolveIsInstalled is the real implementation: probe dpkg status.
func resolveIsInstalled(sourceRoot string) func(string) bool {
	return func(name string) bool {
		path := sourceRoot + "/var/lib/dpkg/status"
		data, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		// quick scan — look for "Package: <name>" stanza with status installed
		needle := "\nPackage: " + name + "\n"
		return strings.Contains(string(data), needle)
	}
}

// Apply is called by the cobra RunE handler. It takes the raw parsed flag
// values and the remaining positional args and populates c fully.
func (c *Config) Apply(positionals []string) error {
	roots, target, err := resolveArgs(
		c.Roots,
		positionals,
		c.Target,
		resolveIsInstalled(c.SourceRoot),
	)
	if err != nil {
		return err
	}
	c.Roots = roots
	c.Target = target

	// Normalize source-date-epoch
	c.SourceDateEpoch = sourceEpoch(os.Getenv("SOURCE_DATE_EPOCH"), c.SourceDateEpoch)

	// Default manifest path: sibling of target, outside the rootfs
	if c.ManifestPath == "" {
		c.ManifestPath = c.Target + ".manifest.jsonl"
	}

	return c.Validate()
}
