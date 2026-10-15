package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func workspaceTreeStateHash(workspace string) (string, error) {
	return workspaceTreeStateHashForPaths(workspace, nil)
}

// workspaceTreeStateHashForPaths hashes the declared source paths in their
// current worktree form, including untracked files. A nil scope is the gate
// ledger's established whole-source scope; a non-empty scope is an immutable
// work-session boundary and never widens to unrelated checkout paths.
//
// generatedOutputRoots is optional so existing whole-source callers retain
// their source-only behavior. When supplied, ignored files are enumerated only
// below those exact declared roots; ignored files elsewhere remain invisible.
func workspaceTreeStateHashForPaths(workspace string, scope []string, generatedOutputRoots ...[]string) (string, error) {
	if len(generatedOutputRoots) > 1 {
		return "", fmt.Errorf("workspace material hash accepts at most one generated-output root list")
	}
	scope, err := normalizeWorkspaceMaterialScope(scope)
	if err != nil {
		return "", err
	}
	var generated []string
	if len(generatedOutputRoots) == 1 {
		generated, err = normalizeWorkspaceMaterialScope(generatedOutputRoots[0])
		if err != nil {
			return "", err
		}
	}
	tracked, err := gitFactOutput(workspace, "ls-files", "-c", "-o", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	paths := make(map[string]struct{})
	addPaths := func(raw string) {
		for _, rel := range strings.Split(raw, "\x00") {
			if rel != "" {
				paths[filepath.ToSlash(rel)] = struct{}{}
			}
		}
	}
	addPaths(tracked)
	if len(generated) > 0 {
		ignored, ignoreErr := gitFactOutput(workspace, append([]string{"--literal-pathspecs", "ls-files", "-o", "-i", "--exclude-standard", "-z", "--"}, generated...)...)
		if ignoreErr != nil {
			return "", ignoreErr
		}
		addPaths(ignored)
	}
	ordered := make([]string, 0, len(paths))
	for rel := range paths {
		ordered = append(ordered, rel)
	}
	sort.Strings(ordered)
	hash := sha256.New()
	fileHasher := sha256.New()
	streamBuffer := make([]byte, 32<<10)
	var fileDigest [sha256.Size]byte
	if scope != nil {
		for _, path := range scope {
			_, _ = hash.Write([]byte("scope\x00" + path + "\x00"))
		}
	}
	for _, rel := range ordered {
		if gateLedgerIgnoresPath(rel) || (scope != nil && !workspaceMaterialScopeContains(scope, rel)) {
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
			_, _ = hash.Write([]byte("<symlink>"))
			_, _ = hash.Write([]byte{0})
			_, _ = hash.Write([]byte(target))
			_, _ = hash.Write([]byte{0})
		} else if info.IsDir() {
			_, _ = hash.Write([]byte("<directory>"))
		} else if info.Mode().IsRegular() {
			if info.Mode().Perm()&0o111 != 0 {
				_, _ = hash.Write([]byte("<regular:executable>"))
			} else {
				_, _ = hash.Write([]byte("<regular>"))
			}
			file, openErr := os.Open(path)
			if openErr != nil {
				return "", openErr
			}
			fileHasher.Reset()
			// Hide File.WriteTo so CopyBuffer uses the same bounded buffer for
			// every file instead of allocating another one for each copy.
			_, readErr := io.CopyBuffer(fileHasher, struct{ io.Reader }{file}, streamBuffer)
			closeErr := file.Close()
			if readErr != nil {
				return "", readErr
			}
			if closeErr != nil {
				return "", closeErr
			}
			_, _ = hash.Write(fileHasher.Sum(fileDigest[:0]))
		} else {
			_, _ = hash.Write([]byte(fmt.Sprintf("<special:%#o>", info.Mode()&os.ModeType)))
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func normalizeWorkspaceMaterialScope(scope []string) ([]string, error) {
	if scope == nil {
		return nil, nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(scope))
	for _, raw := range scope {
		path := strings.Trim(strings.TrimSpace(filepath.ToSlash(raw)), "/")
		// Scope paths reach git as pathspecs, so "cmd/.." or ":(top)" would widen
		// a scoped restore to the whole checkout; "src/*" would glob. Only clean,
		// literal relative paths pass.
		if path == "" || path == "." || path == ".." || strings.HasPrefix(path, "../") || pathpkg.Clean(path) != path ||
			strings.HasPrefix(path, ":") || strings.ContainsAny(path, "*?[") || gateLedgerIgnoresPath(path) {
			return nil, fmt.Errorf("invalid workspace material scope path %q", raw)
		}
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}

func workspaceMaterialScopeContains(scope []string, rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, root := range scope {
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

// Gate identity follows the files a gate can consume, including untracked source
// in a dirty worktree. Tusker's mutable control-plane records are deliberately
// excluded: adding proof or moving a task to review cannot change compiled code.
func gateLedgerIgnoresPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	if rel == ".tusker/Dashboard.md" {
		return true
	}
	for _, prefix := range []string{
		".tusker/work/",
		".tusker/events/",
		".tusker/evidence/",
		".tusker/attempts/",
		".tusker/dashboards/",
		".tusker/locks/",
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
	if strings.TrimSpace(entry.Toolchain) == "" {
		return tuskerError(errorInvalidArg, "gate ledger requires a non-empty toolchain fingerprint")
	}
	receiptJSON := ""
	if entry.ProviderReceipt != nil {
		raw, err := json.Marshal(entry.ProviderReceipt)
		if err != nil {
			return fmt.Errorf("encode gate provider receipt: %w", err)
		}
		receiptJSON = string(raw)
	}
	_, err := s.exec(`INSERT INTO gate_ledger (id, project_id, tree_hash, command, profile, toolchain, host, duration_ms, passed_at, provider_receipt_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, tree_hash, command, profile, toolchain) DO UPDATE SET
			host=excluded.host, duration_ms=excluded.duration_ms, passed_at=excluded.passed_at, provider_receipt_json=excluded.provider_receipt_json`,
		entry.ID, entry.ProjectID, entry.TreeHash, entry.Command, entry.Profile, entry.Toolchain, entry.Host, entry.DurationMS, entry.PassedAt, receiptJSON)
	return err
}

func (s *RuntimeStore) FindGateLedger(projectID, treeHash, command, profile, toolchain string) (*GateLedgerEntry, error) {
	if strings.TrimSpace(toolchain) == "" {
		return nil, nil
	}
	var entry GateLedgerEntry
	var receiptJSON string
	err := s.queryRowScan(`SELECT id, project_id, tree_hash, command, profile, toolchain, host, duration_ms, passed_at, provider_receipt_json
		FROM gate_ledger WHERE project_id = ? AND tree_hash = ? AND command = ? AND profile = ? AND toolchain = ?`,
		[]any{projectID, treeHash, command, profile, toolchain}, &entry.ID, &entry.ProjectID, &entry.TreeHash, &entry.Command, &entry.Profile, &entry.Toolchain, &entry.Host, &entry.DurationMS, &entry.PassedAt, &receiptJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := decodeGateProviderReceipt(receiptJSON, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func decodeGateProviderReceipt(raw string, entry *GateLedgerEntry) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var receipt GateProviderReceipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		return fmt.Errorf("decode gate provider receipt: %w", err)
	}
	entry.ProviderReceipt = &receipt
	return nil
}
