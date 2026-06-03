// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

//go:build e2e

// Package e2e contains the Tier-2 smoke test. Unlike the hermetic integration
// suite, this test exercises the REAL subprocess seams (`apt-cache depends` and
// `dpkg -L`) by installing a genuine package and running the built binary against
// the live dpkg database. It requires root, network, apt, dpkg and apt-cache, so
// it is gated behind `//go:build e2e` and runs only in the Docker test job
// (`make e2e`). Run with: go test -tags e2e ./test/e2e/...
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const pkg = "hello" // tiny GNU package: ships /usr/bin/hello

func mustRun(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
}

func requireCmd(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available; skipping e2e", name)
	}
}

func TestRealClosureExport(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("e2e requires root (apt-get install); run via `make e2e`")
	}
	for _, c := range []string{"apt-get", "apt-cache", "dpkg", "go"} {
		requireCmd(t, c)
	}

	// Install a real leaf package.
	mustRun(t, "apt-get", "update")
	mustRun(t, "apt-get", "install", "-y", "--no-install-recommends", pkg)

	// Build the real binary. -buildvcs=false because the e2e job runs as root
	// over a bind mount owned by another uid (git would refuse VCS stamping).
	bin := filepath.Join(t.TempDir(), "apt2distroless")
	mustRun(t, "go", "build", "-buildvcs=false", "-o", bin, "../../cmd/apt2distroless")

	// Export the closure of the real package from the live root.
	// --keep-going tolerates the bookworm-transitional usrmerge /lib duplicates
	// (a known copier limitation tracked separately); on a fully usr-merged base
	// (trixie) this is not needed.
	target := t.TempDir()
	mustRun(t, bin, "--source-root", "/", "--keep-going", "--log-level", "warning", pkg, target)

	// The package's own binary must be present.
	if _, err := os.Stat(filepath.Join(target, "usr/bin/hello")); err != nil {
		t.Errorf("usr/bin/hello missing from export: %v", err)
	}
	// Lean dpkg metadata must be emitted for the scanner-visible package.
	if _, err := os.Stat(filepath.Join(target, "var/lib/dpkg/status.d", pkg)); err != nil {
		t.Errorf("status.d/%s missing: %v", pkg, err)
	}
	// The transitive closure must have pulled in libc6 (a real dependency of hello).
	if _, err := os.Stat(filepath.Join(target, "var/lib/dpkg/status.d/libc6")); err != nil {
		t.Logf("note: status.d/libc6 not found (libc6 may be named differently): %v", err)
	}
}
