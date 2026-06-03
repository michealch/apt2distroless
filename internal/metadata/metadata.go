// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package metadata

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/michealch/apt2distroless/internal/dpkg"
)

// Emit writes all dpkg metadata for one package into the target rootfs.
// Emitted (lean status.d convention):
//   - <target>/var/lib/dpkg/status.d/<pkg>        — verbatim control stanza
//   - <target>/var/lib/dpkg/status.d/<pkg>.md5sums — md5sums file (if present)
//   - <target>/usr/share/doc/<pkg>/copyright       — license text (if present)
//
// NOT emitted: maintainer scripts, .list, .conffiles (not used in distroless images).
// Emission is independent of file excludes.
func Emit(target string, p *dpkg.Package, sourceRoot string) error {
	if err := emitStanza(target, p); err != nil {
		return err
	}
	if err := emitMD5Sums(target, p, sourceRoot); err != nil {
		return err
	}
	if err := emitCopyright(target, p, sourceRoot); err != nil {
		return err
	}
	return nil
}

func emitStanza(target string, p *dpkg.Package) error {
	dir := filepath.Join(target, "var", "lib", "dpkg", "status.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create status.d: %w", err)
	}
	dest := filepath.Join(dir, p.Name)
	if err := os.WriteFile(dest, p.Stanza, 0o644); err != nil {
		return fmt.Errorf("write status.d/%s: %w", p.Name, err)
	}
	return nil
}

func emitMD5Sums(target string, p *dpkg.Package, sourceRoot string) error {
	infoDir := filepath.Join(sourceRoot, "var", "lib", "dpkg", "info")
	for _, candidate := range dpkg.InfoFileNames(p, "md5sums") {
		src := filepath.Join(infoDir, candidate)
		data, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read md5sums %s: %w", src, err)
		}
		dir := filepath.Join(target, "var", "lib", "dpkg", "status.d")
		dest := filepath.Join(dir, p.Name+".md5sums")
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("write status.d/%s.md5sums: %w", p.Name, err)
		}
		return nil // found and written
	}
	return nil // md5sums is optional; not an error if absent
}

func emitCopyright(target string, p *dpkg.Package, sourceRoot string) error {
	src := filepath.Join(sourceRoot, "usr", "share", "doc", p.Name, "copyright")
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // copyright is optional
		}
		return fmt.Errorf("read copyright %s: %w", src, err)
	}
	dir := filepath.Join(target, "usr", "share", "doc", p.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create copyright dir: %w", err)
	}
	dest := filepath.Join(dir, "copyright")
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("write copyright %s: %w", dest, err)
	}
	return nil
}
