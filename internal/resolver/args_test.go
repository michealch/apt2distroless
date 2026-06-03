// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package resolver

import (
	"strings"
	"testing"
)

func TestBuildAptCacheArgs(t *testing.T) {
	contains := func(args []string, want string) bool {
		return strings.Contains(strings.Join(args, " "), want)
	}

	t.Run("defaults exclude recommends and suggests", func(t *testing.T) {
		args := buildAptCacheArgs("curl", false, false, "/")
		for _, want := range []string{"depends", "--recurse", "--no-recommends", "--no-suggests"} {
			if !contains(args, want) {
				t.Errorf("args %v missing %q", args, want)
			}
		}
		if args[len(args)-1] != "curl" {
			t.Errorf("last arg should be the root, got %v", args)
		}
		if contains(args, "Dir=") {
			t.Errorf("source-root / must not add -o Dir=, got %v", args)
		}
	})

	t.Run("include-recommends drops --no-recommends", func(t *testing.T) {
		args := buildAptCacheArgs("curl", true, false, "/")
		if contains(args, "--no-recommends") {
			t.Errorf("--include-recommends should drop --no-recommends, got %v", args)
		}
		if !contains(args, "--no-suggests") {
			t.Errorf("--no-suggests should remain, got %v", args)
		}
	})

	t.Run("include-suggests drops --no-suggests", func(t *testing.T) {
		args := buildAptCacheArgs("curl", false, true, "/")
		if contains(args, "--no-suggests") {
			t.Errorf("--include-suggests should drop --no-suggests, got %v", args)
		}
		if !contains(args, "--no-recommends") {
			t.Errorf("--no-recommends should remain, got %v", args)
		}
	})

	t.Run("non-host source-root adds -o Dir= prefix", func(t *testing.T) {
		args := buildAptCacheArgs("curl", false, false, "/mnt/root")
		if len(args) < 2 || args[0] != "-o" || args[1] != "Dir=/mnt/root" {
			t.Fatalf("expected -o Dir=/mnt/root prefix, got %v", args)
		}
		if args[len(args)-1] != "curl" {
			t.Errorf("last arg should be the root, got %v", args)
		}
	})
}
