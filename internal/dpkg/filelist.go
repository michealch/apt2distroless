// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package dpkg

import (
	"fmt"
	"os/exec"
	"strings"
)

// FileLister lists the filesystem paths owned by a package.
// The returned paths are rootfs-absolute (e.g. /usr/bin/curl).
type FileLister interface {
	List(pkg string) ([]string, error)
}

// DpkgLister is the production implementation: it shells to `dpkg -L <pkg>`.
// It is diversion-correct (dpkg -L applies dpkg-divert rerouting).
type DpkgLister struct {
	// SourceRoot is the --source-root value. When "/" the plain `dpkg -L` is
	// used; for a non-host root, `dpkg --root=<sourceRoot> -L` is used.
	SourceRoot string
}

// List runs `dpkg [-L|--root=…] -L <pkg>` and returns the file list.
func (d *DpkgLister) List(pkg string) ([]string, error) {
	var cmd *exec.Cmd
	if d.SourceRoot == "" || d.SourceRoot == "/" {
		cmd = exec.Command("dpkg", "-L", pkg)
	} else {
		cmd = exec.Command("dpkg", "--root="+d.SourceRoot, "-L", pkg)
	}
	out, err := cmd.Output()
	if err != nil {
		// dpkg -L on a package with no files exits 0 with empty output;
		// a genuine error (package not found) exits non-zero.
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("dpkg -L %s: %w\n%s", pkg, err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("dpkg -L %s: %w", pkg, err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	paths := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		// dpkg -L emits "." as first line and "diverted by …" markers; skip those.
		if l == "" || l == "." || strings.HasPrefix(l, "diverted") {
			continue
		}
		paths = append(paths, l)
	}
	return paths, nil
}

// FakeLister is a test double that returns a pre-configured list of paths.
type FakeLister struct {
	Paths map[string][]string // pkg → []path
}

// List returns the pre-configured paths for pkg, or an empty slice.
func (f *FakeLister) List(pkg string) ([]string, error) {
	if paths, ok := f.Paths[pkg]; ok {
		return paths, nil
	}
	return []string{}, nil
}
