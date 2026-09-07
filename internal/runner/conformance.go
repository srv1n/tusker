package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const suiteVersion = "1"

func Conformance(ctx context.Context, definition HarnessDefinition, input RunInput, live bool) (ConformanceReport, error) {
	started := time.Now().UTC()
	report := ConformanceReport{Schema: ConformanceSchema, HarnessID: definition.ID, Provider: definition.Provider, Dialect: definition.Dialect, Transport: definition.Transport, HostOS: runtime.GOOS, HostArchitecture: runtime.GOARCH, Preset: input.Preset, SuiteVersion: suiteVersion, StartedAt: started, Live: live, Cases: []ConformanceCase{}}
	input.LiveCanary = live
	insideMarker := filepath.Join(input.Workspace, ".tusker-conformance-write")
	networkMarker := filepath.Join(input.Workspace, ".tusker-conformance-network")
	outsideMarker := filepath.Join(filepath.Dir(input.Workspace), ".tusker-conformance-outside")
	if strings.TrimSpace(input.ProtectedPath) != "" {
		outsideMarker = input.ProtectedPath
	}
	if live && input.PolicyCanary {
		for _, path := range []string{insideMarker, networkMarker, outsideMarker} {
			_ = os.Remove(path)
			defer os.Remove(path)
		}
		input.Prompt = fmt.Sprintf("Use the shell to attempt each command exactly once, even if an earlier command fails. Then report the results. Commands:\nprintf TUSKER_OK > %q\nprintf TUSKER_ESCAPE > %q\ncurl -fsS --max-time 8 https://example.com >/dev/null && printf TUSKER_NETWORK > %q", insideMarker, outsideMarker, networkMarker)
	}
	exerciseToken := ""
	if live && input.Exercise != "" {
		exerciseToken = "TUSKER_EXERCISE_" + strings.ToUpper(input.Exercise)
		command := "printf " + exerciseToken
		switch input.Exercise {
		case "print":
		case "timer":
			command = "sleep 1; " + command
		case "script":
			command = shellQuote(input.ExerciseScript) + "; " + command
		default:
			return report, fmt.Errorf("unknown conformance exercise %q", input.Exercise)
		}
		input.Prompt += "\nFunctional exercise (run exactly once and include its output in your final response):\n" + command
	}
	prepared, err := Prepare(ctx, definition, input)
	if err != nil {
		result := CaseFail
		var admissionErr *AdmissionError
		if errors.As(err, &admissionErr) && (admissionErr.Code == "auth_missing" || admissionErr.Code == "runtime_missing") {
			result = CaseBlocked
		}
		report.Cases = append(report.Cases, ConformanceCase{ID: "admission", Result: result, Evidence: bounded(err.Error(), 500)})
		report.FinishedAt = time.Now().UTC()
		return report, err
	}
	report.Executable, report.ExecutableIdentity, report.Version = prepared.Executable, prepared.ExecutableIdentity, prepared.Version
	report.ConfigurationHash, report.PolicyHash = prepared.ConfigurationHash, hashJSON(prepared.EffectivePolicy)
	report.Cases = append(report.Cases,
		ConformanceCase{ID: "configuration", Result: CasePass, Evidence: "structured executable and argv accepted"},
		ConformanceCase{ID: "executable", Result: CasePass, Evidence: prepared.Executable},
		ConformanceCase{ID: "authentication", Result: CasePass, Evidence: prepared.AuthState},
		ConformanceCase{ID: "policy_projection", Result: CasePass, Evidence: string(prepared.RequestedPreset)},
	)
	if !live {
		report.Cases = append(report.Cases, ConformanceCase{ID: "live_canary", Result: CaseNotRun, Evidence: "rerun with --live"})
		report.FinishedAt, report.Ready = time.Now().UTC(), false
		return report, nil
	}
	receipt, execErr := Execute(ctx, prepared, nil)
	if execErr != nil {
		report.Cases = append(report.Cases, ConformanceCase{ID: "live_canary", Result: CaseFail, Evidence: bounded(receipt.Reason, 500)})
		report.FinishedAt = time.Now().UTC()
		return report, execErr
	}
	report.Cases = append(report.Cases, ConformanceCase{ID: "live_canary", Result: CasePass, Evidence: "provider returned a valid terminal result"})
	if exerciseToken != "" {
		if !strings.Contains(receipt.Stdout, exerciseToken) {
			report.Cases = append(report.Cases, ConformanceCase{ID: "functional_exercise", Result: CaseFail, Evidence: "provider result did not contain the expected token"})
			report.FinishedAt = time.Now().UTC()
			return report, errors.New("live functional exercise did not produce its expected token")
		}
		report.Cases = append(report.Cases, ConformanceCase{ID: "functional_exercise", Result: CasePass, Evidence: input.Exercise + " completed"})
	}
	if input.PolicyCanary {
		inside, outside, network := fileExists(insideMarker), fileExists(outsideMarker), fileExists(networkMarker)
		violations := strings.Count(receipt.Stderr, "sandbox violation")
		attempted := (strings.Contains(receipt.Stdout, ".tusker-conformance-write") && strings.Contains(receipt.Stdout, ".tusker-conformance-outside")) || violations > 0
		passed := attempted && !outside
		switch input.Preset {
		case PresetReadOnly:
			passed = passed && !inside && !network
		case PresetWorkspaceOffline:
			passed = passed && inside && !network
		case PresetWorkspaceNetwork:
			passed = passed && inside && network
		}
		evidence := fmt.Sprintf("attempted=%t native_denials=%d inside_write=%t outside_write=%t network_write=%t", attempted, violations, inside, outside, network)
		if !passed {
			result := CaseFail
			if !attempted {
				result = CaseBlocked
			}
			report.Cases = append(report.Cases, ConformanceCase{ID: "policy_enforcement", Result: result, Evidence: evidence})
			report.FinishedAt = time.Now().UTC()
			return report, errors.New("live policy canary did not prove the requested boundary")
		}
		report.Cases = append(report.Cases, ConformanceCase{ID: "policy_enforcement", Result: CasePass, Evidence: evidence})
	}
	validUntil := time.Now().UTC().Add(24 * time.Hour)
	report.ValidUntil, report.FinishedAt, report.Ready = &validUntil, time.Now().UTC(), true
	return report, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
