// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package summary

import (
	"fmt"
	"os"
	"strings"

	"github.com/michealch/apt2distroless/internal/manifest"
)

// Write appends one run's Markdown section to the summary file at path.
// The file is created with a header if it doesn't exist; each run appends.
func Write(path string, r *manifest.RunResult) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read summary %s: %w", path, err)
	}

	var sb strings.Builder
	if len(existing) == 0 {
		sb.WriteString("# Distroless Package Export Summary\n")
	}

	sb.WriteString("\n---\n\n")
	fmt.Fprintf(&sb, "## %s\n\n", strings.Join(r.Roots, ", "))
	sb.WriteString("| Property | Value |\n|---|---|\n")
	fmt.Fprintf(&sb, "| Root Package(s) | `%s` |\n", strings.Join(r.Roots, "`, `"))
	fmt.Fprintf(&sb, "| Architecture | `%s` |\n", r.Arch)
	if r.SourceDistro != "" {
		fmt.Fprintf(&sb, "| Source distro | `%s` (final image base **must** match this Debian release) |\n", r.SourceDistro)
	}
	fmt.Fprintf(&sb, "| Packages Copied | %d |\n", len(r.Packages))
	fmt.Fprintf(&sb, "| Files Copied | %d |\n", r.TotalFilesCopied)
	fmt.Fprintf(&sb, "| Total Bytes | %d |\n", r.TotalBytes)

	if len(r.BlacklistedSkipped) > 0 {
		sb.WriteString("\n### Blacklisted (skipped)\n\n")
		for _, p := range r.BlacklistedSkipped {
			fmt.Fprintf(&sb, "- `%s`\n", p)
		}
	}

	if len(r.BrokenEdges) > 0 {
		sb.WriteString("\n### Broken Dependency Edges (warnings)\n\n")
		sb.WriteString("The following packages depend on blacklisted/excluded packages:\n\n")
		for _, e := range r.BrokenEdges {
			fmt.Fprintf(&sb, "- `%s` depends on `%s`\n", e.From, e.To)
		}
	}

	if len(r.DanglingSymlinks) > 0 {
		sb.WriteString("\n### Dangling Symlinks (warnings)\n\n")
		for _, s := range r.DanglingSymlinks {
			fmt.Fprintf(&sb, "- `%s`\n", s)
		}
	}

	sb.WriteString("\n### Packages\n\n")
	for _, p := range r.Packages {
		fmt.Fprintf(&sb, "- `%s` (%s)\n", p.Name, p.Version)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open summary %s: %w", path, err)
	}
	if _, err := f.WriteString(sb.String()); err != nil {
		_ = f.Close()
		return fmt.Errorf("write summary %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close summary %s: %w", path, err)
	}
	return nil
}
