package main

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func demoRunSessionPermission(ctx context.Context, repo, project, task, wave, harness, capability string, proof demoSessionProof) demoSessionProof {
	route, blockers, err := demoResolveTaskRoute(demoNewExec(), repo, demoVaultPath(repo), task)
	if err != nil || len(blockers) > 0 || route.Profile == "" || route.Harness != harness {
		proof.Status, proof.Reason = "refused", fmt.Sprintf("standalone route has no usable %s profile: %v %v", harness, err, blockers)
		return proof
	}
	return demoRunSessionPermissionWithProfile(ctx, repo, project, task, wave, route.Profile, capability, proof)
}

func demoRunSessionPermissionWithProfile(ctx context.Context, repo, project, task, wave, profileName, capability string, proof demoSessionProof) demoSessionProof {
	endpoint := demoServeBaseURL + "/api/runs/" + url.PathEscape(task) + "?project=" + url.QueryEscape(project)
	blocked, err := demoWithDeniedProfile(repo, profileName, func() demoSessionProof {
		if _, startErr := demoStartWave(ctx, project, wave); startErr != nil {
			proof.Status, proof.Reason = "refused", "standalone Wave Start: "+startErr.Error()
			return proof
		}
		var before serveRunDetail
		if waitErr := demoSessionWait(ctx, endpoint, "blocked", &before, &proof); waitErr != nil {
			proof.Status, proof.Reason = "failed", waitErr.Error()
			return proof
		}
		if denyErr := demoSessionTypedPermissionDenial(before); denyErr != nil {
			proof.Status, proof.Reason = "failed", denyErr.Error()
			return proof
		}
		proof.NativeBefore = demoSessionNative(before)
		demoSessionAttempts(&proof, before)
		return proof
	})
	if err != nil {
		blocked.Status, blocked.Reason = "refused", err.Error()
		return blocked
	}
	if blocked.Status == "failed" || blocked.Status == "refused" {
		return blocked
	}
	var result serveRunSayResponse
	if err := demoSessionPost(ctx, endpoint, "continue", capability, map[string]string{"message": "Continue after the demo permission denial."}, &result); err != nil || !result.OK || result.Refused {
		blocked.Status, blocked.Reason = "failed", firstNonEmpty(result.Reason, demoSessionError(err))
		return blocked
	}
	var after serveRunDetail
	if err := demoSessionWait(ctx, endpoint, "working", &after, &blocked); err != nil {
		blocked.Status, blocked.Reason = "failed", err.Error()
		return blocked
	}
	blocked.NativeAfter = demoSessionNative(after)
	demoSessionAttempts(&blocked, after)
	blocked.Checks = demoSessionChecks(blocked)
	blocked.Status = "passed"
	for _, check := range blocked.Checks {
		if !check.Passed {
			blocked.Status, blocked.Reason = "failed", check.Reason
			break
		}
	}
	return blocked
}

// demoWithDeniedProfile temporarily makes the demo's effective execute profile
// read-only. The caller must start the wave and observe the denial inside run.
// It restores exact bytes and refuses to overwrite an intervening edit.
func demoWithDeniedProfile(repo, profileName string, run func() demoSessionProof) (proof demoSessionProof, err error) {
	manifest, loadErr := demoLoadManifest(repo)
	if loadErr != nil || manifest.RepoRoot != repo {
		return proof, fmt.Errorf("permission demo requires its seeded repo")
	}
	path := managedTuskerLocalConfigPath(demoVaultPath(repo))
	info, statErr := os.Lstat(path)
	vault, vaultErr := filepath.EvalSymlinks(demoVaultPath(repo))
	resolved, resolveErr := filepath.EvalSymlinks(path)
	if statErr != nil || !info.Mode().IsRegular() || vaultErr != nil || resolveErr != nil || resolved != filepath.Join(vault, managedTuskerLocalConfigName) {
		return proof, fmt.Errorf("permission demo config must be a regular demo-local file")
	}
	original, readErr := os.ReadFile(path)
	if readErr != nil {
		return proof, readErr
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(original, &doc); err != nil {
		return proof, err
	}
	if len(doc.Content) == 0 {
		return proof, fmt.Errorf("demo-local config is empty")
	}
	profile := demoYAMLChild(demoYAMLChild(demoYAMLChild(doc.Content[0], "automation"), "profiles"), profileName)
	if profile == nil || demoYAMLChild(profile, "harness") == nil {
		return proof, fmt.Errorf("profile %q is not defined in demo-local config", profileName)
	}
	preset, sandbox := demoYAMLChild(profile, "permission_preset"), demoYAMLChild(profile, "sandbox")
	mode := demoYAMLChild(sandbox, "mode")
	if preset == nil || mode == nil || preset.Value == "" || mode.Value == "" {
		return proof, fmt.Errorf("profile %q has no mutable permission preset and sandbox mode", profileName)
	}
	preset.Value, mode.Value = "read-only", "read-only"
	changed, marshalErr := yaml.Marshal(&doc)
	if marshalErr != nil {
		return proof, marshalErr
	}
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		return proof, err
	}
	defer func() {
		current, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(current, changed) {
			err = fmt.Errorf("demo profile changed while permission scenario ran; original config was not overwritten")
			return
		}
		if restoreErr := os.WriteFile(path, original, 0o644); restoreErr != nil {
			err = fmt.Errorf("restore demo profile: %w", restoreErr)
		}
	}()
	return run(), nil
}

func demoSessionTypedPermissionDenial(detail serveRunDetail) error {
	if detail.OperatorState.State != "blocked" || detail.OperatorState.Reason == nil {
		return fmt.Errorf("Blocked with a typed permission reason was not observed")
	}
	switch detail.OperatorState.Reason.Code {
	case string(RunFailurePermissionDenied), string(RunFailureSandboxDenied):
		return nil
	default:
		return fmt.Errorf("Blocked reason %q is not a typed permission or sandbox denial", detail.OperatorState.Reason.Code)
	}
}

func demoYAMLChild(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
