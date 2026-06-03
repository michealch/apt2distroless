// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package dedup

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"github.com/michealch/apt2distroless/internal/dpkg"
)

// Deduper performs content-based deduplication over a set of regular file Entries.
type Deduper struct {
	Strategy string // "hardlink" | "none"
	Jobs     int
}

// Run deduplicates entries in place. Returns the number of files hard-linked.
// Only KindRegular entries are considered; symlinks and dirs are skipped.
func (d *Deduper) Run(entries []dpkg.Entry) (linked int, warnings []string, err error) {
	if d.Strategy == "none" || len(entries) == 0 {
		return 0, nil, nil
	}

	// Collect only regular file entries.
	regs := make([]dpkg.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Kind == dpkg.KindRegular {
			regs = append(regs, e)
		}
	}

	// Phase B1: bucket by size (prefilter).
	bySize := make(map[int64][]dpkg.Entry)
	for _, e := range regs {
		bySize[e.Size] = append(bySize[e.Size], e)
	}

	// Gather size-collision groups (>1 entry per size).
	type group struct {
		size    int64
		entries []dpkg.Entry
	}
	var collisions []group
	for sz, es := range bySize {
		if len(es) > 1 {
			collisions = append(collisions, group{size: sz, entries: es})
		}
	}
	if len(collisions) == 0 {
		return 0, nil, nil
	}

	// Phase B2: hash each file in each collision group in parallel.
	type hashResult struct {
		entry dpkg.Entry
		hash  string
		err   error
	}

	jobs := d.Jobs
	if jobs < 1 {
		jobs = 1
	}

	workCh := make(chan dpkg.Entry)
	resCh := make(chan hashResult)
	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range workCh {
				h, herr := hashFile(e.Dst)
				resCh <- hashResult{entry: e, hash: h, err: herr}
			}
		}()
	}
	go func() {
		for _, g := range collisions {
			for _, e := range g.entries {
				workCh <- e
			}
		}
		close(workCh)
		wg.Wait()
		close(resCh)
	}()

	// Collect hashes.
	type dedupKey struct {
		sha256  string
		mode    os.FileMode
		uid     int
		gid     int
		xattrFP string
	}
	byKey := make(map[dedupKey][]dpkg.Entry)
	for r := range resCh {
		if r.err != nil {
			warnings = append(warnings, fmt.Sprintf("dedup hash %s: %v", r.entry.Dst, r.err))
			continue
		}
		k := dedupKey{
			sha256:  r.hash,
			mode:    r.entry.Mode,
			uid:     r.entry.Uid,
			gid:     r.entry.Gid,
			xattrFP: r.entry.XattrFP,
		}
		byKey[k] = append(byKey[k], r.entry)
	}

	// Phase B3: for each key group >1, sort by Dst and hardlink non-winners to winner.
	for _, es := range byKey {
		if len(es) < 2 {
			continue
		}
		sort.Slice(es, func(i, j int) bool { return es[i].Dst < es[j].Dst })
		winner := es[0]
		for _, loser := range es[1:] {
			if err := hardlink(winner.Dst, loser.Dst); err != nil {
				warnings = append(warnings, fmt.Sprintf("hardlink %s → %s: %v", loser.Dst, winner.Dst, err))
			} else {
				linked++
			}
		}
	}
	return linked, warnings, nil
}

// hardlink replaces dst with a hard link to src.
// On EXDEV (cross-device), falls back to a plain copy.
func hardlink(src, dst string) error {
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove before hardlink: %w", err)
	}
	if err := os.Link(src, dst); err != nil {
		if isEXDEV(err) {
			// Cross-device: fall back to copy (target is on a different filesystem).
			return copyFallback(src, dst)
		}
		return err
	}
	return nil
}

func copyFallback(src, dst string) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sf.Close() }()
	fi, err := sf.Stat()
	if err != nil {
		return err
	}
	df, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode())
	if err != nil {
		return err
	}
	_, err = io.Copy(df, sf)
	if cerr := df.Close(); err == nil {
		err = cerr
	}
	return err
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func isEXDEV(err error) bool {
	var errno interface{ Unwrap() error }
	// Check syscall.EXDEV via errors.As on *os.LinkError.
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err.Error() == "invalid cross-device link"
	}
	_ = errno
	return false
}
