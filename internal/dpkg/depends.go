// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package dpkg

import "strings"

// ParseDepends parses a Debian Depends: or Pre-Depends: field value into
// a slice of AtomGroups. Each comma-separated clause becomes one AtomGroup;
// pipe-separated alternatives within a clause become Atoms within that group.
//
// Example: "libc6 (>= 2.14), libssl3 | libssl1.1:amd64"
// → [{libc6}, {libssl3, libssl1.1:amd64}]
func ParseDepends(field string) []AtomGroup {
	if strings.TrimSpace(field) == "" {
		return nil
	}
	clauses := strings.Split(field, ",")
	groups := make([]AtomGroup, 0, len(clauses))
	for _, clause := range clauses {
		alts := strings.Split(clause, "|")
		group := AtomGroup{Alts: make([]Atom, 0, len(alts))}
		for _, alt := range alts {
			atom := parseAtom(strings.TrimSpace(alt))
			if atom.Name != "" {
				group.Alts = append(group.Alts, atom)
			}
		}
		if len(group.Alts) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

// parseAtom strips version constraints and arch qualifiers from a single dep atom.
// "libssl3 (>= 3.0.0)"  → Atom{Name:"libssl3"}
// "foo:amd64"            → Atom{Name:"foo", ArchQual:"amd64"}
// "bar:any (>= 1.0)"    → Atom{Name:"bar", ArchQual:"any"}
func parseAtom(s string) Atom {
	raw := s

	// strip version constraint: everything from '(' onward
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	// strip arch qualifier: "name:qual"
	archQual := ""
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		archQual = s[i+1:]
		s = s[:i]
	}
	name := strings.TrimSpace(s)
	return Atom{Name: name, ArchQual: archQual, Raw: raw}
}
