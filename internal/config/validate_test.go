// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"valid hardlink", Config{DedupStrategy: "hardlink"}, ""},
		{"valid none", Config{DedupStrategy: "none"}, ""},
		{"unknown dedup strategy", Config{DedupStrategy: "symlink"}, "unknown --dedup-strategy"},
		{"empty dedup strategy rejected", Config{DedupStrategy: ""}, "unknown --dedup-strategy"},
		{"overwrite+merge conflict", Config{DedupStrategy: "hardlink", Overwrite: true, Merge: true}, "mutually exclusive"},
		{"overwrite only ok", Config{DedupStrategy: "none", Overwrite: true}, ""},
		{"merge only ok", Config{DedupStrategy: "none", Merge: true}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestApplyDefaultsManifestPath(t *testing.T) {
	c := &Config{
		Roots:         nil,
		Target:        "",
		SourceRoot:    "/nonexistent-root", // isInstalled probe will read nothing
		DedupStrategy: "hardlink",
	}
	// positional form: one root + target
	if err := c.Apply([]string{"curl", "/export"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if c.Target != "/export" {
		t.Errorf("target: got %q want /export", c.Target)
	}
	if want := "/export.manifest.jsonl"; c.ManifestPath != want {
		t.Errorf("default manifest path: got %q want %q", c.ManifestPath, want)
	}
	if len(c.Roots) != 1 || c.Roots[0] != "curl" {
		t.Errorf("roots: got %v want [curl]", c.Roots)
	}
}
