// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

// Package integration runs the REAL run.Pipeline end-to-end against the
// synthetic --source-root fixture. The two subprocess seams (apt-cache resolver,
// dpkg -L lister) are injected via run.Deps: a resolver.FakeResolver supplies the
// closure and a fixtureLister reads the fixture's .list files. No live apt-cache
// or dpkg is invoked — every flag is exercised through production code paths.
package integration

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michealch/apt2distroless/internal/config"
	"github.com/michealch/apt2distroless/internal/log"
	"github.com/michealch/apt2distroless/internal/resolver"
	"github.com/michealch/apt2distroless/internal/run"
)

const fixtureRoot = "../fixtures/source-root"

// fixtureLister reads .list files from the fixture, mimicking `dpkg -L`.
type fixtureLister struct{ sourceRoot string }

func (f *fixtureLister) List(pkg string) ([]string, error) {
	candidates := []string{
		filepath.Join(f.sourceRoot, "var/lib/dpkg/info", pkg+".list"),
		filepath.Join(f.sourceRoot, "var/lib/dpkg/info", pkg+":amd64.list"),
	}
	for _, c := range candidates {
		data, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		var paths []string
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || line == "." {
				continue
			}
			if !strings.HasPrefix(line, "/") {
				line = "/" + line
			}
			paths = append(paths, line)
		}
		return paths, nil
	}
	return nil, nil
}

// newCfg returns a Config with host-independent test defaults. Arch is pinned to
// amd64 so tests behave the same on amd64 and arm64 Docker hosts.
func newCfg(target string, roots ...string) *config.Config {
	return &config.Config{
		Roots:         roots,
		Target:        target,
		SourceRoot:    fixtureRoot,
		Arch:          "amd64",
		Jobs:          1,
		Deduplicate:   true,
		DedupStrategy: "hardlink",
		SummaryPath:   target + ".summary.md",
		ManifestPath:  target + ".manifest.jsonl",
	}
}

// runReal invokes the real pipeline with the FakeResolver closure and returns
// the exit code.
func runReal(t *testing.T, cfg *config.Config, closure ...string) int {
	t.Helper()
	logger := log.New(log.LevelError, "text")
	deps := run.Deps{
		Resolver: &resolver.FakeResolver{Result: closure},
		Lister:   &fixtureLister{sourceRoot: fixtureRoot},
	}
	return run.Pipeline(cfg, logger, deps)
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func isSymlink(t *testing.T, p string) bool {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSymlink != 0
}

func TestCopiesClosure(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkga")
	// pkga depends on pkgb.
	if code := runReal(t, cfg, "pkga", "pkgb"); code != run.ExitSuccess {
		t.Fatalf("exit code: got %d want 0", code)
	}

	mustExist := []string{
		"usr/bin/pkga",
		"usr/lib/pkga.so",
		"usr/share/pkgb-data/data.txt",
		"var/lib/dpkg/status.d/pkga",
		"usr/share/doc/pkga/copyright",
	}
	for _, rel := range mustExist {
		if !exists(filepath.Join(target, rel)) {
			t.Errorf("expected %s in target", rel)
		}
	}
	// Verbatim symlink preserved as a symlink (not chased).
	if !isSymlink(t, filepath.Join(target, "usr/bin/pkga-alias")) {
		t.Error("usr/bin/pkga-alias should be a symlink")
	}
}

func TestReproducibility(t *testing.T) {
	run1 := t.TempDir()
	run2 := t.TempDir()
	for _, tgt := range []string{run1, run2} {
		cfg := newCfg(tgt, "pkga")
		cfg.SourceDateEpoch = 1000000
		if code := runReal(t, cfg, "pkga", "pkgb", "pkgd"); code != run.ExitSuccess {
			t.Fatalf("exit code: got %d want 0", code)
		}
	}

	type fileInfo struct {
		mode  string
		size  int64
		mtime int64
	}
	scan := func(root string) map[string]fileInfo {
		m := make(map[string]fileInfo)
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			fi, _ := d.Info()
			m[rel] = fileInfo{fi.Mode().String(), fi.Size(), fi.ModTime().Unix()}
			return nil
		})
		return m
	}
	t1, t2 := scan(run1), scan(run2)
	for path, i1 := range t1 {
		i2, ok := t2[path]
		if !ok {
			t.Errorf("%s missing in second run", path)
			continue
		}
		if i1 != i2 {
			t.Errorf("%s differs: %+v vs %+v", path, i1, i2)
		}
	}
	for path := range t2 {
		if _, ok := t1[path]; !ok {
			t.Errorf("%s only in second run", path)
		}
	}
}

func TestManifestOutsideTargetAndDistro(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkga")
	if code := runReal(t, cfg, "pkga", "pkgb"); code != run.ExitSuccess {
		t.Fatalf("exit code: got %d want 0", code)
	}

	// Manifest must be a sibling, never inside the rootfs.
	entries, _ := os.ReadDir(target)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			t.Errorf("manifest leaked inside target: %s", e.Name())
		}
	}
	data, err := os.ReadFile(cfg.ManifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal(data[:len(data)-1], &rec); err != nil {
		t.Fatalf("manifest not valid JSON: %v", err)
	}
	if got, _ := rec["source_distro"].(string); !strings.Contains(got, "debian") || !strings.Contains(got, "13") {
		t.Errorf("manifest source_distro = %q, want debian 13", got)
	}
}

func TestSourceDistroInSummary(t *testing.T) {
	target := t.TempDir()
	cfg := newCfg(target, "pkga")
	if code := runReal(t, cfg, "pkga"); code != run.ExitSuccess {
		t.Fatalf("exit code: got %d want 0", code)
	}
	data, err := os.ReadFile(cfg.SummaryPath)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !strings.Contains(string(data), "trixie") {
		t.Errorf("summary missing source-distro guardrail (trixie):\n%s", data)
	}
}

func TestSBOMDeterministic(t *testing.T) {
	read := func(t *testing.T) (spdx, cdx []byte) {
		t.Helper()
		target := t.TempDir()
		cfg := newCfg(target, "pkga")
		cfg.SourceDateEpoch = 1000000
		cfg.SBOMSpdx = target + ".spdx.json"
		cfg.SBOMCycloneDX = target + ".cdx.json"
		if code := runReal(t, cfg, "pkga", "pkgb"); code != run.ExitSuccess {
			t.Fatalf("exit code: got %d want 0", code)
		}
		s, err := os.ReadFile(cfg.SBOMSpdx)
		if err != nil {
			t.Fatalf("read spdx: %v", err)
		}
		c, err := os.ReadFile(cfg.SBOMCycloneDX)
		if err != nil {
			t.Fatalf("read cyclonedx: %v", err)
		}
		// Both must be valid JSON.
		var tmp any
		if err := json.Unmarshal(s, &tmp); err != nil {
			t.Fatalf("spdx invalid JSON: %v", err)
		}
		if err := json.Unmarshal(c, &tmp); err != nil {
			t.Fatalf("cyclonedx invalid JSON: %v", err)
		}
		if !strings.Contains(string(s), "pkga") {
			t.Errorf("spdx missing pkga")
		}
		return s, c
	}
	s1, c1 := read(t)
	s2, c2 := read(t)
	if string(s1) != string(s2) {
		t.Error("SPDX not byte-identical across runs")
	}
	if string(c1) != string(c2) {
		t.Error("CycloneDX not byte-identical across runs")
	}
}
