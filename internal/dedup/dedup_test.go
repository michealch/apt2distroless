// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package dedup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michealch/apt2distroless/internal/dpkg"
)

func writeFile(t *testing.T, dir, name, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func sameInode(t *testing.T, a, b string) bool {
	t.Helper()
	fi1, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	fi2, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(fi1, fi2)
}

func TestDedupHardlinks(t *testing.T) {
	dir := t.TempDir()
	pa := writeFile(t, dir, "a.txt", "hello", 0o644)
	pb := writeFile(t, dir, "b.txt", "hello", 0o644)

	entries := []dpkg.Entry{
		{Dst: pa, Kind: dpkg.KindRegular, Mode: 0o644, Size: 5},
		{Dst: pb, Kind: dpkg.KindRegular, Mode: 0o644, Size: 5},
	}

	d := &Deduper{Strategy: "hardlink", Jobs: 1}
	linked, warns, err := d.Run(entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) > 0 {
		t.Logf("warnings: %v", warns)
	}
	if linked != 1 {
		t.Errorf("linked = %d, want 1", linked)
	}
	if !sameInode(t, pa, pb) {
		t.Error("a.txt and b.txt should share an inode after hardlinking")
	}
}

func TestDedupDifferentMode(t *testing.T) {
	dir := t.TempDir()
	pa := writeFile(t, dir, "a.bin", "hello", 0o644)
	pb := writeFile(t, dir, "b.bin", "hello", 0o755) // same content, different mode

	entries := []dpkg.Entry{
		{Dst: pa, Kind: dpkg.KindRegular, Mode: 0o644, Size: 5},
		{Dst: pb, Kind: dpkg.KindRegular, Mode: 0o755, Size: 5},
	}

	d := &Deduper{Strategy: "hardlink", Jobs: 1}
	linked, _, err := d.Run(entries)
	if err != nil {
		t.Fatal(err)
	}
	if linked != 0 {
		t.Error("files with different modes must NOT be hardlinked")
	}
	if sameInode(t, pa, pb) {
		t.Error("different-mode files share an inode — metadata-inclusive key broken")
	}
}

func TestDedupWinnerIsLexicallyFirst(t *testing.T) {
	dir := t.TempDir()
	pa := writeFile(t, dir, "aaa.txt", "same", 0o644)
	pb := writeFile(t, dir, "zzz.txt", "same", 0o644)

	entries := []dpkg.Entry{
		{Dst: pb, Kind: dpkg.KindRegular, Mode: 0o644, Size: 4},
		{Dst: pa, Kind: dpkg.KindRegular, Mode: 0o644, Size: 4},
	}

	d := &Deduper{Strategy: "hardlink", Jobs: 2}
	_, _, err := d.Run(entries)
	if err != nil {
		t.Fatal(err)
	}
	if !sameInode(t, pa, pb) {
		t.Error("expected hardlink")
	}
}

func TestDedupStrategyNone(t *testing.T) {
	dir := t.TempDir()
	pa := writeFile(t, dir, "a.txt", "dup", 0o644)
	pb := writeFile(t, dir, "b.txt", "dup", 0o644)

	entries := []dpkg.Entry{
		{Dst: pa, Kind: dpkg.KindRegular, Mode: 0o644, Size: 3},
		{Dst: pb, Kind: dpkg.KindRegular, Mode: 0o644, Size: 3},
	}
	d := &Deduper{Strategy: "none"}
	linked, _, _ := d.Run(entries)
	if linked != 0 {
		t.Error("strategy=none should not link anything")
	}
}

func TestDedupSkipsSymlinksAndDirs(t *testing.T) {
	dir := t.TempDir()
	pa := writeFile(t, dir, "a.txt", "real", 0o644)
	entries := []dpkg.Entry{
		{Dst: pa, Kind: dpkg.KindRegular, Mode: 0o644, Size: 4},
		{Dst: dir + "/link", Kind: dpkg.KindSymlink, Size: 4},
		{Dst: dir, Kind: dpkg.KindDir},
	}
	d := &Deduper{Strategy: "hardlink", Jobs: 1}
	linked, _, _ := d.Run(entries)
	if linked != 0 {
		t.Error("symlinks and dirs should not be hardlinked")
	}
}
