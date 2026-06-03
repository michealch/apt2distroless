// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package config

import (
	"testing"
)

func noInstalled(string) bool { return false }

func pkgInstalled(pkgs ...string) func(string) bool {
	set := make(map[string]struct{})
	for _, p := range pkgs {
		set[p] = struct{}{}
	}
	return func(s string) bool { _, ok := set[s]; return ok }
}

func TestResolveArgs(t *testing.T) {
	cases := []struct {
		name        string
		rootFlags   []string
		positionals []string
		targetFlag  string
		isInstalled func(string) bool
		wantRoots   []string
		wantTarget  string
		wantErr     string
	}{
		{
			name:        "single root + target positional",
			positionals: []string{"curl", "/export"},
			isInstalled: noInstalled,
			wantRoots:   []string{"curl"},
			wantTarget:  "/export",
		},
		{
			name:        "multi root positional + target",
			positionals: []string{"curl", "openssl", "/export"},
			isInstalled: noInstalled,
			wantRoots:   []string{"curl", "openssl"},
			wantTarget:  "/export",
		},
		{
			name:        "--target given, all positionals are roots",
			rootFlags:   []string{"curl"},
			positionals: []string{"openssl"},
			targetFlag:  "/export",
			isInstalled: noInstalled,
			wantRoots:   []string{"curl", "openssl"},
			wantTarget:  "/export",
		},
		{
			name:        "--root flags deduped with positional roots",
			rootFlags:   []string{"curl", "openssl"},
			positionals: []string{"curl", "/export"},
			isInstalled: noInstalled,
			wantRoots:   []string{"curl", "openssl"},
			wantTarget:  "/export",
		},
		{
			name:        "roots sorted",
			positionals: []string{"zzz", "aaa", "/export"},
			isInstalled: noInstalled,
			wantRoots:   []string{"aaa", "zzz"},
			wantTarget:  "/export",
		},
		{
			name:        "footgun guard: last positional is installed package",
			positionals: []string{"curl", "openssl"},
			isInstalled: pkgInstalled("openssl"),
			wantErr:     `"openssl" looks like a package`,
		},
		{
			name:        "footgun guard: path with slash passes through",
			positionals: []string{"curl", "./openssl"},
			isInstalled: pkgInstalled("openssl"),
			wantRoots:   []string{"curl"},
			wantTarget:  "./openssl",
		},
		{
			name:        "no positionals, no --target",
			positionals: []string{},
			isInstalled: noInstalled,
			wantErr:     "missing required argument",
		},
		{
			name:        "only target, no roots",
			targetFlag:  "/export",
			isInstalled: noInstalled,
			wantErr:     "no root packages specified",
		},
		{
			name:        "single positional only (no root)",
			positionals: []string{"/export"},
			isInstalled: noInstalled,
			wantErr:     "no root packages specified",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roots, target, err := resolveArgs(tc.rootFlags, tc.positionals, tc.targetFlag, tc.isInstalled)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if target != tc.wantTarget {
				t.Errorf("target: got %q, want %q", target, tc.wantTarget)
			}
			if !sliceEqual(roots, tc.wantRoots) {
				t.Errorf("roots: got %v, want %v", roots, tc.wantRoots)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
