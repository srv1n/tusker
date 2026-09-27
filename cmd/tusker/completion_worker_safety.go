package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const completionAuthoritativeRawLogMaxBytes int64 = 16 << 20

func completionExecutableIdentity(path, version string) (string, string, error) {
	path = strings.TrimSpace(path)
	version = strings.TrimSpace(version)
	if path == "" || version == "" {
		return "", "", fmt.Errorf("completion authority requires a versioned preflighted codex executable")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	physical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", "", fmt.Errorf("resolve preflighted codex executable: %w", err)
	}
	if !filepath.IsAbs(physical) {
		return "", "", fmt.Errorf("completion authority requires an absolute codex executable")
	}
	before, err := os.Stat(physical)
	if err != nil {
		return "", "", err
	}
	if !before.Mode().IsRegular() || before.Mode()&0o111 == 0 {
		return "", "", fmt.Errorf("completion authority requires a regular executable codex file")
	}
	file, err := os.Open(physical)
	if err != nil {
		return "", "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	opened, statErr := file.Stat()
	closeErr := file.Close()
	if copyErr != nil {
		return "", "", copyErr
	}
	if statErr != nil {
		return "", "", statErr
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	after, err := os.Stat(physical)
	if err != nil {
		return "", "", err
	}
	if !os.SameFile(before, opened) || !os.SameFile(opened, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", "", fmt.Errorf("preflighted codex executable changed while its identity was captured")
	}
	payload := strings.Join([]string{
		"tusker.completion-executable/v1",
		filepath.Clean(physical),
		before.Mode().String(),
		hex.EncodeToString(hash.Sum(nil)),
		version,
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return filepath.Clean(physical), "sha256:" + hex.EncodeToString(sum[:]), nil
}

func completionVerifyExecutableIdentity(path, expected, searchPath string) error {
	if !v7CloseAuthorityDigest(expected, "sha256:") {
		return fmt.Errorf("completion authority requires a valid codex executable identity")
	}
	if strings.TrimSpace(searchPath) == "" {
		return fmt.Errorf("completion authority requires the captured non-login runner search path")
	}
	version, err := runnerExecutableHealthCheck(path, searchPath)
	if err != nil {
		return err
	}
	physical, actual, err := completionExecutableIdentity(path, version)
	if err != nil {
		return err
	}
	if filepath.Clean(path) != physical || actual != expected {
		return fmt.Errorf("completion authority refuses codex executable path or identity drift")
	}
	return nil
}
