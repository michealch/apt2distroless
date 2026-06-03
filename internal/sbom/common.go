// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package sbom

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"github.com/michealch/apt2distroless/internal/dpkg"
)

// Distro holds os-release fields needed for PURL construction and the
// Debian-release-match guardrail.
type Distro struct {
	ID          string // e.g. "debian", "ubuntu"
	VersionCode string // e.g. "bookworm", "trixie"
	VersionID   string // e.g. "12", "13"
}

// Label renders a human-readable distro string, e.g. "debian 13 (trixie)".
func (d Distro) Label() string {
	s := d.ID
	if d.VersionID != "" {
		s += " " + d.VersionID
	}
	if d.VersionCode != "" {
		s += " (" + d.VersionCode + ")"
	}
	return s
}

// RecommendedBase returns the distroless base image the final stage should use
// to match this source distro, e.g. "gcr.io/distroless/base-debian13". Returns
// "" for non-Debian sources (the version-pinned distroless tags are Debian-only).
func (d Distro) RecommendedBase() string {
	if d.ID == "debian" && d.VersionID != "" {
		return "gcr.io/distroless/base-debian" + d.VersionID
	}
	return ""
}

// ReadOSRelease parses <sourceRoot>/etc/os-release and returns distro info.
// Falls back to {ID:"debian"} on error.
func ReadOSRelease(sourceRoot string) Distro {
	d := Distro{ID: "debian"}
	path := strings.TrimRight(sourceRoot, "/") + "/etc/os-release"
	f, err := os.Open(path)
	if err != nil {
		return d
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			d.ID = strings.ToLower(v)
		case "VERSION_CODENAME":
			d.VersionCode = v
		case "VERSION_ID":
			d.VersionID = v
		}
	}
	return d
}

// PURL builds a package-url for one package.
// Format: pkg:deb/<id>/<name>@<version>?arch=<arch>&distro=<codename>
func PURL(p *dpkg.Package, d Distro) string {
	q := "arch=" + p.Architecture
	if d.VersionCode != "" {
		q += "&distro=" + d.VersionCode
	}
	return fmt.Sprintf("pkg:deb/%s/%s@%s?%s", d.ID, p.Name, p.Version, q)
}

// LicenseOf attempts to extract the SPDX license identifier from a dep5
// machine-readable copyright file. Returns "NOASSERTION" if the file is absent,
// unreadable, or not dep5-formatted.
func LicenseOf(p *dpkg.Package, sourceRoot string) string {
	path := strings.TrimRight(sourceRoot, "/") + "/usr/share/doc/" + p.Name + "/copyright"
	data, err := os.ReadFile(path)
	if err != nil {
		return "NOASSERTION"
	}
	return parseDep5License(string(data))
}

// parseDep5License checks whether data is a dep5 file and extracts the first
// License: short-name. Returns "NOASSERTION" for non-dep5 files.
func parseDep5License(data string) string {
	// dep5 files start with "Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/"
	if !strings.Contains(data, "Format: https://www.debian.org/doc/packaging-manuals/copyright-format") {
		return "NOASSERTION"
	}
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "License:") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "License:"))
			// The short name is the first word (before any whitespace or newline)
			short := strings.Fields(raw)
			if len(short) > 0 {
				return mapToSPDX(short[0])
			}
		}
	}
	return "NOASSERTION"
}

// mapToSPDX maps common Debian dep5 license short-names to SPDX identifiers.
// Returns the input unchanged for unknown identifiers (best-effort).
var spdxMap = map[string]string{
	"Apache-2.0":    "Apache-2.0",
	"Apache-2":      "Apache-2.0",
	"GPL-2.0":       "GPL-2.0-only",
	"GPL-2.0+":      "GPL-2.0-or-later",
	"GPL-2":         "GPL-2.0-only",
	"GPL-2+":        "GPL-2.0-or-later",
	"GPL-3.0":       "GPL-3.0-only",
	"GPL-3.0+":      "GPL-3.0-or-later",
	"GPL-3":         "GPL-3.0-only",
	"GPL-3+":        "GPL-3.0-or-later",
	"LGPL-2.0":      "LGPL-2.0-only",
	"LGPL-2.0+":     "LGPL-2.0-or-later",
	"LGPL-2.1":      "LGPL-2.1-only",
	"LGPL-2.1+":     "LGPL-2.1-or-later",
	"LGPL-3":        "LGPL-3.0-only",
	"LGPL-3+":       "LGPL-3.0-or-later",
	"MIT":           "MIT",
	"BSD-2-clause":  "BSD-2-Clause",
	"BSD-3-clause":  "BSD-3-Clause",
	"ISC":           "ISC",
	"public-domain": "LicenseRef-public-domain",
	"Artistic":      "Artistic-1.0",
	"Artistic-2.0":  "Artistic-2.0",
	"MPL-2.0":       "MPL-2.0",
	"CC0-1.0":       "CC0-1.0",
}

func mapToSPDX(s string) string {
	if mapped, ok := spdxMap[s]; ok {
		return mapped
	}
	return s // return raw for unknown — better than silently inventing an ID
}

// docNamespaceHash derives a deterministic SPDX document namespace suffix
// from the sorted set of package names + versions.
func docNamespaceHash(pkgs []dpkg.Package) string {
	h := sha256.New()
	for _, p := range pkgs {
		_, _ = fmt.Fprintf(h, "%s@%s\n", p.Name, p.Version)
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}
