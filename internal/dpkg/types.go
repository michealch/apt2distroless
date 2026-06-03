// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package dpkg

import "os"

// Package is the parsed representation of one stanza from /var/lib/dpkg/status.
type Package struct {
	Name         string
	Version      string
	Architecture string // "amd64" | "arm64" | "all" | ...
	MultiArch    string // "same" | "foreign" | "allowed" | "no" | ""
	Maintainer   string
	Depends      []AtomGroup // each group holds alternatives (usually one)
	PreDepends   []AtomGroup
	Stanza       []byte // verbatim status block (Package: … to blank line)
}

// AtomGroup is a list of alternatives from a single dependency clause.
// "a | b | c" → one AtomGroup with three Atoms.
// "a"         → one AtomGroup with one Atom.
type AtomGroup struct {
	Alts []Atom
}

// Atom is a single dependency name after stripping version constraints and
// architecture qualifiers. It is used only for the broken-edge check and SBOM
// relationships — dependency resolution is delegated to apt-cache.
type Atom struct {
	Name     string // bare package name (e.g. "libc6")
	ArchQual string // "" | "any" | "native" | "amd64" | ...
	Raw      string // original text before stripping, for diagnostics
}

// EntryKind distinguishes the type of a filesystem entry.
type EntryKind uint8

const (
	KindRegular EntryKind = iota
	KindSymlink
	KindDir
)

// Entry describes one filesystem entry to materialize in the target.
type Entry struct {
	Pkg      string
	Src      string // absolute path under source-root
	Dst      string // absolute path under target
	Rel      string // rootfs-absolute path as dpkg reports it
	Kind     EntryKind
	Mode     os.FileMode
	Uid, Gid int
	Size     int64
	LinkText string // verbatim link destination for KindSymlink
	XattrFP  string // fingerprint of security.*/user.* xattrs (KindRegular)
}

// FileError records a per-file copy failure.
type FileError struct {
	Path string
	Err  error
}

// BrokenEdge records a copied package that depends on a blacklisted/excluded one.
type BrokenEdge struct {
	From string
	To   string
}
