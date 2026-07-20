package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func workspaceTreeStateHash(workspace string) (string, error) {
	tracked, err := gitFactOutput(workspace, "ls-files", "-c", "-o", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, rel := range strings.Split(tracked, "\x00") {
		if rel == "" || gateLedgerIgnoresPath(rel) {
			continue
		}
		_, _ = hash.Write([]byte(rel))
		_, _ = hash.Write([]byte{0})
		path := filepath.Join(workspace, filepath.FromSlash(rel))
		info, statErr := os.Lstat(path)
		if os.IsNotExist(statErr) {
			_, _ = hash.Write([]byte("<deleted>"))
		} else if statErr != nil {
			return "", statErr
		} else if info.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(path)
			if readErr != nil {
				return "", readErr
			}
			_, _ = hash.Write([]byte("<symlink>" + target))
		} else if info.IsDir() {
			_, _ = hash.Write([]byte("<directory>"))
		} else {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return "", readErr
			}
			fileHash := sha256.Sum256(content)
			_, _ = hash.Write(fileHash[:])
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Gate identity follows the files a gate can consume, including untracked source
// in a dirty worktree. Tusker's mutable control-plane records are deliberately
// excluded: adding proof or moving a task to review cannot change compiled code.
func gateLedgerIgnoresPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, prefix := range []string{
		".tusker/work/",
		".tusker/events/",
		".tusker/evidence/",
		".tusker/attempts/",
		".tusker/dashboards/",
		".tusker/scratch/",
		".tusker/_generated/",
	} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

func (s *RuntimeStore) RecordGateLedger(entry GateLedgerEntry) error {
	if entry.ID == "" {
		entry.ID = "gate-" + strings.ToLower(newRecordID())
	}
	if entry.PassedAt == "" {
		entry.PassedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.exec(`INSERT INTO gate_ledger (id, project_id, tree_hash, command, profile, host, duration_ms, passed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, tree_hash, command, profile) DO UPDATE SET
			host=excluded.host, duration_ms=excluded.duration_ms, passed_at=excluded.passed_at`,
		entry.ID, entry.ProjectID, entry.TreeHash, entry.Command, entry.Profile, entry.Host, entry.DurationMS, entry.PassedAt)
	return err
}

func (s *RuntimeStore) FindGateLedger(projectID, treeHash, command, profile string) (*GateLedgerEntry, error) {
	var entry GateLedgerEntry
	err := s.queryRowScan(`SELECT id, project_id, tree_hash, command, profile, host, duration_ms, passed_at
		FROM gate_ledger WHERE project_id = ? AND tree_hash = ? AND command = ? AND profile = ?`,
		[]any{projectID, treeHash, command, profile}, &entry.ID, &entry.ProjectID, &entry.TreeHash, &entry.Command, &entry.Profile, &entry.Host, &entry.DurationMS, &entry.PassedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &entry, nil
}

func gateLedgerCmd(args Args, action string) error {
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return err
	}
	defer ctx.Close()
	command := strings.TrimSpace(args.String("command"))
	if command == "" {
		return tuskerError(errorMissingArg, "gate-ledger "+action+" requires --command")
	}
	workspace := firstNonEmpty(strings.TrimSpace(args.String("workspace")), ctx.Project.RepoRoot)
	treeHash, err := workspaceTreeStateHash(workspace)
	if err != nil {
		return tuskerError("TREE_HASH_FAILED", "cannot hash current tracked tree: "+err.Error())
	}
	projectID := ctx.Project.ProjectID
	if projectID == "" {
		projectID, _ = resolveV7ProjectID(ctx.Project.VaultRoot)
	}
	profile := strings.TrimSpace(args.String("profile"))
	switch action {
	case "check":
		entry, err := ctx.Store.FindGateLedger(projectID, treeHash, command, profile)
		if err != nil {
			return err
		}
		emitJSON(map[string]any{"ok": true, "hit": entry != nil, "tree_hash": treeHash, "command": command, "profile": profile, "entry": entry})
		return nil
	case "record":
		if result := strings.ToLower(firstNonEmpty(args.String("result"), "pass")); result != "pass" && result != "passed" {
			return tuskerError(errorInvalidArg, "gate-ledger records passing gates only")
		}
		duration, _ := strconv.ParseInt(firstNonEmpty(args.String("duration-ms"), "0"), 10, 64)
		entry := GateLedgerEntry{ID: "gate-" + strings.ToLower(newRecordID()), ProjectID: projectID, TreeHash: treeHash, Command: command, Profile: profile, Host: runtimeLeaseHost(), DurationMS: duration, PassedAt: time.Now().UTC().Format(time.RFC3339)}
		if err := ctx.Store.RecordGateLedger(entry); err != nil {
			return err
		}
		emitJSON(map[string]any{"ok": true, "recorded": entry})
		return nil
	default:
		return fmt.Errorf("unknown gate-ledger action %s", action)
	}
}
