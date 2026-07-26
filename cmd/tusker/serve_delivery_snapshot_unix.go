//go:build darwin || linux

package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type serveDeliveryPlanComponent struct {
	Name string
	Dev  uint64
	Ino  uint64
	Mode uint32
}

type serveDeliveryPlanUnixHandle struct {
	directories []int
	components  []serveDeliveryPlanComponent
	file        *os.File
	size        int64
	modTime     time.Time
	digest      [sha256.Size]byte
}

func (handle *serveDeliveryPlanUnixHandle) Close() {
	if handle == nil {
		return
	}
	if handle.file != nil {
		_ = handle.file.Close()
		handle.file = nil
	}
	for index := len(handle.directories) - 1; index >= 0; index-- {
		_ = unix.Close(handle.directories[index])
	}
	handle.directories = nil
}

func (handle *serveDeliveryPlanUnixHandle) Verify() error {
	if handle == nil || handle.file == nil || len(handle.components) == 0 || len(handle.directories) != len(handle.components) {
		return tuskerError(errorInvalidTransition, "delivery plan snapshot is no longer available")
	}
	for index, expected := range handle.components {
		var current unix.Stat_t
		if err := unix.Fstatat(handle.directories[index], expected.Name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return tuskerError(errorInvalidTransition, "delivery plan path identity changed after review")
		}
		if uint64(current.Dev) != expected.Dev || uint64(current.Ino) != expected.Ino ||
			uint32(current.Mode)&unix.S_IFMT != expected.Mode&unix.S_IFMT {
			return tuskerError(errorInvalidTransition, "delivery plan path identity changed after review")
		}
	}
	current, err := handle.file.Stat()
	if err != nil || current.Size() != handle.size || !current.ModTime().Equal(handle.modTime) {
		return tuskerError(errorInvalidTransition, "delivery plan contents changed after its snapshot was read")
	}
	raw, err := io.ReadAll(io.NewSectionReader(handle.file, 0, current.Size()))
	if err != nil || sha256.Sum256(raw) != handle.digest {
		return tuskerError(errorInvalidTransition, "delivery plan fingerprint changed after its snapshot was read")
	}
	after, err := handle.file.Stat()
	if err != nil || after.Size() != handle.size || !after.ModTime().Equal(handle.modTime) {
		return tuskerError(errorInvalidTransition, "delivery plan contents changed while its fingerprint was being verified")
	}
	return nil
}

// serveDeliveryPlanSnapshotAt accepts only an existing repo-relative regular
// file. Every caller-controlled component is opened relative to the already
// opened repository descriptor with O_NOFOLLOW; no pathname is reopened to
// obtain plan bytes.
func serveDeliveryPlanSnapshotAt(project RegisteredProject, raw string, nestedRoots []string) (*serveDeliveryPlanSnapshot, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return nil, tuskerError(errorMissingArg, "delivery review requires a repo-relative plan path")
	}
	if filepath.IsAbs(path) {
		return nil, tuskerError(errorInvalidArg, "delivery plan path must be relative to the repository", withPath(path))
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, tuskerError(errorInvalidArg, "delivery plan path must stay inside the repository", withPath(path))
	}
	for _, nested := range nestedRoots {
		nested = filepath.Clean(nested)
		if clean == nested || strings.HasPrefix(clean, nested+string(filepath.Separator)) {
			return nil, tuskerError(errorInvalidArg, "delivery plan belongs to a different registered nested project", withPath(path))
		}
	}
	repoRoot, err := filepath.EvalSymlinks(project.RepoRoot)
	if err != nil {
		return nil, tuskerError(errorInvalidArg, "cannot resolve repository root for delivery plan", withPath(project.RepoRoot))
	}
	rootFD, err := unix.Open(repoRoot, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, tuskerError(errorInvalidArg, "cannot open authorized repository root for delivery plan", withPath(project.RepoRoot))
	}
	handle := &serveDeliveryPlanUnixHandle{
		directories: []int{rootFD},
		components:  []serveDeliveryPlanComponent{},
	}
	snapshot := &serveDeliveryPlanSnapshot{
		Path:     filepath.Join(repoRoot, clean),
		Relative: clean,
		handle:   handle,
	}
	fail := func(openErr error) (*serveDeliveryPlanSnapshot, error) {
		snapshot.Close()
		if errors.Is(openErr, unix.ENOENT) {
			return nil, tuskerError(errorNotFound, "delivery plan does not exist", withPath(clean))
		}
		return nil, tuskerError(errorInvalidArg, "delivery plan path must contain only real directories and a non-symlink regular file", withPath(clean))
	}
	var rootStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil {
		return fail(err)
	}
	parts := strings.Split(clean, string(filepath.Separator))
	for index, part := range parts {
		if serveDeliveryPlanOpenComponentHook != nil {
			serveDeliveryPlanOpenComponentHook(clean, index)
		}
		final := index == len(parts)-1
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if final {
			flags |= unix.O_NONBLOCK
		} else {
			flags |= unix.O_DIRECTORY
		}
		fd, openErr := unix.Openat(handle.directories[index], part, flags, 0)
		if openErr != nil {
			return fail(openErr)
		}
		var stat unix.Stat_t
		if statErr := unix.Fstat(fd, &stat); statErr != nil {
			_ = unix.Close(fd)
			return fail(statErr)
		}
		mode := uint32(stat.Mode)
		wantMode := uint32(unix.S_IFDIR)
		if final {
			wantMode = unix.S_IFREG
		}
		if mode&unix.S_IFMT != wantMode {
			_ = unix.Close(fd)
			return fail(unix.EINVAL)
		}
		handle.components = append(handle.components, serveDeliveryPlanComponent{Name: part, Dev: uint64(stat.Dev), Ino: uint64(stat.Ino), Mode: mode})
		if final {
			handle.file = os.NewFile(uintptr(fd), snapshot.Path)
		} else {
			handle.directories = append(handle.directories, fd)
		}
	}
	before, err := handle.file.Stat()
	if err != nil {
		return fail(err)
	}
	snapshot.Raw, err = io.ReadAll(handle.file)
	if err != nil {
		return fail(err)
	}
	after, err := handle.file.Stat()
	if err != nil {
		return fail(err)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		snapshot.Close()
		return nil, tuskerError(errorInvalidTransition, "delivery plan changed while its snapshot was being read; review it again", withPath(clean))
	}
	handle.size = after.Size()
	handle.modTime = after.ModTime()
	handle.digest = sha256.Sum256(snapshot.Raw)
	identity := sha256.New()
	_, _ = fmt.Fprintf(identity, "root:%d:%d\npath:%x\n", uint64(rootStat.Dev), uint64(rootStat.Ino), []byte(filepath.ToSlash(clean)))
	for _, component := range handle.components {
		_, _ = fmt.Fprintf(identity, "component:%x:%d:%d:%d\n", []byte(component.Name), component.Dev, component.Ino, component.Mode&unix.S_IFMT)
	}
	snapshot.Identity = fmt.Sprintf("sha256:%x", identity.Sum(nil))
	if err := snapshot.Verify(); err != nil {
		snapshot.Close()
		return nil, tuskerError(errorInvalidTransition, "delivery plan path changed while its snapshot was being opened; review it again", withPath(clean), withContext(map[string]any{"cause": err.Error()}))
	}
	return snapshot, nil
}
