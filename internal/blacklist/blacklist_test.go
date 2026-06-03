// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package blacklist

import (
	"os"
	"testing"

	"github.com/michealch/apt2distroless/internal/config"
)

func TestBuildBuiltIn(t *testing.T) {
	s, err := Build(config.BlacklistConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Blocked("bash", nil) {
		t.Error("bash should be blocked by built-in")
	}
	if s.Blocked("libssl3", nil) {
		t.Error("libssl3 should not be blocked by built-in")
	}
}

func TestBuildAdd(t *testing.T) {
	s, err := Build(config.BlacklistConfig{Add: []string{"myservice"}})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Blocked("myservice", nil) {
		t.Error("myservice should be blocked after --blacklist-add")
	}
}

func TestBuildRemove(t *testing.T) {
	s, err := Build(config.BlacklistConfig{Remove: []string{"bash"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Blocked("bash", nil) {
		t.Error("bash should not be blocked after --blacklist-remove")
	}
}

func TestBuildAddWinsTie(t *testing.T) {
	// Remove bash then re-add it — add should win.
	s, err := Build(config.BlacklistConfig{Remove: []string{"bash"}, Add: []string{"bash"}})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Blocked("bash", nil) {
		t.Error("bash should be blocked: add wins over remove")
	}
}

func TestBuildFile(t *testing.T) {
	f, err := os.CreateTemp("", "blacklist-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("myapp\nanother\n# comment\n\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	s, err := Build(config.BlacklistConfig{File: f.Name()})
	if err != nil {
		t.Fatal(err)
	}
	// File replaces built-in — bash no longer blocked.
	if s.Blocked("bash", nil) {
		t.Error("bash should not be blocked when file replaces built-in")
	}
	if !s.Blocked("myapp", nil) {
		t.Error("myapp should be blocked from file")
	}
}

func TestBlockedRootNeverBlocked(t *testing.T) {
	s, err := Build(config.BlacklistConfig{})
	if err != nil {
		t.Fatal(err)
	}
	// bash is in the built-in list, but if it's a root it passes.
	if s.Blocked("bash", []string{"bash"}) {
		t.Error("root package should never be blocked even if in built-in list")
	}
}
