// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package exclude

import (
	"strings"

	"github.com/michealch/apt2distroless/internal/config"
)

// Matcher holds a sorted list of path prefixes to exclude.
type Matcher struct {
	prefixes []string
}

// Build constructs a Matcher from the user's ExcludeConfig.
func Build(cfg config.ExcludeConfig) Matcher {
	var prefixes []string
	add := func(p string) { prefixes = append(prefixes, p) }

	if cfg.All || cfg.Docs {
		add("/usr/share/doc")
	}
	if cfg.All || cfg.Man {
		add("/usr/share/man")
	}
	if cfg.All || cfg.Info {
		add("/usr/share/info")
	}
	if cfg.All || cfg.Locale {
		add("/usr/share/locale")
	}
	if cfg.All || cfg.Icons {
		add("/usr/share/icons")
		add("/usr/share/pixmaps")
		add("/usr/share/applications")
		add("/usr/share/mime")
	}
	if cfg.All || cfg.Fonts {
		add("/usr/share/fonts")
	}
	if cfg.All || cfg.Cache {
		add("/var/cache")
		add("/tmp")
	}
	for _, p := range cfg.Paths {
		if p != "" {
			add(p)
		}
	}

	// Deduplicate and sort for determinism.
	prefixes = dedupSorted(prefixes)
	return Matcher{prefixes: prefixes}
}

// Excluded reports whether a rootfs-absolute path should be excluded.
// Two carve-outs are always allowed through regardless of prefix matches:
//   - /usr/share/doc/<pkg>/copyright  (legal compliance)
//   - /var/lib/dpkg/**                (metadata we emit ourselves)
func (m Matcher) Excluded(rel string) bool {
	// Carve-outs: copyright files and dpkg metadata always pass.
	if isCopyright(rel) || strings.HasPrefix(rel, "/var/lib/dpkg/") {
		return false
	}
	for _, prefix := range m.prefixes {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	return false
}

// isCopyright reports whether rel is a /usr/share/doc/<pkg>/copyright path.
func isCopyright(rel string) bool {
	if !strings.HasPrefix(rel, "/usr/share/doc/") {
		return false
	}
	rest := rel[len("/usr/share/doc/"):]
	// rest should be "<pkg>/copyright" with no further slashes
	slash := strings.IndexByte(rest, '/')
	return slash >= 0 && rest[slash+1:] == "copyright"
}

func dedupSorted(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	// insertion sort
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
