// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package dpkg

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Index is an in-memory index of installed packages built from
// <sourceRoot>/var/lib/dpkg/status.
type Index struct {
	byName map[string]*Package
}

// ReadStatus parses <sourceRoot>/var/lib/dpkg/status and returns an Index.
func ReadStatus(sourceRoot string) (*Index, error) {
	path := strings.TrimRight(sourceRoot, "/") + "/var/lib/dpkg/status"
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dpkg status %s: %w", path, err)
	}
	return parseStatus(data)
}

func parseStatus(data []byte) (*Index, error) {
	ix := &Index{byName: make(map[string]*Package)}

	// Stanzas are separated by blank lines ("\n\n").
	// We walk byte-by-byte to preserve each verbatim block.
	stanzas := splitStanzas(data)
	for _, raw := range stanzas {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		p, err := parseStanza(raw)
		if err != nil {
			return nil, err
		}
		if p == nil {
			continue
		}
		// Only index packages that are fully installed.
		ix.byName[p.Name] = p
	}
	return ix, nil
}

// splitStanzas splits a dpkg status file into individual stanza byte slices.
// Each stanza is terminated by a blank line (two consecutive newlines).
func splitStanzas(data []byte) [][]byte {
	var stanzas [][]byte
	sep := []byte("\n\n")
	for {
		i := bytes.Index(data, sep)
		if i < 0 {
			if len(data) > 0 {
				stanzas = append(stanzas, data)
			}
			break
		}
		stanzas = append(stanzas, data[:i+1]) // include trailing newline
		data = data[i+2:]
	}
	return stanzas
}

// parseStanza extracts a Package from one raw stanza block.
// Returns nil (not an error) for non-installed or empty stanzas.
func parseStanza(raw []byte) (*Package, error) {
	fields := make(map[string]string)
	lines := strings.Split(string(raw), "\n")
	var key string
	for _, line := range lines {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			// continuation line
			if key != "" {
				fields[key] += "\n" + strings.TrimSpace(line)
			}
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key = strings.TrimSpace(line[:colon])
		fields[key] = strings.TrimSpace(line[colon+1:])
	}

	name := fields["Package"]
	if name == "" {
		return nil, nil
	}

	// Only include packages that are installed (status field contains "installed").
	status := fields["Status"]
	if !strings.Contains(status, "installed") {
		return nil, nil
	}

	p := &Package{
		Name:         name,
		Version:      fields["Version"],
		Architecture: fields["Architecture"],
		MultiArch:    fields["Multi-Arch"],
		Maintainer:   fields["Maintainer"],
		Depends:      ParseDepends(fields["Depends"]),
		PreDepends:   ParseDepends(fields["Pre-Depends"]),
		Stanza:       append([]byte(nil), raw...), // verbatim copy
	}
	return p, nil
}

// Get returns the Package for name, or false if not found.
func (ix *Index) Get(name string) (*Package, bool) {
	p, ok := ix.byName[name]
	return p, ok
}

// Installed reports whether name is in the installed index.
func (ix *Index) Installed(name string) bool {
	_, ok := ix.byName[name]
	return ok
}

// All returns all packages in the index sorted by name.
func (ix *Index) All() []*Package {
	names := make([]string, 0, len(ix.byName))
	for n := range ix.byName {
		names = append(names, n)
	}
	sortStr(names)
	out := make([]*Package, len(names))
	for i, n := range names {
		out[i] = ix.byName[n]
	}
	return out
}

func sortStr(ss []string) { sort.Strings(ss) }
