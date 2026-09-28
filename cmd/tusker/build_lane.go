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
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

var localCompile = regexp.MustCompile(`^\s*Compiling \S+ v\S+ \(/`)

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

func ensureBuildLaneShims(stateRoot string) (string, error) {
	dir := filepath.Join(stateRoot, "build-lane", "bin")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	for _, tool := range []string{"cargo", "xcodebuild", "swift"} {
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
	root := DefaultStateRoot()
	dir, err := ensureBuildLaneShims(root)
	if err != nil {
		return env
	}
	env = setEnvValue(env, "PATH", dir+string(os.PathListSeparator)+runnerEnvValue(env, "PATH"))
	return setEnvValue(env, "TUSKER_BUILD_LANE_STATE_ROOT", root)
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
	if len(args) == 0 {
		return false
	}
	if tool == "swift" {
		switch args[0] {
		case "build", "test", "run":
			return true
		}
		return false
	}
	switch args[0] {
	case "build", "b", "check", "c", "test", "t", "clippy", "run", "r", "bench", "doc", "rustc", "fix":
		return true
	}
	return false
}

func buildLaneGuard(tool string, args []string) bool {
	if os.Getenv("TUSKER_SHARED_CHECKOUT") != "1" || tool != "cargo" || len(args) == 0 {
		return false
	}
	switch args[0] {
	case "fix":
		return true
	case "fmt":
		for i, a := range args {
			if a == "--" {
				for _, file := range args[i+1:] {
					if !strings.HasPrefix(file, "-") {
						return false
					}
				}
			}
		}
		return true
	case "clippy":
		for _, a := range args[1:] {
			if a == "--fix" {
				return true
			}
		}
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

type buildLaneStderr struct {
	deps, local int
	pending     string
}

func (w *buildLaneStderr) Write(p []byte) (int, error) {
	n, err := os.Stderr.Write(p)
	w.pending += string(p)
	for {
		i := strings.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSuffix(w.pending[:i], "\r")
		w.pending = w.pending[i+1:]
		if localCompile.MatchString(line) {
			w.local++
		} else if strings.HasPrefix(strings.TrimSpace(line), "Compiling ") {
			w.deps++
		}
	}
	return n, err
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
			if len(lines) > 2000 {
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

func runBuildLane(tool string, args []string) int {
	if buildLaneGuard(tool, args) {
		fmt.Fprintln(os.Stderr, "tusker: other agents share this checkout; format or fix only your files, e.g. rustfmt <file>...")
		return 2
	}
	root := os.Getenv("TUSKER_BUILD_LANE_STATE_ROOT")
	if root == "" {
		root = DefaultStateRoot()
	}
	shimDir := filepath.Join(root, "build-lane", "bin")
	realTool, err := buildLaneTool(tool, shimDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tusker:", err)
		return 1
	}
	heavy := buildLaneHeavy(tool, args) && os.Getenv("TUSKER_BUILD_LANE_HELD") != "1"
	var slot *os.File
	var waitMS int64
	if heavy {
		_, slots := buildLaneSettings()
		slot, waitMS, err = buildLaneSlot(root, slots)
		if err != nil {
			fmt.Fprintln(os.Stderr, "tusker:", err)
			return 1
		}
		defer slot.Close()
	}
	cold := false
	if heavy && tool == "cargo" {
		target := os.Getenv("CARGO_TARGET_DIR")
		if target == "" {
			if top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
				target = filepath.Join(strings.TrimSpace(string(top)), "target")
			}
		}
		if target != "" {
			_, err := os.Stat(target)
			cold = os.IsNotExist(err)
		}
	}
	cmd := exec.Command(realTool, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	stderr := &buildLaneStderr{}
	if heavy && tool == "cargo" {
		cmd.Stderr = stderr
	} else {
		cmd.Stderr = os.Stderr
	}
	env := os.Environ()
	if heavy {
		env = setEnvValue(env, "TUSKER_BUILD_LANE_HELD", "1")
	}
	if tool == "cargo" && os.Getenv("CARGO_BUILD_JOBS") == "" {
		jobs := runtime.NumCPU() - 2
		if jobs < 1 {
			jobs = 1
		}
		env = setEnvValue(env, "CARGO_BUILD_JOBS", fmt.Sprint(jobs))
	}
	cmd.Env = env
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
		record := map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "project_id": os.Getenv("TUSKER_PROJECT_ID"), "task_id": os.Getenv("TUSKER_ITEM_ID"), "checkout": checkout, "tool": tool, "cmd": summary, "flags_digest": digest, "cold": cold, "wait_ms": waitMS, "build_ms": buildMS, "compiled_local": stderr.local, "compiled_deps": stderr.deps, "exit": code}
		if e := appendBuildLaneLog(root, record); e != nil {
			fmt.Fprintln(os.Stderr, "tusker: build lane log:", e)
		}
	}
	return code
}
