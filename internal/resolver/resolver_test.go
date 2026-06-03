// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package resolver

import "testing"

func TestParseAptCacheOutput(t *testing.T) {
	input := `curl
  Depends: libc6
  Depends: libssl3
  |Depends: libssl1.1
  Depends: zlib1g
 <virtual-pkg>
  Recommends: ca-certificates
libssl3
  Depends: libc6
  Depends: libgcc-s1
libc6
zlib1g
libgcc-s1
`
	got := parseAptCacheOutput(input)
	// Must include real packages, must NOT include virtual ones.
	has := func(name string) bool {
		for _, n := range got {
			if n == name {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"curl", "libc6", "libssl3", "libssl1.1", "zlib1g", "libgcc-s1"} {
		if !has(want) {
			t.Errorf("expected %q in output, got: %v", want, got)
		}
	}
	if has("<virtual-pkg>") || has("virtual-pkg") {
		t.Errorf("virtual package should not appear in output")
	}
}

func TestFakeResolver(t *testing.T) {
	f := &FakeResolver{Result: []string{"zzz", "aaa", "mmm"}}
	got, err := f.Closure(nil)
	if err != nil {
		t.Fatal(err)
	}
	// Must be sorted
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Errorf("result not sorted: %v", got)
		}
	}
}

func TestParseAptCacheDeduplicated(t *testing.T) {
	// Same package appearing in multiple dependency chains.
	input := "libc6\n  Depends: libc6\nlibc6\n"
	got := parseAptCacheOutput(input)
	count := 0
	for _, n := range got {
		if n == "libc6" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("libc6 appears %d times, want 1", count)
	}
}
