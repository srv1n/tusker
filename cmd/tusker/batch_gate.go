package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

func (s *RuntimeStore) latestBatchGateRun(projectID string) (*BatchGateRun, error) {
	var run BatchGateRun
	err := s.queryRowScan(`SELECT id, project_id, tree_hash, profile, commands_json, status, started_at, finished_at, first_failure
		FROM batch_gate_runs WHERE project_id = ? ORDER BY started_at DESC LIMIT 1`, []any{projectID},
		&run.ID, &run.ProjectID, &run.TreeHash, &run.Profile, &run.CommandsJSON, &run.Status, &run.StartedAt, &run.FinishedAt, &run.FirstFailure)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

func (s *RuntimeStore) saveBatchGateRun(run BatchGateRun) error {
	_, err := s.exec(`INSERT INTO batch_gate_runs (id, project_id, tree_hash, profile, commands_json, status, started_at, finished_at, first_failure)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET tree_hash=excluded.tree_hash, status=excluded.status, finished_at=excluded.finished_at, first_failure=excluded.first_failure`,
		run.ID, run.ProjectID, run.TreeHash, run.Profile, run.CommandsJSON, run.Status, run.StartedAt, run.FinishedAt, run.FirstFailure)
	return err
}

func (d *Daemon) scheduleBatchGateIfDue(project RegisteredProject, wf Workflow, now time.Time) error {
	policy := wf.Orchestration.BatchGate
	if !policy.Enabled || len(policy.Commands) == 0 {
		return nil
	}
	period := time.Duration(policy.PeriodHours) * time.Hour
	if period <= 0 {
		period = 24 * time.Hour
	}
	latest, err := d.store.latestBatchGateRun(project.ProjectID)
	if err != nil {
		return err
	}
	if latest != nil {
		started, parseErr := time.Parse(time.RFC3339, latest.StartedAt)
		if parseErr == nil && now.Sub(started) < period {
			return nil
		}
		if latest.Status == "running" && parseErr == nil && now.Sub(started) < 2*period {
			return nil
		}
	}
	treeHash, err := workspaceTreeStateHash(project.RepoRoot)
	if err != nil {
		return err
	}
	commandsJSON, _ := json.Marshal(policy.Commands)
	run := BatchGateRun{ID: "batch-" + strings.ToLower(newRecordID()), ProjectID: project.ProjectID, TreeHash: treeHash, Profile: policy.FeatureProfile, CommandsJSON: string(commandsJSON), Status: "running", StartedAt: now.UTC().Format(time.RFC3339)}
	if err := d.store.saveBatchGateRun(run); err != nil {
		return err
	}
	go d.executeBatchGate(project, policy, run)
	return nil
}

func (d *Daemon) executeBatchGate(project RegisteredProject, policy BatchGatePolicy, run BatchGateRun) {
	maxRepairs := policy.MaxRepairs
	if maxRepairs <= 0 {
		maxRepairs = 3
	}
	failures := 0
	for _, command := range policy.Commands {
		started := time.Now()
		cmd := exec.CommandContext(context.Background(), "/bin/sh", "-lc", command)
		cmd.Dir = project.RepoRoot
		var output bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &output
		err := cmd.Run()
		if err == nil {
			treeHash, hashErr := workspaceTreeStateHash(project.RepoRoot)
			if hashErr == nil {
				_ = d.store.RecordGateLedger(GateLedgerEntry{ID: "gate-" + strings.ToLower(newRecordID()), ProjectID: project.ProjectID, TreeHash: treeHash, Command: command, Profile: policy.FeatureProfile, Host: runtimeLeaseHost(), DurationMS: time.Since(started).Milliseconds(), PassedAt: time.Now().UTC().Format(time.RFC3339)})
			}
			continue
		}
		failures++
		excerpt := actionableGateFailure(output.String(), err)
		if run.FirstFailure == "" {
			run.FirstFailure = excerpt
		}
		if failures <= maxRepairs {
			_ = createBatchGateRepairTask(project.VaultRoot, run.ID, command, excerpt)
		}
	}
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if failures == 0 {
		run.Status = "passed"
	} else {
		run.Status = "failed"
	}
	_ = d.store.saveBatchGateRun(run)
	_ = refreshStreamBoardForVault(project.VaultRoot)
}

func actionableGateFailure(output string, runErr error) string {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	kept := make([]string, 0, 12)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "fail") || strings.Contains(lower, "error") || strings.Contains(lower, "panic") || strings.Contains(lower, "undefined") {
			kept = append(kept, safePacketText(line, 320))
			if len(kept) == 12 {
				break
			}
		}
	}
	if len(kept) == 0 && runErr != nil {
		kept = append(kept, runErr.Error())
	}
	return strings.Join(kept, "\n")
}

func createBatchGateRepairTask(vaultPath, gateRunID, command, excerpt string) error {
	if idx, err := loadV7Index(vaultPath); err == nil {
		for _, existing := range idx.Tasks {
			status := stringField(existing.Data, "status")
			if stringField(existing.Data, "batch_gate_command") != command || status == "done" || status == "cancelled" || status == "superseded" {
				continue
			}
			_, _, updateErr := mutateV7DocumentLocked(existing.AbsolutePath, v7FrontmatterOrder["task"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
				data["batch_gate_run"] = gateRunID
				data["updated_by"] = "tusker:batch-gate"
				data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
				intent := "Repair unattended batch gate `" + gateRunID + "`.\n\nFirst actionable failure:\n\n" + excerpt
				return data, replaceSection(body, "## Intent", intent), true, nil
			})
			return updateErr
		}
	}
	if _, err := resolveNote(vaultPath, "BGR"); err != nil {
		if err := newV7Epic(Args{"vault": vaultPath, "quiet": "true", "acronym": "BGR", "title": "Batch gate repairs", "summary": "Automatically generated repair work from unattended batch gates."}); err != nil && !strings.Contains(err.Error(), "already") {
			return err
		}
	}
	id := nextSafeV7TaskID(vaultPath, "BGR")
	if err := newV7Task(Args{"vault": vaultPath, "quiet": "true", "epic": "BGR", "id": id, "title": "Repair red batch gate " + gateRunID, "risk": "medium", "priority": "p1", "status": "backlog", "by": "tusker:batch-gate"}); err != nil {
		return err
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	body := strings.Join([]string{
		"## Intent", "", "Repair unattended batch gate `" + gateRunID + "`.", "", "First actionable failure:", "", excerpt,
		"", "## Acceptance", "", "| ID | Outcome | Proof |", "|---|---|---|", "| A1 | The failing batch command passes on the repaired tree. | focused_test |",
		"", "## Verification", "", "| Covers | Check | Result | Notes |", "|---|---|---|---|", "| A1 | command: " + strings.ReplaceAll(command, "|", "\\|") + " | pending | spawned from " + gateRunID + " |", "",
	}, "\n")
	_, _, err = mutateV7DocumentLocked(note.AbsolutePath, v7FrontmatterOrder["task"], func(data map[string]any, _ string) (map[string]any, string, bool, error) {
		data["next_action"] = "Fix the first actionable batch failure and rerun the failed command."
		data["batch_gate_command"] = command
		data["batch_gate_run"] = gateRunID
		data["updated_by"] = "tusker:batch-gate"
		data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		return data, body, true, nil
	})
	if err != nil {
		return err
	}
	return statusCmd(Args{"vault": vaultPath, "id": id, "status": "ready", "actor": "tusker:batch-gate", "reason": "unattended batch gate red"})
}
