package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

func buildLaneSettings() (bool, int) {
	cfg, _, present, err := readTuskerConfigLayer(userGlobalTuskerConfigPath())
	if err != nil || !present {
		return true, 1
	}
	c := cfg.Automation.Concurrency
	slots := c.BuildSlots
	if slots == 0 {
		slots = 1
	}
	if c.BuildLane != nil && !*c.BuildLane {
		return false, slots
	}
	return true, slots
}

// buildLaneRustcWrapper is the argv0 cargo runs through RUSTC_WRAPPER. The
// variable survives login shells, which rebuild PATH ahead of any PATH shim.
const buildLaneRustcWrapper = "tusker-rustc"

func ensureBuildLaneShims(stateRoot string) (string, error) {
	dir := filepath.Join(stateRoot, "build-lane", "bin")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// A cargo PATH shim lost to login shells that rebuild PATH; RUSTC_WRAPPER
	// replaces it, so drop links left by older installs.
	_ = os.Remove(filepath.Join(dir, "cargo"))
	for _, tool := range []string{"xcodebuild", "swift", buildLaneRustcWrapper} {
		link := filepath.Join(dir, tool)
		target, err := os.Readlink(link)
		if err == nil && target == exe {
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		f, err := os.CreateTemp(dir, tool+".*.tmp")
		if err != nil {
			return "", err
		}
		tmp := f.Name()
		_ = f.Close()
		_ = os.Remove(tmp)
		if err = os.Symlink(exe, tmp); err != nil {
			return "", err
		}
		if err = os.Rename(tmp, link); err != nil {
			_ = os.Remove(tmp)
			return "", err
		}
	}
	return dir, nil
}

func buildLaneWorkerEnv(env []string) []string {
	enabled, _ := buildLaneSettings()
	if !enabled {
		return env
	}
	dir, err := ensureBuildLaneShims(DefaultStateRoot())
	if err != nil {
		return env
	}
	env = setEnvValue(env, "PATH", dir+string(os.PathListSeparator)+runnerEnvValue(env, "PATH"))
	wrapper := filepath.Join(dir, buildLaneRustcWrapper)
	if prev := runnerEnvValue(env, "RUSTC_WRAPPER"); prev != "" && prev != wrapper {
		env = setEnvValue(env, "TUSKER_RUSTC_WRAPPER_NEXT", prev)
	}
	env = setEnvValue(env, "RUSTC_WRAPPER", wrapper)
	if runnerEnvValue(env, "CARGO_BUILD_JOBS") == "" {
		env = setEnvValue(env, "CARGO_BUILD_JOBS", fmt.Sprint(max(1, runtime.NumCPU()-2)))
	}
	if root, err := buildLaneDataRoot(); err == nil {
		env = setEnvValue(env, "TUSKER_BUILD_LANE_STATE_ROOT", root)
	}
	return env
}

// buildLaneDataRoot holds the slot locks and build log. Sandboxed workers
// (Codex workspace-write) may write only their checkout and /tmp, so the data
// lives in a private per-user /tmp folder; the shims stay in the state root,
// which the sandbox only needs to read.
func buildLaneDataRoot() (string, error) {
	if root := os.Getenv("TUSKER_BUILD_LANE_STATE_ROOT"); root != "" {
		return root, nil
	}
	root := filepath.Join("/tmp", fmt.Sprintf("tusker-%d", os.Getuid()))
	if err := os.Mkdir(root, 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || int(stat.Uid) != os.Getuid() || info.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("%s is not a private directory owned by this user", root)
	}
	return root, nil
}

func buildLaneTool(tool, shimDir string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	realExe, _ := filepath.EvalSymlinks(exe)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		if same, _ := filepath.Abs(dir); same == shimDir {
			continue
		}
		candidate := filepath.Join(dir, tool)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
			continue
		}
		realCandidate, err := filepath.EvalSymlinks(candidate)
		if err != nil || realCandidate == realExe {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("real %s not found on PATH", tool)
}

func buildLaneHeavy(tool string, args []string) bool {
	if tool == "xcodebuild" {
		for _, a := range args {
			switch a {
			case "-version", "-list", "-showBuildSettings", "-showsdks":
				return false
			}
		}
		return true
	}
	if tool != "swift" || len(args) == 0 {
		return false
	}
	switch args[0] {
	case "build", "test", "run":
		return true
	}
	return false
}

func buildLaneSlot(root string, slots int) (*os.File, int64, error) {
	start := time.Now()
	dir := filepath.Join(root, "build-lane")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, 0, err
	}
	if slots < 1 {
		slots = 1
	}
	for i := 0; i < slots; i++ {
		f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("slot-%d.lock", i)), os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return nil, 0, err
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, time.Since(start).Milliseconds(), nil
		}
		f.Close()
		if err != syscall.EWOULDBLOCK {
			return nil, 0, err
		}
	}
	f, err := os.OpenFile(filepath.Join(dir, "slot-0.lock"), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, 0, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, time.Since(start).Milliseconds(), nil
}

func buildLaneSummary(tool string, args []string) (string, string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	flags := []string{}
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "-") && !strings.Contains(a, "/") && !strings.Contains(a, "\\") {
			flags = append(flags, a)
		}
	}
	sort.Strings(flags)
	display := strings.Join(append([]string{tool, sub}, flags...), " ")
	if len(display) > 120 {
		display = display[:120]
	}
	digestParts := append([]string{sub}, flags...)
	for _, key := range []string{"RUSTFLAGS", "CARGO_BUILD_TARGET", "CARGO_ENCODED_RUSTFLAGS"} {
		digestParts = append(digestParts, key+"="+os.Getenv(key))
	}
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "CARGO_PROFILE_") {
			digestParts = append(digestParts, e)
		}
	}
	sort.Strings(digestParts[4+len(flags):])
	sum := sha256.Sum256([]byte(strings.Join(digestParts, "\x00")))
	return display, hex.EncodeToString(sum[:])[:16]
}

func appendBuildLaneLog(root string, record map[string]any) error {
	dir := filepath.Join(root, "build-lane")
	guard, err := os.OpenFile(filepath.Join(dir, "log.lock"), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer guard.Close()
	if err = syscall.Flock(int(guard.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(guard.Fd()), syscall.LOCK_UN)
	path := filepath.Join(dir, "builds.jsonl")
	if info, err := os.Stat(path); err == nil && info.Size() > 2*1024*1024 {
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 4096), 4*1024*1024)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
			if len(lines) > 4000 {
				lines = lines[1:]
			}
		}
		scanErr := scanner.Err()
		in.Close()
		if scanErr != nil {
			return scanErr
		}
		tmp := path + ".tmp"
		if err = os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
			return err
		}
		if err = os.Rename(tmp, path); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// runBuildLane fronts xcodebuild and swift, which have no environment hook.
// Cargo goes through runRustcWrapper instead.
func runBuildLane(tool string, args []string) int {
	root, rootErr := buildLaneDataRoot()
	shimDir := filepath.Join(DefaultStateRoot(), "build-lane", "bin")
	realTool, err := buildLaneTool(tool, shimDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tusker:", err)
		return 1
	}
	heavy := buildLaneHeavy(tool, args) && os.Getenv("TUSKER_BUILD_LANE_HELD") != "1"
	if heavy && rootErr != nil {
		fmt.Fprintln(os.Stderr, "tusker: build lane unavailable, building without the queue:", rootErr)
		heavy = false
	}
	var waitMS int64
	if heavy {
		_, slots := buildLaneSettings()
		slot, wait, err := buildLaneSlot(root, slots)
		if err != nil {
			// A sandboxed worker may be unable to open the lock. Build anyway:
			// an unqueued build is slower for the machine, a failed one is wrong.
			fmt.Fprintln(os.Stderr, "tusker: build lane unavailable, building without the queue:", err)
			heavy = false
		} else {
			defer slot.Close()
			waitMS = wait
		}
	}
	cmd := exec.Command(realTool, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	if heavy {
		cmd.Env = setEnvValue(cmd.Env, "TUSKER_BUILD_LANE_HELD", "1")
	}
	start := time.Now()
	err = cmd.Run()
	buildMS := time.Since(start).Milliseconds()
	code := 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			fmt.Fprintln(os.Stderr, "tusker:", err)
			code = 1
		}
	}
	if heavy {
		checkout := ""
		if top, e := exec.Command("git", "rev-parse", "--show-toplevel").Output(); e == nil {
			checkout = strings.TrimSpace(string(top))
		}
		summary, digest := buildLaneSummary(tool, args)
		record := map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "project_id": os.Getenv("TUSKER_PROJECT_ID"), "task_id": os.Getenv("TUSKER_ITEM_ID"), "checkout": checkout, "tool": tool, "cmd": summary, "flags_digest": digest, "wait_ms": waitMS, "build_ms": buildMS, "exit": code}
		if e := appendBuildLaneLog(root, record); e != nil {
			fmt.Fprintln(os.Stderr, "tusker: build lane log:", e)
		}
	}
	return code
}

// rustcHeld keeps the slot lock reachable until exec replaces the process.
var rustcHeld *os.File

// runRustcWrapper runs as cargo's RUSTC_WRAPPER; args are the rustc path and
// its arguments. A compile waits for one of NumCPU machine-wide slots, logs
// one line, then execs rustc. The slot lock stays open across exec and frees
// when rustc exits, and exec keeps cargo's jobserver descriptors intact.
func runRustcWrapper(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "tusker-rustc: expected a rustc path")
		return 1
	}
	if next := os.Getenv("TUSKER_RUSTC_WRAPPER_NEXT"); next != "" {
		args = append([]string{next}, args...)
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "tusker-rustc:", err)
		return 1
	}
	if rustcCompiles(args) {
		start := time.Now()
		root, err := buildLaneDataRoot()
		var slot *os.File
		if err == nil {
			slot, err = rustcSlot(root, runtime.NumCPU())
		}
		if err == nil {
			_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, slot.Fd(), syscall.F_SETFD, 0)
			if errno != 0 {
				err = errno
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "tusker-rustc: build lane unavailable, compiling without the queue:", err)
		} else {
			rustcHeld = slot
			if e := appendBuildLaneLog(root, rustcRecord(args, time.Since(start).Milliseconds())); e != nil {
				fmt.Fprintln(os.Stderr, "tusker-rustc: build lane log:", e)
			}
		}
	}
	err = syscall.Exec(path, args, os.Environ())
	fmt.Fprintln(os.Stderr, "tusker-rustc:", err)
	return 1
}

// rustcCompiles skips cargo's probes (rustc -vV, --print) that name no source.
func rustcCompiles(args []string) bool {
	for _, a := range args[1:] {
		if strings.HasSuffix(a, ".rs") {
			return true
		}
	}
	return false
}

func rustcSlot(root string, slots int) (*os.File, error) {
	dir := filepath.Join(root, "build-lane")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	files := make([]*os.File, 0, slots)
	defer func() {
		for _, f := range files {
			if f != nil {
				f.Close()
			}
		}
	}()
	for i := 0; i < max(1, slots); i++ {
		f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("rustc-%d.lock", i)), os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	for {
		for i, f := range files {
			err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				files[i] = nil
				return f, nil
			}
			if err != syscall.EWOULDBLOCK {
				return nil, err
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func rustcRecord(args []string, waitMS int64) map[string]any {
	flag := func(name string) string {
		for i, a := range args {
			if a == name && i+1 < len(args) {
				return args[i+1]
			}
			if strings.HasPrefix(a, name+"=") {
				return strings.TrimPrefix(a, name+"=")
			}
		}
		return ""
	}
	// --out-dir is <target>/<profile>/deps, or <target>/<profile>/build/<unit>
	// for build scripts; the target directory identifies the build cache.
	target := flag("--out-dir")
	if filepath.Base(target) == "deps" {
		target = filepath.Dir(filepath.Dir(target))
	} else if filepath.Base(filepath.Dir(target)) == "build" {
		target = filepath.Dir(filepath.Dir(filepath.Dir(target)))
	}
	cargoHome := os.Getenv("CARGO_HOME")
	if cargoHome == "" {
		home, _ := os.UserHomeDir()
		cargoHome = filepath.Join(home, ".cargo")
	}
	manifest := os.Getenv("CARGO_MANIFEST_DIR")
	local := !strings.HasPrefix(manifest, filepath.Join(cargoHome, "registry")+string(os.PathSeparator)) && !strings.HasPrefix(manifest, filepath.Join(cargoHome, "git")+string(os.PathSeparator))
	return map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "project_id": os.Getenv("TUSKER_PROJECT_ID"), "task_id": os.Getenv("TUSKER_ITEM_ID"), "tool": "rustc", "target": target, "cargo_pid": os.Getppid(), "crate": firstNonEmpty(os.Getenv("CARGO_CRATE_NAME"), flag("--crate-name")), "local": local, "wait_ms": waitMS}
}
