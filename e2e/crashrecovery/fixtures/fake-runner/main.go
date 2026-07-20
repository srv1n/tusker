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
	delivery := flag.Bool("delivery", false, "execute the lane-aware spec-to-wave delivery fixture")
	deliveryControl := flag.String("delivery-control", "", "fixture-only delivery failure/hold control directory")
	tuskerBin := flag.String("tusker-bin", "tusker", "tusker binary used for public-CLI task transitions")
	exitCode := flag.Int("exit-code", 1, "exit code for fail mode")
	heartbeatEvery := flag.Duration("heartbeat-every", 100*time.Millisecond, "heartbeat interval")
	holdTimeout := flag.Duration("hold-timeout", defaultHoldTimeout, "hard wall-clock timeout for hold modes")
	flag.Parse()

	mustWrite(*pidFile, fmt.Sprintf("%d\n", os.Getpid()))
	touch(*readyFile)
	if *delivery {
		emitFirstEvent()
		if os.Getenv("TUSKER_RUN_LANE") != "review" && *deliveryControl != "" {
			task := os.Getenv("TUSKER_ITEM_ID")
			if exists(filepath.Join(*deliveryControl, "fail-"+task)) {
				os.Exit(19)
			}
			if exists(filepath.Join(*deliveryControl, "hold-"+task)) && !runHoldLoop(*heartbeatEvery, *holdTimeout, func() bool { return exists(filepath.Join(*deliveryControl, "release-"+task)) }) {
				os.Exit(124)
			}
		}
		if err := runDeliveryFixture(*tuskerBin); err != nil {
			fmt.Fprintf(os.Stderr, "delivery fixture failed: %v\n", err)
			os.Exit(18)
		}
		emitHeartbeat()
		return
	}

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
				if err := ensureGitEndState(); err != nil {
					fmt.Fprintf(os.Stderr, "fake-runner git end state failed: %v\n", err)
					os.Exit(15)
				}
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

func ensureGitEndState() error {
	workspace := os.Getenv("TUSKER_WORKSPACE")
	if workspace == "" {
		return fmt.Errorf("missing TUSKER_WORKSPACE")
	}
	run := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, out)
		}
		return nil
	}
	if err := run("rev-parse", "--verify", "HEAD"); err == nil {
		return nil
	}
	if err := run("init", "-b", "main"); err != nil {
		return err
	}
	if err := run("config", "user.email", "crash@example.com"); err != nil {
		return err
	}
	if err := run("config", "user.name", "Crash Recovery"); err != nil {
		return err
	}
	if err := run("add", "-A"); err != nil {
		return err
	}
	return run("commit", "--allow-empty", "-m", "fixture end state")
}

func runDeliveryFixture(tuskerBin string) error {
	taskID := os.Getenv("TUSKER_ITEM_ID")
	workspace := os.Getenv("TUSKER_WORKSPACE")
	lane := os.Getenv("TUSKER_RUN_LANE")
	if taskID == "" || workspace == "" {
		return fmt.Errorf("missing task or workspace identity")
	}
	run := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Dir = workspace
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out)
		}
		return nil
	}
	if lane == "review" {
		kind, relPath := deliveryEvidenceForTask(taskID)
		evidenceArgs := []string{"evidence", "add", taskID, "--kind", kind, "--status", "accepted", "--accepted-by", "reviewer:e2e", "--covers", "A1", "--path", relPath, "--summary", "objective fixture artifact inspected", "--local", "--quiet"}
		if kind == "screenshot" {
			evidenceArgs = append(evidenceArgs, "--checked-by", "reviewer:e2e")
		}
		if err := run(tuskerBin, evidenceArgs...); err != nil {
			return err
		}
		if err := run(tuskerBin, "verify", "add", taskID, "--by", "reviewer:e2e", "--covers", "A1", "--check", "review: acceptance, evidence, gates, and docs", "--result", "pass", "--note", "independent fixture review", "--local", "--quiet"); err != nil {
			return err
		}
		if err := run("git", "add", ".tusker"); err != nil {
			return err
		}
		if err := run(tuskerBin, "close", taskID, "--by", "reviewer:e2e", "--reason", "fixture acceptance independently verified", "--quiet"); err != nil {
			return err
		}
		if err := run("git", "add", ".tusker"); err != nil {
			return err
		}
		return run("git", "commit", "-m", "review and close "+taskID)
	}
	artifactDir := filepath.Join(workspace, "artifacts", "delivery")
	docDir := filepath.Join(workspace, "docs", "delivery")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		return err
	}
	_, artifactRel := deliveryEvidenceForTask(taskID)
	artifact := filepath.Join(workspace, artifactRel)
	doc := filepath.Join(docDir, strings.ToLower(taskID)+".md")
	artifactBody := []byte(fmt.Sprintf("{\"task\":%q,\"acceptance\":[\"A1\"],\"result\":\"pass\"}\n", taskID))
	if strings.HasSuffix(artifact, ".svg") {
		artifactBody = []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="960" height="540" role="img" aria-label="Spec to wave delivery brief"><rect width="960" height="540" fill="#101827"/><text x="48" y="74" fill="#fff" font-size="34">Spec → Wave delivered</text><text x="48" y="130" fill="#7dd3fc" font-size="22">7 tasks · one arm · isolated review · landed</text><rect x="48" y="170" width="864" height="280" rx="18" fill="#1e293b"/><text x="78" y="225" fill="#86efac" font-size="24">Outcome: fully drained</text><text x="78" y="280" fill="#fff" font-size="20">See it: screenshot · benchmark · trace · matrix</text><text x="78" y="330" fill="#fff" font-size="20">Recovery: SIGKILL adoption and bounded reclaim</text><text x="78" y="380" fill="#fff" font-size="20">Human action: credential boundary only</text></svg>` + "\n")
	}
	if err := os.WriteFile(artifact, artifactBody, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(doc, []byte("# "+taskID+" delivery\n\nAcceptance A1 delivered in an isolated workspace.\n"), 0o644); err != nil {
		return err
	}
	focusedCheck := "test -s " + artifactRel
	if err := run("test", "-s", artifactRel); err != nil {
		return err
	}
	if err := run(tuskerBin, "verify", "add", taskID, "--by", "agent:e2e", "--covers", "A1", "--check", focusedCheck, "--result", "pass", "--note", "focused artifact test passed in the isolated workspace", "--local", "--quiet"); err != nil {
		return err
	}
	if err := requireSatisfiedProof(tuskerBin, workspace, taskID); err != nil {
		return err
	}
	if err := run(tuskerBin, "finish", taskID, "--summary", "fixture implementation complete", "--request-review", "--local", "--quiet"); err != nil {
		return err
	}
	if err := run("git", "add", "-A"); err != nil {
		return err
	}
	return run("git", "commit", "-m", "deliver "+taskID)
}

func requireSatisfiedProof(tuskerBin, workspace, taskID string) error {
	type proofStatus struct {
		TaskID         string   `json:"task_id"`
		Status         string   `json:"status"`
		Missing        []string `json:"missing"`
		MachineMissing []string `json:"machine_missing"`
	}

	cmd := exec.Command(tuskerBin, "proof", "status", taskID, "--json", "--local")
	cmd.Dir = workspace
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("read back focused proof for %s: %w: %s", taskID, err, out)
	}
	var status proofStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return fmt.Errorf("decode focused proof status for %s: %w: %s", taskID, err, out)
	}
	if status.TaskID != taskID || status.Status != "satisfied" || len(status.Missing) != 0 || len(status.MachineMissing) != 0 {
		return fmt.Errorf("focused proof for %s is not satisfied before finish: task_id=%q status=%q missing=%v machine_missing=%v", taskID, status.TaskID, status.Status, status.Missing, status.MachineMissing)
	}
	return nil
}

func deliveryEvidenceForTask(taskID string) (string, string) {
	base := "artifacts/delivery/" + strings.ToLower(taskID)
	switch taskID {
	case "APP-T-0001":
		return "screenshot", base + ".svg"
	case "APP-T-0002":
		return "benchmark", base + ".json"
	case "APP-T-0003":
		return "trace", base + ".json"
	case "APP-T-0004":
		return "integration_test", base + ".json"
	case "APP-T-0005":
		return "release_smoke", base + ".json"
	case "APP-T-0006":
		return "security_review", base + ".json"
	default:
		return "manual_smoke", base + ".json"
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
