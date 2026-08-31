package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestV7DocumentCASConcurrentProcessesAllowExactlyOneWriter(t *testing.T) {
	tempDir := t.TempDir()
	documentPath := filepath.Join(tempDir, "vault", "work", "tasks", "APP-T-0001.md")
	if err := os.MkdirAll(filepath.Dir(documentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "\n## Contract\n\nConcurrent CAS fixture.\n"
	data := map[string]any{
		"id":          "APP-T-0001",
		"kind":        "task",
		"next_action": "initial",
	}
	baseRev := v7StateRev(data, body)
	data["state_rev"] = baseRev
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(documentPath, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}

	goPath := filepath.Join(tempDir, "go")
	commands := make([]*exec.Cmd, 2)
	readyPaths := make([]string, 2)
	for index := range commands {
		readyPaths[index] = filepath.Join(tempDir, "ready-"+string(rune('0'+index)))
		command := exec.Command(os.Args[0], "-test.run=^TestV7DocumentCASProcessHelper$")
		command.Env = append(os.Environ(),
			"TUSKER_V7_CAS_HELPER=1",
			"TUSKER_V7_CAS_PATH="+documentPath,
			"TUSKER_V7_CAS_BASE="+baseRev,
			"TUSKER_V7_CAS_VALUE=writer-"+string(rune('0'+index)),
			"TUSKER_V7_CAS_READY="+readyPaths[index],
			"TUSKER_V7_CAS_GO="+goPath,
			"TUSKER_STATE_ROOT="+filepath.Join(tempDir, "state-"+string(rune('0'+index))),
		)
		commands[index] = command
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	waitForV7CASTestFiles(t, readyPaths, 5*time.Second)
	if err := os.WriteFile(goPath, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}

	successes := 0
	conflicts := 0
	for _, command := range commands {
		err := command.Wait()
		if err == nil {
			successes++
			continue
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
			conflicts++
			continue
		}
		t.Fatalf("CAS helper failed unexpectedly: %v", err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one successful writer and one CAS conflict, got successes=%d conflicts=%d", successes, conflicts)
	}

	finalData, finalBody, err := parseFrontmatterMustRead(documentPath)
	if err != nil {
		t.Fatalf("final document is invalid: %v", err)
	}
	if !v7StateRevMatches(finalData, finalBody, stringField(finalData, "state_rev")) {
		t.Fatal("final document state_rev does not match its content")
	}
	if value := stringField(finalData, "next_action"); value != "writer-0" && value != "writer-1" {
		t.Fatalf("final document contains unexpected writer value %q", value)
	}
	if info, err := os.Stat(documentPath); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o640 {
		t.Fatalf("document mode changed: got %o want 640", info.Mode().Perm())
	}
}

func TestV7SkillMutationUsesOneLockForDocumentAndMaterialEpoch(t *testing.T) {
	vault := t.TempDir()
	if err := os.Mkdir(filepath.Join(vault, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(vault, "SKILL.md")
	body := "\n# Project skill\n"
	data := map[string]any{"schema": "tusker.project-skill/v7", "kind": "project_skill"}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, []string{"schema", "kind", "state_rev"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}

	_, changed, err := mutateV7DocumentLocked(skillPath, []string{"schema", "kind", "title", "state_rev"}, func(data map[string]any, body string) (map[string]any, string, bool, error) {
		data["title"] = "Tusker"
		return data, body, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("SKILL.md mutation did not run")
	}
}

func TestV7DocumentCASAllowsBodyOnlyHumanEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "APP-T-0001.md")
	body := "\n## Question\n\nOriginal.\n"
	data := map[string]any{"schema": "tusker.task/v7", "kind": "task", "id": "APP-T-0001", "next_action": "start"}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}

	current, _, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	editedBody := "\n## Question\n\nHuman prose edit.\n"
	edited, err := serializeDocument(current, editedBody, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(edited), 0o640); err != nil {
		t.Fatal(err)
	}
	current["next_action"] = "continue"
	if _, err := saveV7DocumentCAS(path, current, editedBody, v7FrontmatterOrder["task"], stringField(current, "state_rev")); err != nil {
		t.Fatal(err)
	}
	_, finalBody, err := parseFrontmatterMustRead(path)
	if err != nil || !strings.Contains(finalBody, "Human prose edit.") {
		t.Fatalf("body-only edit was not preserved: body=%q err=%v", finalBody, err)
	}
}

func TestV7DocumentCASProcessHelper(t *testing.T) {
	if os.Getenv("TUSKER_V7_CAS_HELPER") != "1" {
		return
	}
	documentPath := os.Getenv("TUSKER_V7_CAS_PATH")
	data, body, err := parseFrontmatterMustRead(documentPath)
	if err != nil {
		os.Exit(2)
	}
	data["next_action"] = os.Getenv("TUSKER_V7_CAS_VALUE")
	if err := os.WriteFile(os.Getenv("TUSKER_V7_CAS_READY"), []byte("ready"), 0o600); err != nil {
		os.Exit(2)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !fileExists(os.Getenv("TUSKER_V7_CAS_GO")) {
		if time.Now().After(deadline) {
			os.Exit(2)
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, err = saveV7DocumentCAS(documentPath, data, body, v7FrontmatterOrder["task"], os.Getenv("TUSKER_V7_CAS_BASE"))
	if err == nil {
		return
	}
	if typed, ok := err.(*TuskerError); ok && typed.Code == "CAS_CONFLICT" {
		os.Exit(3)
	}
	os.Exit(2)
}

func TestV7DocumentCASAtomicFailuresPreserveOriginal(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*v7AtomicWriteOps)
	}{
		{
			name: "write",
			mutate: func(ops *v7AtomicWriteOps) {
				ops.writeFile = func(file *os.File, content string) (int, error) {
					written, _ := file.WriteString(content[:min(4, len(content))])
					return written, errors.New("injected write failure")
				}
			},
		},
		{
			name: "sync",
			mutate: func(ops *v7AtomicWriteOps) {
				ops.syncFile = func(*os.File) error { return errors.New("injected sync failure") }
			},
		},
		{
			name: "close",
			mutate: func(ops *v7AtomicWriteOps) {
				ops.closeFile = func(file *os.File) error {
					_ = file.Close()
					return errors.New("injected close failure")
				}
			},
		},
		{
			name: "rename",
			mutate: func(ops *v7AtomicWriteOps) {
				ops.rename = func(string, string) error { return errors.New("injected rename failure") }
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			documentPath := filepath.Join(t.TempDir(), "APP-T-0001.md")
			original := []byte("original canonical document\n")
			if err := os.WriteFile(documentPath, original, 0o640); err != nil {
				t.Fatal(err)
			}
			ops := defaultV7AtomicWriteOps()
			testCase.mutate(&ops)
			if err := atomicReplaceV7DocumentWithOps(documentPath, "replacement\n", ops); err == nil {
				t.Fatal("expected injected atomic-write failure")
			}
			after, err := os.ReadFile(documentPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(original) {
				t.Fatalf("canonical document changed after %s failure: %q", testCase.name, after)
			}
			temps, err := filepath.Glob(filepath.Join(filepath.Dir(documentPath), ".APP-T-0001.md.tmp-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(temps) != 0 {
				t.Fatalf("temporary files leaked after %s failure: %v", testCase.name, temps)
			}
		})
	}
}

func TestV7DocumentCASParentDirectorySyncFailureIsReportedAfterRename(t *testing.T) {
	documentPath := filepath.Join(t.TempDir(), "APP-T-0001.md")
	if err := os.WriteFile(documentPath, []byte("original canonical document\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	ops := defaultV7AtomicWriteOps()
	var syncedDirectory string
	ops.syncDirectory = func(path string) error {
		syncedDirectory = path
		return errors.New("injected parent directory sync failure")
	}
	err := atomicReplaceV7DocumentWithOps(documentPath, "replacement\n", ops)
	if err == nil {
		t.Fatal("expected parent directory sync failure")
	}
	if !strings.Contains(err.Error(), "sync V7 document parent directory after rename") {
		t.Fatalf("expected parent directory sync context, got %v", err)
	}
	if !strings.Contains(err.Error(), "injected parent directory sync failure") {
		t.Fatalf("expected injected failure to be preserved, got %v", err)
	}
	if syncedDirectory != filepath.Dir(documentPath) {
		t.Fatalf("synced directory = %q, want %q", syncedDirectory, filepath.Dir(documentPath))
	}

	after, err := os.ReadFile(documentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "replacement\n" {
		t.Fatalf("canonical document was not replaced before parent directory sync: %q", after)
	}
	temps, err := filepath.Glob(filepath.Join(filepath.Dir(documentPath), ".APP-T-0001.md.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) != 0 {
		t.Fatalf("temporary files leaked after parent directory sync failure: %v", temps)
	}
}

func TestV7DocumentCASLockStateIsOwnerOnlyAndOutsideVault(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempDir, "vault", ".tusker", "runtime-state"))
	documentPath := filepath.Join(tempDir, "vault", "work", "tasks", "APP-T-0001.md")
	if err := os.MkdirAll(filepath.Dir(documentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(documentPath, []byte("fixture"), 0o640); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireV7DocumentLock(documentPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()

	lockDir := v7DocumentLockDirectory()
	if strings.HasPrefix(lockDir, filepath.Join(tempDir, "vault")+string(os.PathSeparator)) {
		t.Fatalf("lock directory is inside canonical vault: %s", lockDir)
	}
	if info, err := os.Stat(lockDir); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o700 {
		t.Fatalf("lock directory mode = %o, want 700", info.Mode().Perm())
	}
	if info, err := os.Stat(lock.file.Name()); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("lock file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestV7DocumentCASRevisionlessBaseStillConflictsAfterFirstWriter(t *testing.T) {
	tempDir := t.TempDir()
	documentPath := filepath.Join(tempDir, "APP-T-0001.md")
	body := "\nRevisionless fixture.\n"
	initial := map[string]any{"id": "APP-T-0001", "kind": "task", "next_action": "initial"}
	content, err := serializeDocument(initial, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(documentPath, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	first, firstBody, err := parseFrontmatterMustRead(documentPath)
	if err != nil {
		t.Fatal(err)
	}
	second := make(map[string]any, len(first))
	for key, value := range first {
		second[key] = value
	}
	first["next_action"] = "first writer"
	second["next_action"] = "stale second writer"
	if _, err := saveV7DocumentCAS(documentPath, first, firstBody, v7FrontmatterOrder["task"], ""); err != nil {
		t.Fatal(err)
	}
	if _, err := saveV7DocumentCAS(documentPath, second, firstBody, v7FrontmatterOrder["task"], ""); err == nil {
		t.Fatal("expected revisionless stale writer to receive CAS conflict")
	} else if typed, ok := err.(*TuskerError); !ok || typed.Code != "CAS_CONFLICT" {
		t.Fatalf("expected CAS_CONFLICT, got %v", err)
	}
}

func TestV7DocumentCASRefusesHardLinkedCanonicalDocument(t *testing.T) {
	tempDir := t.TempDir()
	documentPath := filepath.Join(tempDir, "APP-T-0001.md")
	aliasPath := filepath.Join(tempDir, "APP-T-0001-alias.md")
	if err := os.WriteFile(documentPath, []byte("fixture"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(documentPath, aliasPath); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireV7DocumentLock(documentPath, time.Second); err == nil || !strings.Contains(err.Error(), "hard-linked") {
		t.Fatalf("expected hard-link refusal, got %v", err)
	}
}

func TestV7DocumentCASLockIsReleasedWhenWriterProcessExits(t *testing.T) {
	tempDir := t.TempDir()
	stateRoot := filepath.Join(tempDir, "state")
	documentPath := filepath.Join(tempDir, "vault", "work", "tasks", "APP-T-0001.md")
	if err := os.MkdirAll(filepath.Dir(documentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(documentPath, []byte("fixture"), 0o640); err != nil {
		t.Fatal(err)
	}
	readyPath := filepath.Join(tempDir, "ready")
	command := exec.Command(os.Args[0], "-test.run=^TestV7DocumentLockProcessExitHelper$")
	command.Env = append(os.Environ(),
		"TUSKER_V7_LOCK_EXIT_HELPER=1",
		"TUSKER_V7_CAS_PATH="+documentPath,
		"TUSKER_V7_CAS_READY="+readyPath,
		"TUSKER_STATE_ROOT="+stateRoot,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waitForV7CASTestFiles(t, []string{readyPath}, 5*time.Second)

	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	started := time.Now()
	lock, err := acquireV7DocumentLock(documentPath, 2*time.Second)
	if err != nil {
		t.Fatalf("lock remained unavailable after writer process exited: %v", err)
	}
	defer func() { _ = lock.Close() }()
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond {
		t.Fatalf("parent acquired lock too quickly (%s); helper did not hold it across the check", elapsed)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("lock helper failed: %v", err)
	}
}

func TestV7DocumentLockProcessExitHelper(t *testing.T) {
	if os.Getenv("TUSKER_V7_LOCK_EXIT_HELPER") != "1" {
		return
	}
	lock, err := acquireV7DocumentLock(os.Getenv("TUSKER_V7_CAS_PATH"), time.Second)
	if err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Getenv("TUSKER_V7_CAS_READY"), []byte("ready"), 0o600); err != nil {
		os.Exit(2)
	}
	time.Sleep(150 * time.Millisecond)
	runtime.KeepAlive(lock)
	os.Exit(0)
}

func TestV7DocumentCASFailureDoesNotMutateCallerRevision(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempDir, "state"))
	targetPath := filepath.Join(tempDir, "target.md")
	linkPath := filepath.Join(tempDir, "task.md")
	body := "\nBody.\n"
	data := map[string]any{"id": "APP-T-0001", "kind": "task", "next_action": "initial"}
	baseRev := v7StateRev(data, body)
	data["state_rev"] = baseRev
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Fatal(err)
	}
	data["next_action"] = "must not land"
	if _, err := saveV7DocumentCAS(linkPath, data, body, v7FrontmatterOrder["task"], baseRev); err == nil {
		t.Fatal("expected symlink replacement refusal")
	}
	if got := stringField(data, "state_rev"); got != baseRev {
		t.Fatalf("caller state_rev mutated after failed write: got %q want %q", got, baseRev)
	}
}

func waitForV7CASTestFiles(t *testing.T, paths []string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		allReady := true
		for _, path := range paths {
			if !fileExists(path) {
				allReady = false
				break
			}
		}
		if allReady {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for CAS helper readiness: %v", paths)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
