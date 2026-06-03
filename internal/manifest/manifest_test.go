// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package manifest

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/michealch/apt2distroless/internal/dpkg"
)

func TestAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.manifest.jsonl")

	r := &RunResult{
		Roots: []string{"curl"},
		Arch:  "amd64",
		Packages: []dpkg.Package{
			{Name: "curl", Version: "7.88.1-1"},
			{Name: "libc6", Version: "2.36-1"},
		},
		BlacklistedSkipped: []string{"bash"},
		TotalFilesCopied:   42,
		TotalBytes:         100000,
	}

	// First run.
	if err := Append(path, r); err != nil {
		t.Fatal(err)
	}
	// Second run (simulates appending).
	if err := Append(path, r); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
		var rec RunRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", lines, err)
		}
		if rec.Schema != 1 {
			t.Errorf("schema = %d, want 1", rec.Schema)
		}
		if len(rec.Packages) != 2 {
			t.Errorf("packages = %d, want 2", len(rec.Packages))
		}
	}
	if lines != 2 {
		t.Errorf("expected 2 JSONL lines, got %d", lines)
	}
}

func TestManifestOutsideRootfs(t *testing.T) {
	target := t.TempDir()
	path := target + ".manifest.jsonl" // sibling, NOT inside target

	r := &RunResult{Roots: []string{"foo"}, Arch: "amd64"}
	if err := Append(path, r); err != nil {
		t.Fatal(err)
	}

	// The manifest must not exist inside the target directory.
	entries, _ := os.ReadDir(target)
	for _, e := range entries {
		if e.Name() == "manifest.jsonl" || e.Name() == ".distroless-manifest.jsonl" {
			t.Errorf("manifest found inside target dir: %s", e.Name())
		}
	}
}
