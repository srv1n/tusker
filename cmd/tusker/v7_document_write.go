package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const v7DocumentLockTimeout = 5 * time.Second

type v7DocumentLock struct {
	file *os.File
}

func (lock *v7DocumentLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func acquireV7DocumentLock(filePath string, timeout time.Duration) (*v7DocumentLock, error) {
	identity, err := v7DocumentLockIdentity(filePath)
	if err != nil {
		return nil, err
	}
	lockDir := v7DocumentLockDirectory()
	if err := ensureV7DocumentLockDirectory(lockDir); err != nil {
		return nil, err
	}
	lockName := fmt.Sprintf("%x.lock", sha256.Sum256([]byte(identity)))
	lockPath := filepath.Join(lockDir, lockName)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open V7 document lock: %w", err)
	}
	if err := requireOwnerRegularV7Lock(lockFile, lockPath); err != nil {
		_ = lockFile.Close()
		return nil, err
	}
	if err := lockFile.Chmod(0o600); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("secure V7 document lock: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &v7DocumentLock{file: lockFile}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			_ = lockFile.Close()
			return nil, fmt.Errorf("lock V7 document: %w", err)
		}
		if time.Now().After(deadline) {
			_ = lockFile.Close()
			return nil, tuskerError("CAS_BUSY", "V7 object is busy: "+filepath.Base(filePath), withPath(filePath), withHint("retry the Tusker control operation after the current writer finishes"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func v7DocumentLockDirectory() string {
	return filepath.Join(string(os.PathSeparator), "tmp", fmt.Sprintf("tusker-v7-document-locks-%d", os.Getuid()))
}

func ensureV7DocumentLockDirectory(lockDir string) error {
	if err := os.Mkdir(lockDir, 0o700); err != nil && !os.IsExist(err) {
		return fmt.Errorf("create V7 document lock directory: %w", err)
	}
	info, err := os.Lstat(lockDir)
	if err != nil {
		return fmt.Errorf("inspect V7 document lock directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("V7 document lock path is not a real directory: %s", lockDir)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("V7 document lock directory is not owned by the current user: %s", lockDir)
	}
	if err := os.Chmod(lockDir, 0o700); err != nil {
		return fmt.Errorf("secure V7 document lock directory: %w", err)
	}
	return nil
}

func v7DocumentLockIdentity(filePath string) (string, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("V7 document is not a regular file: %s", filePath)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("V7 document filesystem identity is unavailable: %s", filePath)
	}
	if stat.Nlink > 1 {
		return "", fmt.Errorf("refusing CAS mutation of hard-linked V7 document: %s", filePath)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absPath))
	if err != nil {
		return "", err
	}
	identity := filepath.Join(parent, filepath.Base(absPath))
	if runtime.GOOS == "darwin" {
		identity = strings.ToLower(identity)
	}
	return identity, nil
}

func requireOwnerRegularV7Lock(file *os.File, path string) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect V7 document lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("V7 document lock is not a regular file: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("V7 document lock is not owned by the current user: %s", path)
	}
	return nil
}

type v7AtomicWriteOps struct {
	createTemp    func(string, string) (*os.File, error)
	writeFile     func(*os.File, string) (int, error)
	syncFile      func(*os.File) error
	closeFile     func(*os.File) error
	rename        func(string, string) error
	syncDirectory func(string) error
}

func defaultV7AtomicWriteOps() v7AtomicWriteOps {
	return v7AtomicWriteOps{
		createTemp:    os.CreateTemp,
		writeFile:     func(file *os.File, content string) (int, error) { return file.WriteString(content) },
		syncFile:      func(file *os.File) error { return file.Sync() },
		closeFile:     func(file *os.File) error { return file.Close() },
		rename:        os.Rename,
		syncDirectory: syncV7DocumentDirectory,
	}
}

func syncV7DocumentDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	defer func() { _ = directory.Close() }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

func atomicReplaceV7Document(filePath, content string) error {
	return atomicReplaceV7DocumentWithOps(filePath, content, defaultV7AtomicWriteOps())
}

func atomicReplaceV7DocumentWithOps(filePath, content string, ops v7AtomicWriteOps) error {
	info, err := os.Lstat(filePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to atomically replace symlinked V7 document: %s", filePath)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to atomically replace non-regular V7 document: %s", filePath)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
		return fmt.Errorf("refusing to atomically replace hard-linked V7 document: %s", filePath)
	}

	temp, err := ops.createTemp(filepath.Dir(filePath), "."+filepath.Base(filePath)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	renamed := false
	defer func() {
		_ = temp.Close()
		if !renamed {
			_ = os.Remove(tempPath)
		}
	}()

	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if written, err := ops.writeFile(temp, content); err != nil {
		return err
	} else if written != len(content) {
		return fmt.Errorf("write V7 document temporary file: wrote %d of %d bytes", written, len(content))
	}
	if err := ops.syncFile(temp); err != nil {
		return err
	}
	if err := ops.closeFile(temp); err != nil {
		return err
	}
	if err := ops.rename(tempPath, filePath); err != nil {
		return err
	}
	renamed = true
	if err := ops.syncDirectory(filepath.Dir(filePath)); err != nil {
		return fmt.Errorf("sync V7 document parent directory after rename: %w", err)
	}
	return nil
}
