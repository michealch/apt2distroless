// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package dpkg

import (
	"os/exec"
	"strings"
	"sync"
)

var (
	hostArchOnce  sync.Once
	hostArchValue string
)

// HostArch returns the host dpkg architecture (e.g. "amd64", "arm64").
// Result is cached after the first call.
func HostArch() string {
	hostArchOnce.Do(func() {
		out, err := exec.Command("dpkg", "--print-architecture").Output()
		if err == nil {
			hostArchValue = strings.TrimSpace(string(out))
		} else {
			hostArchValue = "amd64" // safe fallback
		}
	})
	return hostArchValue
}

// InfoFileNames returns the candidate info-file basenames for a package and
// extension, in probe order. It tries both the plain name and the arch-qualified
// form based on the package's Architecture field, killing the Bash `:amd64`
// hardcode.
//
// Examples:
//
//	pkg{Name:"curl", Architecture:"amd64"}, "list"  → ["curl.list", "curl:amd64.list"]
//	pkg{Name:"tzdata", Architecture:"all"},  "md5sums" → ["tzdata.md5sums"]
func InfoFileNames(p *Package, ext string) []string {
	if p.Architecture == "all" || p.Architecture == "" {
		return []string{p.Name + "." + ext}
	}
	return []string{
		p.Name + "." + ext,
		p.Name + ":" + p.Architecture + "." + ext,
	}
}
