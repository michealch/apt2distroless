// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

//go:build linux || darwin

package copier

import (
	"os"
	"syscall"
)

func fileUID(fi os.FileInfo) int {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(stat.Uid)
	}
	return 0
}

func fileGID(fi os.FileInfo) int {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(stat.Gid)
	}
	return 0
}
