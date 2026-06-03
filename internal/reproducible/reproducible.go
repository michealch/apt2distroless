// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package reproducible

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"time"

	"golang.org/x/sys/unix"
)

// StampMTime sets the mtime of path to epoch seconds without following symlinks.
// atime is set to the same value for consistency.
func StampMTime(path string, epoch int64) error {
	// unix.Timespec's Sec/Nsec fields are int32 on 32-bit arches (386, arm) and
	// int64 on 64-bit ones. NsecToTimespec converts portably, avoiding an
	// int64→int32 literal that fails to compile on 32-bit targets.
	ts := unix.NsecToTimespec(epoch * int64(time.Second))
	times := []unix.Timespec{ts, ts} // atime, mtime
	if err := unix.UtimesNanoAt(unix.AT_FDCWD, path, times, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("stamp mtime %s: %w", path, err)
	}
	return nil
}

// XattrFingerprint returns a deterministic hash of all security.* and user.*
// xattr names and values on path, without following symlinks.
// Returns "" if there are no such xattrs or on error.
func XattrFingerprint(path string) string {
	// List xattrs without following symlinks.
	sz, err := unix.Llistxattr(path, nil)
	if err != nil || sz == 0 {
		return ""
	}
	buf := make([]byte, sz)
	n, err := unix.Llistxattr(path, buf)
	if err != nil || n == 0 {
		return ""
	}

	// Parse null-terminated names.
	var names []string
	for _, raw := range splitNullTerminated(buf[:n]) {
		if len(raw) > 0 && (hasPrefix(raw, "security.") || hasPrefix(raw, "user.")) {
			names = append(names, raw)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)

	h := sha256.New()
	for _, name := range names {
		vbuf := make([]byte, 512)
		vn, verr := unix.Lgetxattr(path, name, vbuf)
		if verr != nil {
			continue
		}
		_, _ = fmt.Fprintf(h, "%s=%x\n", name, vbuf[:vn])
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// CopyXattrs copies all security.* and user.* xattrs from src to dst,
// without following symlinks on either end.
// EPERM and ENOTSUP errors are returned as a non-fatal string list.
func CopyXattrs(src, dst string) []string {
	sz, err := unix.Llistxattr(src, nil)
	if err != nil || sz == 0 {
		return nil
	}
	buf := make([]byte, sz)
	n, err := unix.Llistxattr(src, buf)
	if err != nil || n == 0 {
		return nil
	}

	var warnings []string
	for _, name := range splitNullTerminated(buf[:n]) {
		if !hasPrefix(name, "security.") && !hasPrefix(name, "user.") {
			continue
		}
		vbuf := make([]byte, 65536)
		vn, err := unix.Lgetxattr(src, name, vbuf)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("getxattr %s %s: %v", src, name, err))
			continue
		}
		if err := unix.Lsetxattr(dst, name, vbuf[:vn], 0); err != nil {
			warnings = append(warnings, fmt.Sprintf("setxattr %s %s: %v", dst, name, err))
		}
	}
	return warnings
}

func splitNullTerminated(b []byte) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c == 0 {
			if i > start {
				out = append(out, string(b[start:i]))
			}
			start = i + 1
		}
	}
	return out
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
