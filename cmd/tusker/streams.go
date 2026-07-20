package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type streamRow struct {
	Lane           string       `json:"lane"`
	TaskID         string       `json:"task_id"`
	Runner         string       `json:"runner"`
	WorktreePath   string       `json:"worktree_path"`
	Branch         string       `json:"branch"`
	BranchAgeHours *float64     `json:"branch_age_hours,omitempty"`
	Ahead          *int         `json:"ahead,omitempty"`
	Behind         *int         `json:"behind,omitempty"`
	OwnedPaths     []string     `json:"owned_paths"`
	LastHeartbeat  string       `json:"last_heartbeat"`
	HeartbeatAge   string       `json:"heartbeat_age"`
	Status         string       `json:"status"`
	LandedAt       string       `json:"landed_at,omitempty"`
	EndState       *RunEndState `json:"end_state,omitempty"`
}

func streamsCmd(args Args) error {
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return err
	}
	defer ctx.Close()
	rows, err := buildStreamRows(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := writeStreamBoard(ctx.Project.VaultRoot, rows); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "schema": "tusker.streams/v1", "streams": rows})
		return nil
	}
	fmt.Print(renderStreamBoard(rows))
	return nil
}

func buildStreamRows(ctx *automationCommandContext, now time.Time) ([]streamRow, error) {
	rows := []streamRow{}
	for _, run := range ctx.Runs {
		if ctx.Project.ProjectID != "" && run.ProjectID != ctx.Project.ProjectID {
			continue
		}
		freshness := runFreshness(&run, now)
		note := ctx.NotesByID[run.ItemID]
		row := streamRow{Lane: run.Lane, TaskID: run.ItemID, Runner: run.Runner, WorktreePath: run.WorkspacePath, OwnedPaths: normalizeOwnedPaths(normalizeList(note.Data["owned_paths"]))}
		if freshness == "released" {
			if !strings.EqualFold(run.AttemptOutcome, "succeeded") {
				continue
			}
			attempts, err := ctx.Store.ListAttemptsForRun(run.ProjectID, run.RecordID)
			if err != nil {
				return nil, err
			}
			if len(attempts) == 0 || attempts[0].FinishedAt == "" {
				continue
			}
			finished, err := time.Parse(time.RFC3339, attempts[0].FinishedAt)
			if err != nil || now.Sub(finished) > 24*time.Hour {
				continue
			}
			row.Status = "landed"
			row.LandedAt = attempts[0].FinishedAt
			if attempts[0].EndState.Schema != "" {
				state := attempts[0].EndState
				row.EndState = &state
				row.Branch = state.Branch
			}
			rows = append(rows, row)
			continue
		}
		heartbeat := firstNonEmpty(run.LastHeartbeatAt, run.LastEventAt, run.UpdatedAt)
		age := "unknown"
		if at, e := time.Parse(time.RFC3339, heartbeat); e == nil {
			age = now.Sub(at).Round(time.Second).String()
		}
		status := freshness
		if freshness == "fresh" {
			status = firstNonEmpty(run.LeaseState, "live")
		}
		row.LastHeartbeat, row.HeartbeatAge, row.Status = heartbeat, age, status
		if facts, err := captureGitBranchFacts(run.WorkspacePath, ctx.Workflow.Data.Orchestration.DefaultBranch, now); err == nil {
			row.Branch = facts.Branch
			row.BranchAgeHours = float64Pointer(facts.BranchAgeHours)
			row.Ahead = intPointer(facts.Ahead)
			row.Behind = intPointer(facts.Behind)
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TaskID < rows[j].TaskID })
	return rows, nil
}

func renderStreamBoard(rows []streamRow) string {
	var b strings.Builder
	b.WriteString("# Streams\n\nGenerated from runtime leases and attempts. Do not edit.\n\n| Lane | Task | Runner | Worktree | Branch | Owned paths | Heartbeat | Status |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, r := range rows {
		branch := r.Branch
		if r.Ahead != nil && r.Behind != nil {
			branch = fmt.Sprintf("%s (+%d/-%d)", branch, *r.Ahead, *r.Behind)
		}
		heartbeat := strings.TrimSpace(r.LastHeartbeat)
		if heartbeat != "" {
			heartbeat += " (" + r.HeartbeatAge + ")"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", r.Lane, r.TaskID, r.Runner, r.WorktreePath, branch, strings.Join(r.OwnedPaths, ", "), heartbeat, r.Status)
	}
	return b.String()
}

func writeStreamBoard(vaultPath string, rows []streamRow) error {
	return writeText(filepath.Join(vaultPath, "dashboards", "streams.md"), renderStreamBoard(rows))
}

func refreshStreamBoardForVault(vaultPath string) error {
	if _, err := os.Stat(filepath.Join(vaultPath, "INDEX.md")); os.IsNotExist(err) {
		return nil
	}
	ctx, err := loadAutomationCommandContext(Args{"vault": vaultPath})
	if err != nil {
		if _, statErr := os.Stat(workflowPath(vaultPath)); !os.IsNotExist(statErr) {
			return err
		}
		ctx, err = loadStreamBoardFallbackContext(vaultPath)
		if err != nil {
			return err
		}
	}
	defer ctx.Close()
	rows, err := buildStreamRows(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	return writeStreamBoard(vaultPath, rows)
}

// Reconcile supports legacy/minimal V7 fixtures that intentionally have no
// WORKFLOW.md. A stream projection needs notes and runtime rows, not dispatch
// policy, so use policy defaults instead of making reconcile newly fallible.
func loadStreamBoardFallbackContext(vaultPath string) (*automationCommandContext, error) {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return nil, err
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	runs, err := store.ListRuns()
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	projectID, _ := resolveV7ProjectID(vaultPath)
	lookup := buildNoteLookup(notes)
	return &automationCommandContext{
		StateRoot:       DefaultStateRoot(),
		Store:           store,
		CloseStore:      true,
		Project:         RegisteredProject{ProjectID: projectID, RepoRoot: v7RepoRoot(vaultPath), VaultRoot: vaultPath},
		Workflow:        WorkflowFile{Path: workflowPath(vaultPath), Data: defaultWorkflow()},
		Notes:           notes,
		NotesByID:       lookup.ByID,
		NotesByRecordID: lookup.ByRecordID,
		Runs:            runs,
	}, nil
}

func refreshStreamBoardForProject(store *RuntimeStore, projectID string) error {
	if store == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil {
		return err
	}
	for _, project := range loadedRegisteredProjects(loaded) {
		if project.ProjectID == projectID {
			return refreshStreamBoardForVault(project.VaultRoot)
		}
	}
	return nil
}
