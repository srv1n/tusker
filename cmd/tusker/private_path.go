package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// openPrivatePathParent resolves a file parent one directory descriptor at a
// time. It never follows a user-controlled symlink component and returns an FD
// that remains authoritative if a pathname is concurrently renamed.
func openPrivatePathParent(path string, create bool) (int, string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return -1, "", err
	}
	base := filepath.Base(abs)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return -1, "", fmt.Errorf("private file path has no basename: %q", path)
	}
	parent := physicalSystemPath(filepath.Dir(abs))
	if !filepath.IsAbs(parent) {
		return -1, "", fmt.Errorf("private file parent is not absolute: %q", parent)
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, "", err
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(parent), string(filepath.Separator)), string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		var st unix.Stat_t
		statErr := unix.Fstatat(fd, part, &st, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(statErr, unix.ENOENT) && create {
			statErr = unix.Mkdirat(fd, part, 0o700)
			if statErr == nil {
				statErr = unix.Fstatat(fd, part, &st, unix.AT_SYMLINK_NOFOLLOW)
			}
		}
		if statErr != nil {
			unix.Close(fd)
			return -1, "", fmt.Errorf("inspect private path component %q: %w", part, statErr)
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR {
			unix.Close(fd)
			return -1, "", fmt.Errorf("private path component is not a directory: %s", part)
		}
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if openErr != nil {
			return -1, "", fmt.Errorf("open private path component %q: %w", part, openErr)
		}
		fd = next
	}
	var parentStat unix.Stat_t
	if err := unix.Fstat(fd, &parentStat); err != nil {
		unix.Close(fd)
		return -1, "", err
	}
	if parentStat.Uid != uint32(os.Getuid()) || parentStat.Mode&0o022 != 0 {
		unix.Close(fd)
		return -1, "", fmt.Errorf("private file parent is not owner-controlled: %s", parent)
	}
	return fd, base, nil
}

// macOS exposes /tmp and /var as root-owned compatibility symlinks. Normalize
// only those operating-system aliases; arbitrary user symlinks remain refused.
func physicalSystemPath(path string) string {
	if runtime.GOOS != "darwin" {
		return path
	}
	for from, to := range map[string]string{"/tmp": "/private/tmp", "/var": "/private/var", "/etc": "/private/etc"} {
		if path == from {
			return to
		}
		if strings.HasPrefix(path, from+string(filepath.Separator)) {
			return to + strings.TrimPrefix(path, from)
		}
	}
	return path
}

func privateTempFileAt(parentFD int, base string) (*os.File, string, error) {
	for attempt := 0; attempt < 32; attempt++ {
		name := fmt.Sprintf(".%s.tmp-%d-%d-%d", base, os.Getpid(), time.Now().UnixNano(), attempt)
		fd, err := unix.Openat(parentFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return os.NewFile(uintptr(fd), name), name, nil
	}
	return nil, "", fmt.Errorf("unable to allocate private temp file for %s", base)
}

func validatePrivatePathEntryAt(parentFD int, base, display string) error {
	fd, err := unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), display)
	defer file.Close()
	return requirePrivateRunnerFile(file, display)
}

func writePrivateFileReplace(path string, raw []byte) (returnErr error) {
	parentFD, base, err := openPrivatePathParent(path, true)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	var existing unix.Stat_t
	if err := unix.Fstatat(parentFD, base, &existing, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if err := validatePrivatePathEntryAt(parentFD, base, path); err != nil {
			return err
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}
	temp, tempName, err := privateTempFileAt(parentFD, base)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = temp.Close()
		}
		_ = unix.Unlinkat(parentFD, tempName, 0)
	}()
	if _, err := temp.Write(raw); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	closed = true
	if err := unix.Renameat(parentFD, tempName, parentFD, base); err != nil {
		return err
	}
	if err := validatePrivatePathEntryAt(parentFD, base, path); err != nil {
		return err
	}
	return unix.Fsync(parentFD)
}

func readPrivateFile(path string, maxBytes int64) ([]byte, error) {
	parentFD, base, err := openPrivatePathParent(path, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parentFD)
	fd, err := unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	if err := requirePrivateRunnerFile(file, path); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("private file exceeds %d bytes: %s", maxBytes, path)
	}
	return raw, nil
}

func removePrivateFile(path string) error {
	parentFD, base, err := openPrivatePathParent(path, false)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	if err := validatePrivatePathEntryAt(parentFD, base, path); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, base, 0); err != nil {
		return err
	}
	return unix.Fsync(parentFD)
}

// removePrivateFileIfExists is the descriptor-relative equivalent of rm -f
// for attempt-scoped private files. It creates only the owner-controlled parent
// path, refuses symlinks/hardlinks/non-private incumbents, and never follows a
// pathname component during validation or removal.
func removePrivateFileIfExists(path string) error {
	parentFD, base, err := openPrivatePathParent(path, true)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	var existing unix.Stat_t
	if err := unix.Fstatat(parentFD, base, &existing, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil {
		return err
	}
	if err := validatePrivatePathEntryAt(parentFD, base, path); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, base, 0); err != nil {
		return err
	}
	return unix.Fsync(parentFD)
}
