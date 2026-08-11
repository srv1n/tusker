//go:build linux

package main

import "golang.org/x/sys/unix"

func publishACPAdapterBundleExclusive(stage, final string) error {
	return unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE)
}
