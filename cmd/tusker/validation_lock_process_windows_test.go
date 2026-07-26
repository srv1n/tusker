//go:build windows

package main

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

const validationWindowsProcessStillActive = 259

func validationTestProcessAlivePlatform(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false
		}
		// Access denial proves that a process currently owns the PID. Unknown
		// probe failures fail closed rather than stealing a possibly live lock.
		return true
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return true
	}
	return exitCode == validationWindowsProcessStillActive
}

func TestValidationSuiteLockWindowsProcessLiveness(t *testing.T) {
	if !validationTestProcessAlive(os.Getpid()) {
		t.Fatal("current Windows process reported dead")
	}
	if validationTestProcessAlive(99999999) {
		t.Fatal("nonexistent Windows process reported alive")
	}
}
