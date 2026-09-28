package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func validTaskWeight(raw any) bool {
	weight, ok := raw.(int)
	return ok && weight >= 1 && weight <= 8
}

func taskWeightArg(args Args) (int, bool, error) {
	raw, set := args["weight"]
	if !set {
		return 0, false, nil
	}
	weight, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || weight < 1 || weight > 8 {
		return 0, false, tuskerError(errorInvalidArg, "--weight must be an integer from 1 to 8")
	}
	return weight, true, nil
}

// taskAdmissionWeight is pure; heavyDirs are repository-relative directories
// containing a Rust, Swift package, or Xcode project marker.
func taskAdmissionWeight(note Note, heavyDirs []string) int {
	if weight := intField(note.Data, "weight"); weight >= 1 && weight <= 8 {
		return weight
	}
	for _, owned := range normalizeList(note.Data["owned_paths"]) {
		owned = filepath.ToSlash(filepath.Clean(owned))
		if strings.HasSuffix(owned, ".rs") || strings.HasSuffix(owned, ".swift") {
			return 4
		}
		for _, dir := range heavyDirs {
			if dir == "." || owned == "." || owned == dir || strings.HasPrefix(owned, dir+"/") || strings.HasPrefix(dir, owned+"/") {
				return 4
			}
		}
	}
	return 1
}

func heavyProjectDirs(root string) []string {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && path != root {
			// ponytail: depth 3 and a fixed skip list keep the walk cheap per poll; cache per repo HEAD if deep monorepos need it.
			name := entry.Name()
			if strings.HasPrefix(name, ".") || name == "target" || name == "node_modules" || name == "build" || name == "DerivedData" || name == "vendor" {
				return filepath.SkipDir
			}
			if rel, _ := filepath.Rel(root, path); strings.Count(filepath.ToSlash(rel), "/") >= 3 {
				return filepath.SkipDir
			}
		}
		if entry.IsDir() && (strings.HasSuffix(entry.Name(), ".xcodeproj") || strings.HasSuffix(entry.Name(), ".xcworkspace")) {
			if strings.HasSuffix(entry.Name(), ".xcodeproj") || strings.HasSuffix(entry.Name(), ".xcworkspace") {
				rel, _ := filepath.Rel(root, filepath.Dir(path))
				dirs = append(dirs, filepath.ToSlash(rel))
			}
			return filepath.SkipDir
		}
		if !entry.IsDir() && (entry.Name() == "Cargo.toml" || entry.Name() == "Package.swift") {
			rel, _ := filepath.Rel(root, filepath.Dir(path))
			dirs = append(dirs, filepath.ToSlash(rel))
		}
		return nil
	})
	return dirs
}

func weightAdmissionReason(used, active, budget, next int) string {
	if budget > 0 && active > 0 && used+next > budget {
		return fmt.Sprintf("weight budget %d used of %d", used, budget)
	}
	return ""
}

func machineLoadPerCPU() (float64, error) {
	var raw []byte
	var err error
	if runtime.GOOS == "darwin" {
		raw, err = exec.Command("sysctl", "-n", "vm.loadavg").Output()
	} else {
		raw, err = os.ReadFile("/proc/loadavg")
	}
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(strings.Trim(string(raw), "{} \n"))
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty load average")
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	return load / float64(runtime.NumCPU()), err
}

func machineMemoryPressure() (int, error) {
	if runtime.GOOS != "darwin" {
		return 0, nil
	}
	raw, err := exec.Command("sysctl", "-n", "kern.memorystatus_vm_pressure_level").Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(raw)))
}

func loadGateReason(max float64, readLoad func() (float64, error), readPressure func() (int, error)) string {
	if max <= 0 {
		return ""
	}
	load, err := readLoad()
	if err != nil {
		return "machine load unavailable: " + err.Error()
	}
	if load > max {
		return fmt.Sprintf("machine busy (load %.1f per CPU)", load)
	}
	pressure, err := readPressure()
	if err != nil {
		return "memory pressure unavailable: " + err.Error()
	}
	if pressure >= 2 {
		return "machine busy (memory pressure warning)"
	}
	return ""
}
