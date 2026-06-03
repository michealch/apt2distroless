// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/michealch/apt2distroless/internal/dpkg"
)

// PackageEntry is one package entry in the manifest.
type PackageEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// BrokenEdgeEntry is a dependency edge where a copied package depends on a blocked one.
type BrokenEdgeEntry struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// RunRecord is the JSON object written per run (one line in the JSONL file).
type RunRecord struct {
	Schema             int               `json:"schema"`
	Timestamp          string            `json:"timestamp"`
	Roots              []string          `json:"roots"`
	Arch               string            `json:"arch"`
	SourceDistro       string            `json:"source_distro,omitempty"`
	Packages           []PackageEntry    `json:"packages"`
	BlacklistedSkipped []string          `json:"blacklisted_skipped"`
	BrokenEdges        []BrokenEdgeEntry `json:"broken_edges,omitempty"`
	DanglingSymlinks   []string          `json:"dangling_symlinks,omitempty"`
	TotalFilesCopied   int               `json:"total_files_copied"`
	TotalBytes         int64             `json:"total_bytes"`
}

// RunResult is the aggregated pipeline result passed to Append.
type RunResult struct {
	Roots              []string
	Arch               string
	SourceDistro       string
	Packages           []dpkg.Package
	BlacklistedSkipped []string
	BrokenEdges        []dpkg.BrokenEdge
	DanglingSymlinks   []string
	TotalFilesCopied   int
	TotalBytes         int64
	StartedAt          time.Time
}

// Append adds one JSONL record for this run to path (outside the rootfs).
// The file is created if it doesn't exist; each run appends one line.
func Append(path string, r *RunResult) error {
	pkgs := make([]PackageEntry, len(r.Packages))
	for i, p := range r.Packages {
		pkgs[i] = PackageEntry{Name: p.Name, Version: p.Version}
	}

	edges := make([]BrokenEdgeEntry, len(r.BrokenEdges))
	for i, e := range r.BrokenEdges {
		edges[i] = BrokenEdgeEntry{From: e.From, To: e.To}
	}

	rec := RunRecord{
		Schema:             1,
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		Roots:              r.Roots,
		Arch:               r.Arch,
		SourceDistro:       r.SourceDistro,
		Packages:           pkgs,
		BlacklistedSkipped: r.BlacklistedSkipped,
		BrokenEdges:        edges,
		DanglingSymlinks:   r.DanglingSymlinks,
		TotalFilesCopied:   r.TotalFilesCopied,
		TotalBytes:         r.TotalBytes,
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal manifest record: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open manifest %s: %w", path, err)
	}

	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close manifest %s: %w", path, err)
	}
	return nil
}
