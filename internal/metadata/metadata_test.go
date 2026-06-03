// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package metadata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michealch/apt2distroless/internal/dpkg"
)

func TestEmit(t *testing.T) {
	// Set up a synthetic source root.
	sourceRoot := t.TempDir()
	target := t.TempDir()

	pkg := &dpkg.Package{
		Name:         "mypkg",
		Architecture: "amd64",
		Stanza:       []byte("Package: mypkg\nVersion: 1.0-1\nArchitecture: amd64\nStatus: install ok installed\n"),
	}

	// Create md5sums in source root (try bare name first).
	infoDir := filepath.Join(sourceRoot, "var", "lib", "dpkg", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	md5Data := []byte("abc123  usr/bin/mypkg\n")
	if err := os.WriteFile(filepath.Join(infoDir, "mypkg.md5sums"), md5Data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create copyright in source root.
	docDir := filepath.Join(sourceRoot, "usr", "share", "doc", "mypkg")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyrightData := []byte("Copyright 2024 Test\nLicense: MIT\n")
	if err := os.WriteFile(filepath.Join(docDir, "copyright"), copyrightData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Run Emit.
	if err := Emit(target, pkg, sourceRoot); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	// Assert status.d stanza.
	stanza, err := os.ReadFile(filepath.Join(target, "var", "lib", "dpkg", "status.d", "mypkg"))
	if err != nil {
		t.Fatalf("status.d stanza missing: %v", err)
	}
	if string(stanza) != string(pkg.Stanza) {
		t.Errorf("stanza mismatch:\ngot:  %q\nwant: %q", stanza, pkg.Stanza)
	}

	// Assert md5sums in status.d.
	md5, err := os.ReadFile(filepath.Join(target, "var", "lib", "dpkg", "status.d", "mypkg.md5sums"))
	if err != nil {
		t.Fatalf("status.d md5sums missing: %v", err)
	}
	if string(md5) != string(md5Data) {
		t.Errorf("md5sums mismatch")
	}

	// Assert copyright.
	cr, err := os.ReadFile(filepath.Join(target, "usr", "share", "doc", "mypkg", "copyright"))
	if err != nil {
		t.Fatalf("copyright missing: %v", err)
	}
	if string(cr) != string(copyrightData) {
		t.Errorf("copyright mismatch")
	}

	// Assert no maintainer scripts present.
	for _, forbidden := range []string{"postinst", "preinst", "prerm", "postrm", "config"} {
		path := filepath.Join(target, "var", "lib", "dpkg", "info", "mypkg."+forbidden)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("maintainer script %s should not be emitted", forbidden)
		}
	}
}

func TestEmitNoCopyrightOK(t *testing.T) {
	sourceRoot := t.TempDir()
	target := t.TempDir()
	pkg := &dpkg.Package{Name: "minimal", Stanza: []byte("Package: minimal\n")}
	// No copyright in source root — should succeed.
	if err := Emit(target, pkg, sourceRoot); err != nil {
		t.Fatalf("Emit without copyright should not error: %v", err)
	}
}
