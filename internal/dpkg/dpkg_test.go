// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package dpkg

import (
	"testing"
)

// --- depends tests ---

func TestParseDepends(t *testing.T) {
	cases := []struct {
		in   string
		want []AtomGroup
	}{
		{
			in:   "",
			want: nil,
		},
		{
			in: "libc6 (>= 2.14)",
			want: []AtomGroup{
				{Alts: []Atom{{Name: "libc6", Raw: "libc6 (>= 2.14)"}}},
			},
		},
		{
			in: "libssl3 | libssl1.1:amd64",
			want: []AtomGroup{
				{Alts: []Atom{
					{Name: "libssl3", Raw: "libssl3"},
					{Name: "libssl1.1", ArchQual: "amd64", Raw: "libssl1.1:amd64"},
				}},
			},
		},
		{
			in: "libc6 (>= 2.14), libssl3",
			want: []AtomGroup{
				{Alts: []Atom{{Name: "libc6", Raw: "libc6 (>= 2.14)"}}},
				{Alts: []Atom{{Name: "libssl3", Raw: "libssl3"}}},
			},
		},
		{
			in: "awk",
			want: []AtomGroup{
				{Alts: []Atom{{Name: "awk", Raw: "awk"}}},
			},
		},
		{
			in: "foo:any (>= 1.0)",
			want: []AtomGroup{
				{Alts: []Atom{{Name: "foo", ArchQual: "any", Raw: "foo:any (>= 1.0)"}}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := ParseDepends(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("ParseDepends(%q): got %d groups, want %d\ngot:  %+v\nwant: %+v",
					tc.in, len(got), len(tc.want), got, tc.want)
			}
			for i, g := range got {
				w := tc.want[i]
				if len(g.Alts) != len(w.Alts) {
					t.Errorf("group %d: got %d alts, want %d", i, len(g.Alts), len(w.Alts))
					continue
				}
				for j, a := range g.Alts {
					wa := w.Alts[j]
					if a.Name != wa.Name || a.ArchQual != wa.ArchQual {
						t.Errorf("group %d alt %d: got {%q %q}, want {%q %q}",
							i, j, a.Name, a.ArchQual, wa.Name, wa.ArchQual)
					}
				}
			}
		})
	}
}

// --- status parser tests ---

const syntheticStatus = `Package: pkga
Status: install ok installed
Version: 1.0-1
Architecture: amd64
Maintainer: Test <test@example.com>
Depends: libc6 (>= 2.14), pkgb
Pre-Depends: libssl3 | libssl1.1
Description: package A
 A fake test package.

Package: pkgb
Status: install ok installed
Version: 2.0-1
Architecture: all
Maintainer: Test <test@example.com>
Description: package B
 Architecture-independent.

Package: pkgc-removed
Status: deinstall ok config-files
Version: 3.0-1
Architecture: amd64
Description: this should not be indexed

`

func TestParseStatus(t *testing.T) {
	ix, err := parseStatus([]byte(syntheticStatus))
	if err != nil {
		t.Fatalf("parseStatus: %v", err)
	}

	// pkgc-removed must NOT be indexed (not installed)
	if ix.Installed("pkgc-removed") {
		t.Error("pkgc-removed should not be installed")
	}

	p, ok := ix.Get("pkga")
	if !ok {
		t.Fatal("pkga not found in index")
	}
	if p.Version != "1.0-1" {
		t.Errorf("pkga version: got %q, want %q", p.Version, "1.0-1")
	}
	if p.Architecture != "amd64" {
		t.Errorf("pkga arch: got %q, want %q", p.Architecture, "amd64")
	}
	if len(p.Depends) != 2 {
		t.Errorf("pkga Depends: got %d groups, want 2", len(p.Depends))
	}
	if len(p.PreDepends) != 1 || len(p.PreDepends[0].Alts) != 2 {
		t.Errorf("pkga Pre-Depends: want 1 group with 2 alts, got %+v", p.PreDepends)
	}
	if len(p.Stanza) == 0 {
		t.Error("pkga Stanza should be non-empty")
	}

	pb, ok := ix.Get("pkgb")
	if !ok {
		t.Fatal("pkgb not found in index")
	}
	if pb.Architecture != "all" {
		t.Errorf("pkgb arch: got %q, want %q", pb.Architecture, "all")
	}

	all := ix.All()
	if len(all) != 2 {
		t.Errorf("All() returned %d packages, want 2", len(all))
	}
	if all[0].Name != "pkga" || all[1].Name != "pkgb" {
		t.Errorf("All() not sorted: %v", []string{all[0].Name, all[1].Name})
	}
}

// --- InfoFileNames tests ---

func TestInfoFileNames(t *testing.T) {
	cases := []struct {
		p    *Package
		ext  string
		want []string
	}{
		{&Package{Name: "curl", Architecture: "amd64"}, "list",
			[]string{"curl.list", "curl:amd64.list"}},
		{&Package{Name: "tzdata", Architecture: "all"}, "md5sums",
			[]string{"tzdata.md5sums"}},
		{&Package{Name: "foo", Architecture: "arm64"}, "conffiles",
			[]string{"foo.conffiles", "foo:arm64.conffiles"}},
	}
	for _, tc := range cases {
		got := InfoFileNames(tc.p, tc.ext)
		if !strSliceEqual(got, tc.want) {
			t.Errorf("InfoFileNames(%s, %s): got %v, want %v", tc.p.Name, tc.ext, got, tc.want)
		}
	}
}

func strSliceEqual(a, b []string) bool {
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
