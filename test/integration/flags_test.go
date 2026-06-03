// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michealch/apt2distroless/internal/config"
	"github.com/michealch/apt2distroless/internal/run"
)

func absent(t *testing.T, target, rel string) {
	t.Helper()
	if exists(filepath.Join(target, rel)) {
		t.Errorf("%s should NOT be in target", rel)
	}
}

func present(t *testing.T, target, rel string) {
	t.Helper()
	if !exists(filepath.Join(target, rel)) {
		t.Errorf("%s should be in target", rel)
	}
}

// ---- Blacklist ----------------------------------------------------------------

func TestBlacklistAdd(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkge")
	cfg.Blacklist.Add = []string{"pkgf"}
	if code := runReal(t, cfg, "pkge", "pkgf"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	present(t, target, "usr/bin/pkge")
	absent(t, target, "usr/bin/pkgf") // blocked
}

func TestBlacklistFileThenRemove(t *testing.T) {
	blFile := filepath.Join(t.TempDir(), "bl.txt")
	if err := os.WriteFile(blFile, []byte("pkgf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// With the file, pkgf is blocked.
	t1 := t.TempDir()
	cfg := newCfg(t1, "pkge")
	cfg.Blacklist.File = blFile
	if code := runReal(t, cfg, "pkge", "pkgf"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	present(t, t1, "usr/bin/pkge")
	absent(t, t1, "usr/bin/pkgf")

	// --blacklist-remove overrides the file entry → pkgf copied.
	t2 := t.TempDir()
	cfg2 := newCfg(t2, "pkge")
	cfg2.Blacklist.File = blFile
	cfg2.Blacklist.Remove = []string{"pkgf"}
	if code := runReal(t, cfg2, "pkge", "pkgf"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	present(t, t2, "usr/bin/pkgf")
}

// ---- Excludes -----------------------------------------------------------------

func TestExcludeMan(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkga")
	cfg.Exclude = config.ExcludeConfig{Man: true}
	if code := runReal(t, cfg, "pkga"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	present(t, target, "usr/bin/pkga")
	absent(t, target, "usr/share/man/man1/pkga.1")
	// info/locale not excluded → still present.
	present(t, target, "usr/share/info/pkga.info")
}

func TestExcludeAllKeepsCopyright(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkga")
	cfg.Exclude = config.ExcludeConfig{All: true}
	if code := runReal(t, cfg, "pkga"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	present(t, target, "usr/bin/pkga")
	absent(t, target, "usr/share/man/man1/pkga.1")
	absent(t, target, "usr/share/info/pkga.info")
	absent(t, target, "usr/share/locale/de/LC_MESSAGES/pkga.mo")
	// copyright carve-out is always preserved.
	present(t, target, "usr/share/doc/pkga/copyright")
}

func TestExcludePath(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkga")
	cfg.Exclude = config.ExcludeConfig{Paths: []string{"/usr/lib"}}
	if code := runReal(t, cfg, "pkga"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	present(t, target, "usr/bin/pkga")
	absent(t, target, "usr/lib/pkga.so")
}

// ---- Dedup --------------------------------------------------------------------

func TestDedupHardlink(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkga") // dedup on by default
	if code := runReal(t, cfg, "pkga", "pkgd"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	a, err := os.Stat(filepath.Join(target, "usr/lib/pkga.so"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := os.Stat(filepath.Join(target, "usr/lib/pkgd.so"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(a, d) {
		t.Error("identical files should be hardlinked (same inode) after dedup")
	}
}

func TestNoDedup(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkga")
	cfg.DedupStrategy = "none"
	if code := runReal(t, cfg, "pkga", "pkgd"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	a, _ := os.Stat(filepath.Join(target, "usr/lib/pkga.so"))
	d, _ := os.Stat(filepath.Join(target, "usr/lib/pkgd.so"))
	if os.SameFile(a, d) {
		t.Error("with --dedup-strategy=none the files must remain distinct inodes")
	}
}

// ---- Failure policy -----------------------------------------------------------

// TestMissingSourceSkipped verifies that a dpkg-listed path which is absent on
// disk (e.g. dpkg path-excludes on a slim base) is a non-fatal skip, NOT a hard
// failure. pkgc.list references usr/bin/pkgc, which doesn't exist in the fixture.
func TestMissingSourceSkipped(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkga") // strict (no --keep-going)
	if code := runReal(t, cfg, "pkga", "pkgc"); code != run.ExitSuccess {
		t.Fatalf("missing source should be skipped, not fail: got exit %d want 0", code)
	}
	present(t, target, "usr/bin/pkga")
	absent(t, target, "usr/bin/pkgc") // listed but absent on disk → skipped
}

// TestKeepGoingStrictVsLenient exercises a GENUINE copy failure: a regular file
// is pre-seeded at <target>/usr, so creating the usr/ directory fails. With
// --merge the seed survives preflight; strict mode returns ExitPartialCopy while
// --keep-going downgrades it to success.
func TestKeepGoingStrictVsLenient(t *testing.T) {
	seed := func(t *testing.T) string {
		t.Helper()
		target := t.TempDir()
		// A regular file where the package needs a directory.
		if err := os.WriteFile(filepath.Join(target, "usr"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return target
	}

	t.Run("strict fails with ExitPartialCopy", func(t *testing.T) {
		target := seed(t)
		cfg := newCfg(target, "pkga")
		cfg.Merge = true // keep the seeded file through preflight
		if code := runReal(t, cfg, "pkga"); code != run.ExitPartialCopy {
			t.Fatalf("exit: got %d want %d", code, run.ExitPartialCopy)
		}
	})
	t.Run("--keep-going downgrades to success", func(t *testing.T) {
		target := seed(t)
		cfg := newCfg(target, "pkga")
		cfg.Merge = true
		cfg.KeepGoing = true
		if code := runReal(t, cfg, "pkga"); code != run.ExitSuccess {
			t.Fatalf("exit: got %d want 0", code)
		}
	})
}

// ---- Dry run ------------------------------------------------------------------

func TestDryRunWritesNothing(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "out") // must not exist after dry-run
	cfg := newCfg(target, "pkga")
	cfg.DryRun = true
	if code := runReal(t, cfg, "pkga", "pkgb"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	if exists(target) {
		t.Errorf("--dry-run must not create the target directory")
	}
}

// ---- Architecture -------------------------------------------------------------

func TestArchFilter(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkgh")
	cfg.Arch = "arm64"
	if code := runReal(t, cfg, "pkga", "pkgh"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	present(t, target, "usr/bin/pkgh") // arm64 → kept
	absent(t, target, "usr/bin/pkga")  // amd64 → filtered out for arm64 target
}

func TestArchNotInstalled(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkga")
	cfg.Arch = "riscv64" // no native riscv64 package in the fixture
	if code := runReal(t, cfg, "pkga"); code != run.ExitArchNotFound {
		t.Fatalf("exit: got %d want %d", code, run.ExitArchNotFound)
	}
}

func TestRootNotInstalled(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "does-not-exist")
	if code := runReal(t, cfg, "does-not-exist"); code != run.ExitNotInstalled {
		t.Fatalf("exit: got %d want %d", code, run.ExitNotInstalled)
	}
}

// ---- Target handling ----------------------------------------------------------

func seedTarget(t *testing.T) (target, stray string) {
	t.Helper()
	target = t.TempDir()
	stray = filepath.Join(target, "stray.txt")
	if err := os.WriteFile(stray, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return target, stray
}

func TestOverwrite(t *testing.T) {
	target, stray := seedTarget(t)
	cfg := newCfg(target, "pkga")
	cfg.Overwrite = true
	if code := runReal(t, cfg, "pkga"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	if exists(stray) {
		t.Error("--overwrite should wipe the pre-existing target contents")
	}
	present(t, target, "usr/bin/pkga")
}

func TestMerge(t *testing.T) {
	target, stray := seedTarget(t)
	cfg := newCfg(target, "pkga")
	cfg.Merge = true
	if code := runReal(t, cfg, "pkga"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	if !exists(stray) {
		t.Error("--merge should preserve pre-existing target contents")
	}
	present(t, target, "usr/bin/pkga")
}

func TestNonEmptyTargetStrict(t *testing.T) {
	target, _ := seedTarget(t)
	cfg := newCfg(target, "pkga") // neither overwrite nor merge
	if code := runReal(t, cfg, "pkga"); code != run.ExitTargetError {
		t.Fatalf("exit: got %d want %d", code, run.ExitTargetError)
	}
}

// ---- Recommends plumbing (closure handling) -----------------------------------

func TestIncludeRecommends(t *testing.T) {
	// Without recommends the resolver returns just pkge; with recommends it also
	// returns pkgf. This verifies the pipeline copies whatever closure it is given;
	// the flag→apt-cache argv mapping is unit-tested in internal/resolver.
	without := t.TempDir()
	cfg := newCfg(without, "pkge")
	if code := runReal(t, cfg, "pkge"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	present(t, without, "usr/bin/pkge")
	absent(t, without, "usr/bin/pkgf")

	with := t.TempDir()
	cfg2 := newCfg(with, "pkge")
	cfg2.IncludeRecommends = true
	if code := runReal(t, cfg2, "pkge", "pkgf"); code != run.ExitSuccess {
		t.Fatalf("exit: got %d want 0", code)
	}
	present(t, with, "usr/bin/pkge")
	present(t, with, "usr/bin/pkgf")
}
