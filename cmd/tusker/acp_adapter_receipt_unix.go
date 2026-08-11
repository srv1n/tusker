//go:build darwin || linux

package main

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const acpAdapterInstallMaxReceiptBytes = int64(1 << 20)

// readACPAdapterInstallReceiptBytes opens the exact path without following a
// final-component symlink, bounds bytes, and proves its descriptor remains the
// object named by the path after reading.
func readACPAdapterInstallReceiptBytes(path string) ([]byte, bool, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err == unix.ENOENT {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if err := validateACPAdapterBundleFileInfo(info, path, false); err != nil || info.Mode().Perm() != 0o400 || info.Size() < 0 || info.Size() > acpAdapterInstallMaxReceiptBytes {
		if err == nil {
			err = fmt.Errorf("ACP adapter receipt has an unsafe mode or size")
		}
		return nil, false, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, acpAdapterInstallMaxReceiptBytes+1))
	if err != nil || int64(len(raw)) > acpAdapterInstallMaxReceiptBytes {
		if err == nil {
			err = fmt.Errorf("ACP adapter receipt exceeds byte limit")
		}
		return nil, false, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return nil, false, fmt.Errorf("ACP adapter receipt changed while reading")
	}
	if err := validateACPAdapterBundleFileInfo(after, path, false); err != nil || after.Mode().Perm() != 0o400 || after.Size() < 0 || after.Size() > acpAdapterInstallMaxReceiptBytes {
		if err == nil {
			err = fmt.Errorf("ACP adapter receipt mode, link, or size changed while reading")
		}
		return nil, false, err
	}
	pathInfo, err := os.Lstat(path)
	if err != nil || !os.SameFile(after, pathInfo) {
		return nil, false, fmt.Errorf("ACP adapter receipt path changed while reading")
	}
	if err := validateACPAdapterBundleFileInfo(pathInfo, path, false); err != nil || pathInfo.Mode().Perm() != 0o400 || pathInfo.Size() != after.Size() || !pathInfo.ModTime().Equal(after.ModTime()) {
		if err == nil {
			err = fmt.Errorf("ACP adapter receipt path metadata changed while reading")
		}
		return nil, false, err
	}
	return raw, true, nil
}
