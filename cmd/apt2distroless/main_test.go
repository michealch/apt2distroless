// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package main

import "testing"

// TestRootCmdFlagsRegistered asserts every documented flag is wired into the
// cobra command with the expected default. This catches "registered but renamed"
// and "removed but still referenced" drift.
func TestRootCmdFlagsRegistered(t *testing.T) {
	cmd := newRootCmd()

	// flag name → expected default value ("" = don't assert a specific default,
	// just that the flag exists).
	want := map[string]string{
		"root":               "[]",
		"target":             "",
		"source-root":        "/",
		"dry-run":            "false",
		"log-level":          "info",
		"log-format":         "text",
		"blacklist-file":     "",
		"blacklist-add":      "[]",
		"blacklist-remove":   "[]",
		"exclude-docs":       "false",
		"exclude-man":        "false",
		"exclude-info":       "false",
		"exclude-locale":     "false",
		"exclude-icons":      "false",
		"exclude-fonts":      "false",
		"exclude-cache":      "false",
		"exclude-all":        "false",
		"exclude-path":       "[]",
		"include-recommends": "false",
		"include-suggests":   "false",
		"arch":               "",
		"jobs":               "", // NumCPU — varies by machine
		"manifest":           "",
		"summary":            "/tmp/build-summary.md",
		"sbom-spdx":          "",
		"sbom-cyclonedx":     "",
		"source-date-epoch":  "0",
		"deduplicate":        "true",
		"dedup-strategy":     "hardlink",
		"keep-going":         "false",
		"overwrite":          "false",
		"merge":              "false",
	}

	for name, def := range want {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("flag --%s is not registered", name)
			continue
		}
		if def != "" && f.DefValue != def {
			t.Errorf("flag --%s default: got %q want %q", name, f.DefValue, def)
		}
	}

	// The dead flag must be gone.
	if cmd.Flags().Lookup("sbom-format") != nil {
		t.Error("--sbom-format should have been removed")
	}
}

// TestRootCmdParsesValues confirms flags accept values (i.e. they bind, not just exist).
func TestRootCmdParsesValues(t *testing.T) {
	cmd := newRootCmd()
	args := []string{
		"--source-root", "/mnt/x",
		"--jobs", "4",
		"--exclude-all",
		"--dedup-strategy", "none",
		"--arch", "arm64",
		"curl", "/export",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if got, _ := cmd.Flags().GetString("source-root"); got != "/mnt/x" {
		t.Errorf("source-root: got %q", got)
	}
	if got, _ := cmd.Flags().GetInt("jobs"); got != 4 {
		t.Errorf("jobs: got %d", got)
	}
	if got, _ := cmd.Flags().GetBool("exclude-all"); !got {
		t.Errorf("exclude-all not set")
	}
	if got, _ := cmd.Flags().GetString("dedup-strategy"); got != "none" {
		t.Errorf("dedup-strategy: got %q", got)
	}
	if got, _ := cmd.Flags().GetString("arch"); got != "arm64" {
		t.Errorf("arch: got %q", got)
	}
	// Positionals remain for the arg-grammar resolver.
	if rest := cmd.Flags().Args(); len(rest) != 2 || rest[0] != "curl" || rest[1] != "/export" {
		t.Errorf("positionals: got %v want [curl /export]", rest)
	}
}
