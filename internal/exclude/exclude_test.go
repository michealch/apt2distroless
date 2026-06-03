// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package exclude

import (
	"testing"

	"github.com/michealch/apt2distroless/internal/config"
)

func TestExcludeDocs(t *testing.T) {
	m := Build(config.ExcludeConfig{Docs: true})

	cases := []struct {
		path     string
		excluded bool
	}{
		{"/usr/share/doc/curl/README", true},
		{"/usr/share/doc/curl/copyright", false}, // carve-out
		{"/usr/bin/curl", false},
		{"/usr/share/man/man1/curl.1", false}, // man not excluded
	}
	for _, tc := range cases {
		got := m.Excluded(tc.path)
		if got != tc.excluded {
			t.Errorf("Excluded(%q) = %v, want %v", tc.path, got, tc.excluded)
		}
	}
}

func TestExcludeAll(t *testing.T) {
	m := Build(config.ExcludeConfig{All: true})
	if !m.Excluded("/usr/share/man/man1/ls.1") {
		t.Error("/usr/share/man should be excluded with --exclude-all")
	}
	if !m.Excluded("/usr/share/locale/en/LC_MESSAGES/foo.mo") {
		t.Error("/usr/share/locale should be excluded with --exclude-all")
	}
	if m.Excluded("/usr/share/doc/curl/copyright") {
		t.Error("copyright should never be excluded")
	}
}

func TestExcludeIcons(t *testing.T) {
	m := Build(config.ExcludeConfig{Icons: true})
	for _, p := range []string{
		"/usr/share/icons/hicolor/48x48/foo.png",
		"/usr/share/pixmaps/app.xpm",
		"/usr/share/applications/app.desktop",
		"/usr/share/mime/text/plain.xml",
	} {
		if !m.Excluded(p) {
			t.Errorf("expected %q to be excluded with --exclude-icons", p)
		}
	}
}

func TestExcludeCustomPath(t *testing.T) {
	m := Build(config.ExcludeConfig{Paths: []string{"/opt/vendor"}})
	if !m.Excluded("/opt/vendor/lib/foo.so") {
		t.Error("/opt/vendor subtree should be excluded")
	}
	if m.Excluded("/opt/other/lib/foo.so") {
		t.Error("/opt/other should not be excluded")
	}
}

func TestExcludeDpkgMetadata(t *testing.T) {
	// Even if /var/lib is somehow in an exclude list, dpkg metadata passes.
	m := Build(config.ExcludeConfig{Paths: []string{"/var/lib"}})
	if m.Excluded("/var/lib/dpkg/status") {
		t.Error("/var/lib/dpkg paths should never be excluded")
	}
}
