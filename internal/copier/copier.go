// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package copier

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/michealch/apt2distroless/internal/dpkg"
	"github.com/michealch/apt2distroless/internal/exclude"
	"github.com/michealch/apt2distroless/internal/reproducible"
	"golang.org/x/sys/unix"
)

// bufPool supplies reusable 512 KiB copy buffers shared across copy workers,
// reducing read/write syscalls on large files versus io.Copy's 32 KiB default.
var bufPool = sync.Pool{New: func() any { b := make([]byte, 512<<10); return &b }}

// PackageResult holds the outcome of copying one package.
type PackageResult struct {
	Pkg         dpkg.Package
	Entries     []dpkg.Entry
	FilesCopied int
	BytesCopied int64
	Failures    []dpkg.FileError
	// MissingSources are paths that dpkg -L reported for the package but which
	// do not exist on disk. This is expected on slim Debian bases where dpkg
	// path-excludes strip changelogs, manpages, lintian overrides, etc. These
	// are skipped (non-fatal), unlike genuine copy errors in Failures.
	MissingSources []string
}

// Copier copies packages from a source root into a target directory.
type Copier struct {
	SourceRoot  string
	Target      string
	Epoch       int64
	Exclude     exclude.Matcher
	Jobs        int
	IsRoot      bool // os.Geteuid() == 0
	Deduplicate bool // whether dedup will run later (controls xattr fingerprint)
}

// New creates a Copier and emits a one-time warning if not running as root.
func New(sourceRoot, target string, epoch int64, excl exclude.Matcher, jobs int, dedup bool) *Copier {
	isRoot := os.Geteuid() == 0
	return &Copier{
		SourceRoot:  strings.TrimRight(sourceRoot, "/"),
		Target:      strings.TrimRight(target, "/"),
		Epoch:       epoch,
		Exclude:     excl,
		Jobs:        jobs,
		IsRoot:      isRoot,
		Deduplicate: dedup,
	}
}

// CopyPackages copies all packages concurrently using a worker pool.
// Results are returned sorted by package name.
func (c *Copier) CopyPackages(packages []*dpkg.Package, lister dpkg.FileLister) []PackageResult {
	type work struct {
		idx int
		pkg *dpkg.Package
	}
	type result struct {
		idx int
		res PackageResult
	}

	jobs := c.Jobs
	if jobs < 1 {
		jobs = 1
	}

	workCh := make(chan work, len(packages))
	resCh := make(chan result, len(packages))

	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range workCh {
				resCh <- result{idx: w.idx, res: c.CopyPackage(w.pkg, lister)}
			}
		}()
	}

	for i, p := range packages {
		workCh <- work{idx: i, pkg: p}
	}
	close(workCh)

	go func() {
		wg.Wait()
		close(resCh)
	}()

	results := make([]PackageResult, len(packages))
	for r := range resCh {
		results[r.idx] = r.res
	}
	return results
}

// CopyPackage copies all files for one package and returns the result.
func (c *Copier) CopyPackage(p *dpkg.Package, lister dpkg.FileLister) PackageResult {
	res := PackageResult{Pkg: *p}

	files, err := lister.List(p.Name)
	if err != nil {
		res.Failures = append(res.Failures, dpkg.FileError{Path: p.Name, Err: err})
		return res
	}

	// Track canonical source paths already processed to avoid duplicate copies
	// (path-idempotency dedup — no hashing needed here).
	processed := make(map[string]struct{})

	for _, rel := range files {
		if c.Exclude.Excluded(rel) {
			continue
		}
		src := c.SourceRoot + rel
		dst := c.Target + rel

		// Resolve canonical source path for idempotency.
		canonical := src

		fi, err := os.Lstat(src)
		if err != nil {
			if os.IsNotExist(err) {
				// dpkg listed a path that isn't on disk — expected on slim bases
				// (path-excludes). Non-fatal: record and skip.
				res.MissingSources = append(res.MissingSources, rel)
			} else {
				// A genuine stat error (permission, I/O) — a real failure.
				res.Failures = append(res.Failures, dpkg.FileError{Path: rel, Err: err})
			}
			continue
		}

		if fi.Mode()&os.ModeSymlink != 0 {
			// Symlink — verbatim, no target chasing.
			if _, seen := processed[canonical]; seen {
				continue
			}
			processed[canonical] = struct{}{}
			entry, ferr := c.copySymlink(rel, src, dst, fi)
			if ferr != nil {
				res.Failures = append(res.Failures, dpkg.FileError{Path: rel, Err: ferr})
			} else {
				res.Entries = append(res.Entries, entry)
			}
			continue
		}

		if fi.IsDir() {
			entry, ferr := c.copyDir(rel, src, dst, fi)
			if ferr != nil {
				res.Failures = append(res.Failures, dpkg.FileError{Path: rel, Err: ferr})
			} else {
				res.Entries = append(res.Entries, entry)
			}
			continue
		}

		// Regular file.
		if _, seen := processed[canonical]; seen {
			continue
		}
		processed[canonical] = struct{}{}
		entry, ferr := c.copyRegular(rel, src, dst, fi)
		if ferr != nil {
			res.Failures = append(res.Failures, dpkg.FileError{Path: rel, Err: ferr})
		} else {
			res.Entries = append(res.Entries, entry)
			res.FilesCopied++
			res.BytesCopied += fi.Size()
		}
	}
	return res
}

func (c *Copier) copyRegular(rel, src, dst string, fi os.FileInfo) (dpkg.Entry, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return dpkg.Entry{}, fmt.Errorf("mkdir parent %s: %w", dst, err)
	}

	// Open source first.
	sf, err := os.Open(src)
	if err != nil {
		return dpkg.Entry{}, fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = sf.Close() }()

	// Create/truncate destination.
	df, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode())
	if err != nil {
		return dpkg.Entry{}, fmt.Errorf("create %s: %w", dst, err)
	}
	// When dedup will run later, hash the content while copying so dedup never
	// has to re-read the file. Otherwise copy in-kernel via copy_file_range
	// (faster, and reflinks on copy-on-write filesystems).
	var sha string
	if c.Deduplicate {
		h := sha256.New()
		buf := bufPool.Get().(*[]byte)
		_, cerr := io.CopyBuffer(io.MultiWriter(df, h), sf, *buf)
		bufPool.Put(buf)
		if cerr != nil {
			_ = df.Close()
			return dpkg.Entry{}, fmt.Errorf("copy %s → %s: %w", src, dst, cerr)
		}
		sha = hex.EncodeToString(h.Sum(nil))
	} else if cerr := copyContent(df, sf, fi.Size()); cerr != nil {
		_ = df.Close()
		return dpkg.Entry{}, fmt.Errorf("copy %s → %s: %w", src, dst, cerr)
	}
	if err := df.Close(); err != nil {
		return dpkg.Entry{}, fmt.Errorf("close %s: %w", dst, err)
	}

	// Preserve mode (already set via OpenFile but chmod handles setuid/setgid/sticky).
	if err := os.Chmod(dst, fi.Mode()); err != nil {
		return dpkg.Entry{}, fmt.Errorf("chmod %s: %w", dst, err)
	}

	// Preserve uid/gid if running as root.
	uid, gid := fileUID(fi), fileGID(fi)
	if c.IsRoot {
		if err := unix.Lchown(dst, uid, gid); err != nil {
			// Warn-don't-fail: log-worthy but not fatal.
			_ = err // caller can check via Failures if needed; here we continue
		}
	}

	// Copy security.* and user.* xattrs (warn-don't-fail).
	xattrWarns := reproducible.CopyXattrs(src, dst)
	_ = xattrWarns

	// Normalize mtime.
	if err := reproducible.StampMTime(dst, c.Epoch); err != nil {
		return dpkg.Entry{}, fmt.Errorf("stamp mtime %s: %w", dst, err)
	}

	fp := ""
	if c.Deduplicate {
		fp = reproducible.XattrFingerprint(dst)
	}

	return dpkg.Entry{
		Pkg:     fi.Name(),
		Src:     src,
		Dst:     dst,
		Rel:     rel,
		Kind:    dpkg.KindRegular,
		Mode:    fi.Mode(),
		Uid:     uid,
		Gid:     gid,
		Size:    fi.Size(),
		XattrFP: fp,
		SHA256:  sha, // empty when dedup is disabled (no hash computed)
	}, nil
}

// copyContent copies sf→df with copy_file_range (in-kernel, reflink-capable),
// falling back to a buffered userspace copy when the syscall is unsupported or
// the files live on different filesystems. Used only when dedup is disabled —
// the dedup path hashes while copying instead.
func copyContent(df, sf *os.File, size int64) error {
	remaining := size
	for remaining > 0 {
		chunk := remaining
		if chunk > 1<<30 { // cap so the int length arg can't overflow on 32-bit
			chunk = 1 << 30
		}
		n, err := unix.CopyFileRange(int(sf.Fd()), nil, int(df.Fd()), nil, int(chunk), 0)
		switch err {
		case nil:
			// ok
		case unix.EINTR:
			continue
		case unix.ENOSYS, unix.EXDEV, unix.EINVAL, unix.EOPNOTSUPP, unix.EPERM, unix.EBADF:
			return bufferedCopy(df, sf) // resumes from the current file offsets
		default:
			return err
		}
		if n == 0 {
			break // EOF
		}
		remaining -= int64(n)
	}
	return nil
}

func bufferedCopy(df, sf *os.File) error {
	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)
	_, err := io.CopyBuffer(df, sf, *buf)
	return err
}

func (c *Copier) copySymlink(rel, src, dst string, fi os.FileInfo) (dpkg.Entry, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return dpkg.Entry{}, fmt.Errorf("mkdir parent %s: %w", dst, err)
	}

	// Read the verbatim link text — no readlink -f, no chasing.
	linkText, err := os.Readlink(src)
	if err != nil {
		return dpkg.Entry{}, fmt.Errorf("readlink %s: %w", src, err)
	}

	// Remove existing target if present (idempotent re-run).
	_ = os.Remove(dst)

	if err := os.Symlink(linkText, dst); err != nil {
		return dpkg.Entry{}, fmt.Errorf("symlink %s → %s: %w", dst, linkText, err)
	}

	// Preserve uid/gid on the symlink itself (Lchown).
	uid, gid := fileUID(fi), fileGID(fi)
	if c.IsRoot {
		_ = unix.Lchown(dst, uid, gid)
	}

	// Copy xattrs on the symlink (Lsetxattr).
	_ = reproducible.CopyXattrs(src, dst)

	// Normalize mtime on the symlink.
	_ = reproducible.StampMTime(dst, c.Epoch)

	return dpkg.Entry{
		Src:      src,
		Dst:      dst,
		Rel:      rel,
		Kind:     dpkg.KindSymlink,
		Uid:      uid,
		Gid:      gid,
		LinkText: linkText,
	}, nil
}

func (c *Copier) copyDir(rel, src, dst string, fi os.FileInfo) (dpkg.Entry, error) {
	if err := os.MkdirAll(dst, fi.Mode()); err != nil {
		return dpkg.Entry{}, fmt.Errorf("mkdir %s: %w", dst, err)
	}
	if err := os.Chmod(dst, fi.Mode()); err != nil {
		return dpkg.Entry{}, fmt.Errorf("chmod dir %s: %w", dst, err)
	}

	uid, gid := fileUID(fi), fileGID(fi)
	if c.IsRoot {
		_ = unix.Lchown(dst, uid, gid)
	}
	_ = reproducible.CopyXattrs(src, dst)
	// NOTE: dir mtime is stamped in Phase D (bottom-up after all copies) by StampDirMTimes.

	return dpkg.Entry{
		Src:  src,
		Dst:  dst,
		Rel:  rel,
		Kind: dpkg.KindDir,
		Mode: fi.Mode(),
		Uid:  uid,
		Gid:  gid,
	}, nil
}

// StampDirMTimes performs Phase D: walk the target directory bottom-up and
// normalize mtime on every directory to epoch.
func StampDirMTimes(target string, epoch int64) error {
	// Collect all directories.
	var dirs []string
	err := filepath.WalkDir(target, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk target for dir mtime fixup: %w", err)
	}

	// Stamp deepest first (bottom-up): reverse the walk order.
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	for _, d := range dirs {
		if err := reproducible.StampMTime(d, epoch); err != nil {
			return err
		}
	}
	return nil
}

// CheckDanglingSymlinks walks the target and warns about symlinks that resolve
// to a missing path within the target. Returns a list of dangling symlink paths.
func CheckDanglingSymlinks(target string) []string {
	target = strings.TrimRight(target, "/")
	var dangling []string
	_ = filepath.WalkDir(target, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		linkText, err := os.Readlink(path)
		if err != nil {
			return nil
		}
		// Resolve within the target root.
		var resolved string
		if filepath.IsAbs(linkText) {
			resolved = target + linkText
		} else {
			resolved = filepath.Join(filepath.Dir(path), linkText)
		}
		if _, err := os.Lstat(resolved); os.IsNotExist(err) {
			dangling = append(dangling, path)
		}
		return nil
	})
	return dangling
}
