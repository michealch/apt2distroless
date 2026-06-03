// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package sbom

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michealch/apt2distroless/internal/dpkg"
	"github.com/michealch/apt2distroless/internal/manifest"
)

var testResult = &manifest.RunResult{
	Roots: []string{"curl"},
	Arch:  "amd64",
	Packages: []dpkg.Package{
		{Name: "curl", Version: "7.88.1-1", Architecture: "amd64",
			Maintainer: "Debian curl team",
			Depends:    []dpkg.AtomGroup{{Alts: []dpkg.Atom{{Name: "libc6"}}}},
		},
		{Name: "libc6", Version: "2.36-1", Architecture: "amd64"},
	},
}

var testDistro = Distro{ID: "debian", VersionCode: "bookworm"}

func TestWriteSPDX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sbom.spdx.json")
	if err := WriteSPDX(path, testResult, testDistro, 0); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	var doc spdxDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid SPDX JSON: %v", err)
	}
	if doc.SPDXVersion != "SPDX-2.3" {
		t.Errorf("spdxVersion = %q, want SPDX-2.3", doc.SPDXVersion)
	}
	if len(doc.Packages) != 2 {
		t.Errorf("packages = %d, want 2", len(doc.Packages))
	}
	if !strings.Contains(doc.Packages[0].ExternalRefs[0].Locator, "pkg:deb/debian/curl") {
		t.Errorf("PURL missing or wrong: %s", doc.Packages[0].ExternalRefs[0].Locator)
	}
	// Created should equal epoch 0 formatted.
	if !strings.HasPrefix(doc.CreationInfo.Created, "1970-") {
		t.Errorf("created should use epoch, got %s", doc.CreationInfo.Created)
	}
}

func TestWriteCycloneDX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sbom.cdx.json")
	if err := WriteCycloneDX(path, testResult, testDistro, 0); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	var doc cdxDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid CycloneDX JSON: %v", err)
	}
	if doc.BOMFormat != "CycloneDX" {
		t.Errorf("bomFormat = %q", doc.BOMFormat)
	}
	if len(doc.Components) != 2 {
		t.Errorf("components = %d, want 2", len(doc.Components))
	}
}

func TestPURL(t *testing.T) {
	p := &dpkg.Package{Name: "curl", Version: "7.88", Architecture: "amd64"}
	d := Distro{ID: "ubuntu", VersionCode: "jammy"}
	got := PURL(p, d)
	want := "pkg:deb/ubuntu/curl@7.88?arch=amd64&distro=jammy"
	if got != want {
		t.Errorf("PURL = %q, want %q", got, want)
	}
}

func TestLicenseDep5(t *testing.T) {
	dir := t.TempDir()
	docDir := filepath.Join(dir, "usr", "share", "doc", "mylib")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dep5Content := `Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: mylib

Files: *
License: MIT
 Permission is hereby granted...
`
	if err := os.WriteFile(filepath.Join(docDir, "copyright"), []byte(dep5Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Test parseDep5License directly.
	got := parseDep5License(dep5Content)
	if got != "MIT" {
		t.Errorf("license = %q, want MIT", got)
	}
}

func TestLicenseNonDep5(t *testing.T) {
	prose := "This software is licensed under the MIT license. See LICENSE for details."
	got := parseDep5License(prose)
	if got != "NOASSERTION" {
		t.Errorf("non-dep5 copyright should return NOASSERTION, got %q", got)
	}
}

func TestSPDXDeterministic(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.spdx.json")
	p2 := filepath.Join(dir, "b.spdx.json")

	if err := WriteSPDX(p1, testResult, testDistro, 12345); err != nil {
		t.Fatal(err)
	}
	if err := WriteSPDX(p2, testResult, testDistro, 12345); err != nil {
		t.Fatal(err)
	}

	d1, _ := os.ReadFile(p1)
	d2, _ := os.ReadFile(p2)
	if string(d1) != string(d2) {
		t.Error("SPDX output is not deterministic across two runs with same epoch")
	}
}
