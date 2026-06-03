// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package resolver

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/michealch/apt2distroless/internal/dpkg"
)

// Resolver computes the transitive dependency closure for a set of root packages.
// All returned package names are sorted and deduplicated.
type Resolver interface {
	Closure(roots []string) ([]string, error)
}

// AptCache is the production Resolver: it shells to apt-cache for each root
// and unions the results.
type AptCache struct {
	SourceRoot        string
	IncludeRecommends bool
	IncludeSuggests   bool
	Index             *dpkg.Index // to intersect closure with installed packages
}

// Closure runs apt-cache depends --recurse for each root, unions the resulting
// package names, and intersects with the installed set. Returns sorted names.
func (a *AptCache) Closure(roots []string) ([]string, error) {
	seen := make(map[string]struct{})

	for _, root := range roots {
		pkgs, err := a.closureForRoot(root)
		if err != nil {
			return nil, fmt.Errorf("resolver: %w", err)
		}
		for _, p := range pkgs {
			seen[p] = struct{}{}
		}
		// Root always included
		seen[root] = struct{}{}
	}

	// Intersect with installed; warn and drop anything not installed.
	out := make([]string, 0, len(seen))
	for name := range seen {
		if a.Index == nil || a.Index.Installed(name) {
			out = append(out, name)
		}
		// packages not installed are silently dropped (they were optional/elsewhere satisfied)
	}
	sortStrings(out)
	return out, nil
}

func (a *AptCache) closureForRoot(root string) ([]string, error) {
	args := buildAptCacheArgs(root, a.IncludeRecommends, a.IncludeSuggests, a.SourceRoot)

	cmd := exec.Command("apt-cache", args...)

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("apt-cache depends %s: %w\n%s", root, err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("apt-cache depends %s: %w", root, err)
	}

	return parseAptCacheOutput(string(out)), nil
}

// buildAptCacheArgs constructs the argv for `apt-cache depends --recurse` for a
// single root. It is a pure function so the flag plumbing (--include-recommends,
// --include-suggests, --source-root) can be unit-tested without running apt-cache.
//
// When sourceRoot is non-empty and not "/", the args are prefixed with
// "-o Dir=<sourceRoot>" so apt-cache reads the mounted rootfs instead of the host.
func buildAptCacheArgs(root string, inclRecommends, inclSuggests bool, sourceRoot string) []string {
	var args []string
	if sourceRoot != "" && sourceRoot != "/" {
		args = append(args, "-o", "Dir="+sourceRoot)
	}
	args = append(args,
		"depends",
		"--recurse",
		"--no-conflicts",
		"--no-breaks",
		"--no-replaces",
	)
	if !inclRecommends {
		args = append(args, "--no-recommends")
	}
	if !inclSuggests {
		args = append(args, "--no-suggests")
	}
	args = append(args, root)
	return args
}

// parseAptCacheOutput extracts package names from `apt-cache depends --recurse`
// output. The format is:
//
//	pkgname
//	  Depends: dep
//	  |Depends: altdep
//	 <virtual-pkg>
//
// We keep only lines that resolve to a real package name (not virtual or empty).
func parseAptCacheOutput(output string) []string {
	var names []string
	seen := make(map[string]struct{})

	for _, line := range strings.Split(output, "\n") {
		// Strip leading whitespace and pipe markers
		line = strings.TrimLeft(line, " \t|")
		// Strip "Depends:", "Recommends:", etc. prefixes
		if i := strings.Index(line, ":"); i >= 0 {
			line = strings.TrimSpace(line[i+1:])
		}
		// Skip virtual packages (enclosed in <>)
		if strings.HasPrefix(line, "<") {
			continue
		}
		// Strip version constraints (anything with spaces after name)
		if i := strings.IndexByte(line, ' '); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, dup := seen[line]; !dup {
			seen[line] = struct{}{}
			names = append(names, line)
		}
	}
	return names
}

// FakeResolver is a test double that returns a pre-configured closure.
type FakeResolver struct {
	Result []string
	Err    error
}

func (f *FakeResolver) Closure(_ []string) ([]string, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	out := make([]string, len(f.Result))
	copy(out, f.Result)
	sortStrings(out)
	return out, nil
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}
