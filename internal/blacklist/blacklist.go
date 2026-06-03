// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package blacklist

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/michealch/apt2distroless/internal/config"
	"github.com/michealch/apt2distroless/internal/dpkg"
)

// builtIn is the default set of packages that must never appear in a distroless image.
var builtIn = []string{
	"apt", "dpkg", "bash", "dash", "sh", "zsh", "zsh-common",
	"fish", "fish-common", "ksh", "csh", "tcsh", "mksh", "ash", "busybox",
	"login", "passwd", "adduser", "mount", "coreutils", "findutils",
	"grep", "sed", "gawk", "tar", "gzip", "bzip2", "xz-utils",
	"wget", "curl", "perl-base", "python3", "gcc", "g++", "make",
	"sudo", "su", "apt-utils", "debconf",
	"init-system-helpers", "sysvinit-utils", "systemd", "systemd-sysv",
}

// Set is an immutable blacklist set built from the user's config.
type Set struct {
	blocked map[string]struct{}
}

// Build constructs a Set from a BlacklistConfig.
// Precedence: (built-in | file) → removes → adds.
func Build(cfg config.BlacklistConfig) (Set, error) {
	base := make(map[string]struct{})

	if cfg.File != "" {
		// File replaces built-in.
		pkgs, err := readFile(cfg.File)
		if err != nil {
			return Set{}, fmt.Errorf("blacklist-file %s: %w", cfg.File, err)
		}
		for _, p := range pkgs {
			base[p] = struct{}{}
		}
	} else {
		for _, p := range builtIn {
			base[p] = struct{}{}
		}
	}

	// Apply removes first, then adds (add wins ties).
	for _, p := range cfg.Remove {
		delete(base, strings.TrimSpace(p))
	}
	for _, p := range cfg.Add {
		base[strings.TrimSpace(p)] = struct{}{}
	}

	return Set{blocked: base}, nil
}

// Blocked reports whether pkg should be skipped.
// roots are never blocked — an explicit root request beats the blacklist.
func (s Set) Blocked(pkg string, roots []string) bool {
	for _, r := range roots {
		if r == pkg {
			return false
		}
	}
	_, ok := s.blocked[pkg]
	return ok
}

// BrokenEdges returns every (copied→blacklisted/excluded) dependency pair
// where a copied package depends on a blacklisted package. The check uses
// Depends + Pre-Depends from the dpkg Index.
func BrokenEdges(copied []string, blocked Set, roots []string, ix *dpkg.Index) []dpkg.BrokenEdge {
	var edges []dpkg.BrokenEdge
	copiedSet := make(map[string]struct{}, len(copied))
	for _, p := range copied {
		copiedSet[p] = struct{}{}
	}

	for _, name := range copied {
		p, ok := ix.Get(name)
		if !ok {
			continue
		}
		allDeps := append(append([]dpkg.AtomGroup{}, p.Depends...), p.PreDepends...)
		for _, group := range allDeps {
			// For alternatives: warn only if ALL alternatives are blocked.
			// If any alt is satisfied, the dependency is met.
			allBlocked := true
			for _, alt := range group.Alts {
				if !blocked.Blocked(alt.Name, roots) {
					if _, isCopied := copiedSet[alt.Name]; isCopied {
						allBlocked = false
						break
					}
				} else {
					// alt is blocked — keep checking other alts
					continue
				}
				allBlocked = false
				break
			}
			if allBlocked && len(group.Alts) > 0 {
				// Report the first alt as the "missing" dep
				edges = append(edges, dpkg.BrokenEdge{From: name, To: group.Alts[0].Name})
			}
		}
	}
	return edges
}

func readFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var pkgs []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pkgs = append(pkgs, line)
	}
	return pkgs, sc.Err()
}
