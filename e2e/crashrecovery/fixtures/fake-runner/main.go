package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultHoldTimeout = 2 * time.Minute

func main() {
	mode := flag.String("mode", "hold-success", "runner behavior: success, hold-success, hold, wedge, fail")
	readyFile := flag.String("ready-file", "", "path touched after fake runner starts")
	pidFile := flag.String("pid-file", "", "path receiving the fake runner pid")
	releaseFile := flag.String("release-file", "", "path that releases hold-success mode")
	completeStatus := flag.String("complete-status", "", "task status to set through tusker before a successful exit")
	tuskerBin := flag.String("tusker-bin", "tusker", "tusker binary used for public-CLI task transitions")
	exitCode := flag.Int("exit-code", 1, "exit code for fail mode")
	heartbeatEvery := flag.Duration("heartbeat-every", 100*time.Millisecond, "heartbeat interval")
	holdTimeout := flag.Duration("hold-timeout", defaultHoldTimeout, "hard wall-clock timeout for hold modes")
	flag.Parse()

	mustWrite(*pidFile, fmt.Sprintf("%d\n", os.Getpid()))
	touch(*readyFile)

	switch *mode {
	case "success":
		emitFirstEvent()
		os.Exit(0)
	case "wedge":
		for {
			time.Sleep(time.Hour)
		}
	case "fail":
		emitFirstEvent()
		os.Exit(*exitCode)
	case "hold":
		emitFirstEvent()
		if !runHoldLoop(*heartbeatEvery, *holdTimeout, nil) {
			os.Exit(124)
		}
	case "hold-success":
		emitFirstEvent()
		resolvedReleaseFile := strings.ReplaceAll(*releaseFile, "{task}", os.Getenv("TUSKER_ITEM_ID"))
		released := runHoldLoop(*heartbeatEvery, *holdTimeout, func() bool {
			return resolvedReleaseFile != "" && exists(resolvedReleaseFile)
		})
		if !released {
			os.Exit(124)
		}
		if *completeStatus != "" {
			if *completeStatus == "review" {
				if err := recordTaskProof(*tuskerBin); err != nil {
					fmt.Fprintf(os.Stderr, "fake-runner proof transition failed: %v\n", err)
					os.Exit(16)
				}
			}
			if err := setTaskStatus(*tuskerBin, *completeStatus); err != nil {
				fmt.Fprintf(os.Stderr, "fake-runner status transition failed: %v\n", err)
				os.Exit(17)
			}
		}
		emitHeartbeat()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown fake-runner mode %q\n", *mode)
		os.Exit(64)
	}
}

func recordTaskProof(tuskerBin string) error {
	taskID := os.Getenv("TUSKER_ITEM_ID")
	vault := os.Getenv("TUSKER_VAULT")
	if taskID == "" || vault == "" {
		return fmt.Errorf("missing TUSKER_ITEM_ID or TUSKER_VAULT")
	}
	cmd := exec.Command(tuskerBin,
		"verify", "add", taskID,
		"--vault", vault,
		"--covers", "A1",
		"--check", "go test ./e2e/crashrecovery",
		"--result", "pass",
		"--note", "fake runner e2e completion proof",
		"--by", "agent:fake-runner",
		"--local",
		"--quiet",
	)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func runHoldLoop(heartbeatEvery, holdTimeout time.Duration, released func() bool) bool {
	if heartbeatEvery <= 0 {
		heartbeatEvery = 100 * time.Millisecond
	}
	if holdTimeout <= 0 {
		holdTimeout = defaultHoldTimeout
	}
	timeout := time.NewTimer(holdTimeout)
	defer timeout.Stop()
	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()
	for {
		if released != nil && released() {
			return true
		}
		emitHeartbeat()
		select {
		case <-timeout.C:
			fmt.Fprintf(os.Stderr, "fake-runner hold timeout after %s\n", holdTimeout)
			return false
		case <-ticker.C:
		}
	}
}

func emitFirstEvent() {
	session := "fake-session-" + os.Getenv("TUSKER_ATTEMPT_ID")
	raw := map[string]any{
		"session_id": session,
		"event":      "first_event",
		"pid":        os.Getpid(),
	}
	encoded, _ := json.Marshal(raw)
	fmt.Println(string(encoded))
	appendEvent("fake_first_event")
}

func emitHeartbeat() {
	appendEvent("fake_heartbeat")
}

func appendEvent(kind string) {
	path := os.Getenv("TUSKER_EVENT_SINK")
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	payload := map[string]any{
		"at":         time.Now().UTC().Format(time.RFC3339Nano),
		"kind":       kind,
		"attempt_id": os.Getenv("TUSKER_ATTEMPT_ID"),
		"pid":        os.Getpid(),
	}
	raw, _ := json.Marshal(payload)
	_, _ = file.Write(append(raw, '\n'))
}

func setTaskStatus(tuskerBin, status string) error {
	taskID := os.Getenv("TUSKER_ITEM_ID")
	vault := os.Getenv("TUSKER_VAULT")
	if taskID == "" || vault == "" {
		return fmt.Errorf("missing TUSKER_ITEM_ID or TUSKER_VAULT")
	}
	cmd := exec.Command(tuskerBin,
		"status", taskID,
		"--vault", vault,
		"--status", status,
		"--by", "agent:fake-runner",
		"--reason", "fake runner completed",
		"--local",
		"--quiet",
	)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func mustWrite(path, text string) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", filepath.Dir(path), err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
		os.Exit(1)
	}
}

func touch(path string) {
	if path == "" {
		return
	}
	mustWrite(path, time.Now().UTC().Format(time.RFC3339Nano)+"\n")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
