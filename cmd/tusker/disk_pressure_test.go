package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestDiskPressureDefaultsEffectiveFloorWarningAndDisable(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	workspacePath := filepath.Join(t.TempDir(), "workspaces", "APP-T-0001")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	config, err := store.DiskPressureConfig()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, config.Enabled, "disk pressure enabled by default")
	assertEqual(t, uint64(2<<30), config.MinFreeBytes, "default byte floor")
	assertEqual(t, float64(1), config.MinFreePercent, "default percentage floor")

	statCalls := 0
	daemon := &Daemon{
		stateRoot: stateRoot,
		store:     store,
		diskStat: func(string) (diskFilesystemStat, error) {
			statCalls++
			return diskFilesystemStat{Blocks: 400, AvailableBlocks: 5, BlockSize: 1 << 30}, nil
		},
	}
	status, err := daemon.checkDiskPressureForDispatch(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "warning", status.State, "twice-floor warning state")
	assertEqual(t, false, status.DispatchPaused, "warning does not pause dispatch")
	assertEqual(t, uint64(4<<30), status.EffectiveThresholdBytes, "larger percentage floor")
	assertEqual(t, uint64(8<<30), status.WarningThresholdBytes, "warning threshold")
	assertEqual(t, 2, len(status.Filesystems), "state and workspace filesystems")
	for _, filesystem := range status.Filesystems {
		assertEqual(t, uint64(5<<30), filesystem.AvailableBytes, filesystem.Kind+" available bytes")
		assertEqual(t, float64(1.25), filesystem.AvailablePercent, filesystem.Kind+" available percent")
		assertEqual(t, uint64(4<<30), filesystem.EffectiveThresholdBytes, filesystem.Kind+" effective threshold")
		if strings.TrimSpace(filesystem.Path) == "" || strings.TrimSpace(filesystem.FilesystemPath) == "" {
			t.Fatalf("expected requested and measured filesystem paths, got %#v", filesystem)
		}
	}

	config.MinFreeBytes = 6 << 30
	config.MinFreePercent = 0.5
	if err := store.SetDiskPressureConfig(config); err != nil {
		t.Fatal(err)
	}
	status, err = daemon.checkDiskPressureForDispatch(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "paused", status.State, "configured byte floor pauses")
	assertEqual(t, true, status.DispatchPaused, "configured pause")
	assertEqual(t, uint64(6<<30), status.EffectiveThresholdBytes, "configured byte floor wins")

	config.Enabled = false
	if err := store.SetDiskPressureConfig(config); err != nil {
		t.Fatal(err)
	}
	statCalls = 0
	status, err = daemon.checkDiskPressureForDispatch(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "disabled", status.State, "disabled state")
	assertEqual(t, false, status.DispatchPaused, "disabled guard does not pause")
	assertEqual(t, 0, statCalls, "disabled guard skips filesystem stats")
}

func TestDispatchDiskPressurePausesBeforeClaimPreservesActiveRunAndRecovers(t *testing.T) {
	vault := automationTestVault(t)
	installCodexSleepShimForTest(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Disk pressure", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wfFile.Data.Runtime.MaxActiveRunsPerProject = 2
	workspaceRoot := filepath.Join(DefaultStateRoot(), "workspaces", "disk-pressure", newRecordID())
	wfFile.Data.Workspace.Root = workspaceRoot
	wfFile.Data.Workspace.Strategy = string(WorkspaceStrategyCopy)
	wfFile.Data.Agents.Default = "test-disk-pressure"
	wfFile.Data.Agents.Enabled = append(wfFile.Data.Agents.Enabled, "test-disk-pressure")
	wfFile.Data.Runners["test-disk-pressure"] = RunnerDefinition{Kind: string(RunnerCodexExec), Command: defaultCodexExecCommand()}
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID:      project.ProjectID,
		RecordID:       "APP-T-0001",
		ItemID:         "APP-T-0001",
		Runner:         "test-disk-pressure",
		Lane:           runLaneExecute,
		LeaseState:     string(LeaseStateUnclaimed),
		AttemptOutcome: string(AttemptOutcomeNone),
		WorkRevision:   intField(note.Data, "work_revision"),
	}
	active := RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0099",
		ItemID:          "APP-T-0099",
		Runner:          "test-disk-pressure",
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		LeaseOwner:      "active-owner",
		LeaseGeneration: 7,
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "active-attempt",
		WorkspacePath:   filepath.Join(t.TempDir(), "active-workspace"),
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(active); err != nil {
		t.Fatal(err)
	}
	activeBefore := latestRunForRecord(t, store, project.ProjectID, active.RecordID)

	availableBlocks := uint64(1)
	daemon := &Daemon{
		stateRoot: DefaultStateRoot(),
		store:     store,
		diskStat: func(string) (diskFilesystemStat, error) {
			return diskFilesystemStat{Blocks: 100, AvailableBlocks: availableBlocks, BlockSize: 1 << 30}, nil
		},
	}
	blocked, persisted, err := daemon.dispatchRun(context.Background(), project, wfFile, note, run, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, persisted, "low disk dispatch persistence")
	assertEqual(t, string(LeaseStateUnclaimed), blocked.LeaseState, "low disk lease state")
	assertEqual(t, 0, blocked.LeaseGeneration, "low disk lease generation")
	assertEqual(t, "", blocked.LeaseOwner, "low disk lease owner")
	assertEqual(t, 0, blocked.AttemptCount, "low disk attempt count")
	assertEqual(t, 0, blocked.ProcessPID, "low disk process pid")
	if !strings.Contains(blocked.LastError, "disk_pressure") {
		t.Fatalf("expected disk pressure refusal, got %#v", blocked)
	}
	stored := latestRunForRecord(t, store, project.ProjectID, run.RecordID)
	assertEqual(t, string(LeaseStateUnclaimed), stored.LeaseState, "stored low disk lease state")
	assertEqual(t, 0, stored.LeaseGeneration, "stored low disk generation")
	attempts, err := store.ListAttemptsForRun(project.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, len(attempts), "low disk attempts")
	if _, err := os.Stat(workspaceRoot); !os.IsNotExist(err) {
		t.Fatalf("low disk dispatch created workspace root %s: %v", workspaceRoot, err)
	}
	activeAfter := latestRunForRecord(t, store, project.ProjectID, active.RecordID)
	assertEqual(t, activeBefore, activeAfter, "active run remains untouched")
	invariant, err := store.ReadInvariantCircuitStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, invariant.Open, "invariant circuit remains closed")

	pressure, err := store.DiskPressureStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "paused", pressure.State, "stored paused status")
	assertEqual(t, true, pressure.DispatchPaused, "stored dispatch pause")

	output := captureStdout(t, func() {
		if err := automationStatusCmd(Args{"json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var automationPayload struct {
		Status automationStatusReport `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &automationPayload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "paused", automationPayload.Status.DiskPressure.State, "automation paused status")
	server := &serveServer{vaultPath: vault, repoRoot: project.RepoRoot, store: store}
	assertEqual(t, "paused", server.daemonStatusFromSnapshot(serveSnapshot{}).DiskPressure.State, "Serve paused status")

	availableBlocks = 5
	dispatched, persisted, err := daemon.dispatchRun(context.Background(), project, wfFile, note, blocked, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, persisted, "recovered dispatch persistence")
	assertEqual(t, string(LeaseStateRunning), dispatched.LeaseState, "recovered dispatch state")
	if dispatched.ProcessPGID > 0 {
		t.Cleanup(func() { _ = syscall.Kill(-dispatched.ProcessPGID, syscall.SIGKILL) })
	} else if dispatched.ProcessPID > 0 {
		t.Cleanup(func() { _ = syscall.Kill(dispatched.ProcessPID, syscall.SIGKILL) })
	}
	pressure, err = store.DiskPressureStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "recovered", pressure.State, "level-triggered recovery status")
	assertEqual(t, false, pressure.DispatchPaused, "recovered dispatch eligibility")
	assertEqual(t, true, pressure.Recovered, "recovery transition")
	attempts, err = store.ListAttemptsForRun(project.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(attempts), "recovered attempt count")
	if strings.TrimSpace(dispatched.WorkspacePath) == "" || !fileExists(dispatched.WorkspacePath) {
		t.Fatalf("recovered dispatch did not prepare workspace: %#v", dispatched)
	}
	assertEqual(t, "recovered", server.daemonStatusFromSnapshot(serveSnapshot{}).DiskPressure.State, "Serve recovered status")
	steady, err := daemon.checkDiskPressureForDispatch(dispatched.WorkspacePath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ok", steady.State, "healthy check after recovery transition")
	assertEqual(t, false, steady.Recovered, "recovery marker is transition-only")
}

func TestDispatchDiskPressureMeasuresExistingTaskWorkspaceBeforeLeaseClaim(t *testing.T) {
	vault := automationTestVault(t)
	installCodexSleepShimForTest(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Mounted workspace", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wfFile.Data.Workspace.Root = filepath.Join(DefaultStateRoot(), "workspaces", "mounted-workspace", newRecordID())
	wfFile.Data.Workspace.Strategy = string(WorkspaceStrategyCopy)
	wfFile.Data.Agents.Default = "test-disk-pressure"
	wfFile.Data.Agents.Enabled = append(wfFile.Data.Agents.Enabled, "test-disk-pressure")
	wfFile.Data.Runners["test-disk-pressure"] = RunnerDefinition{Kind: string(RunnerCodexExec), Command: defaultCodexExecCommand()}
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID:      project.ProjectID,
		RecordID:       "APP-T-0001",
		ItemID:         "APP-T-0001",
		Runner:         "test-disk-pressure",
		Lane:           runLaneExecute,
		LeaseState:     string(LeaseStateUnclaimed),
		AttemptOutcome: string(AttemptOutcomeNone),
		WorkRevision:   intField(note.Data, "work_revision"),
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}

	request := WorkspacePrepareRequest{
		ProjectID: project.ProjectID, ProjectKey: project.ProjectKey, RecordID: run.RecordID, ItemID: run.ItemID,
		RepoRoot: project.RepoRoot, StateRoot: DefaultStateRoot(), WorkspaceRoot: wfFile.Data.Workspace.Root,
		Strategy: WorkspaceStrategyCopy, WorkRevision: run.WorkRevision,
	}
	workspacePath, workspaceRoot, err := workspacePathForRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}

	stateRoot := filepath.Clean(DefaultStateRoot())
	workspacePath = filepath.Clean(workspacePath)
	workspaceRoot = filepath.Clean(workspaceRoot)
	stateFilesystemPath, err := nearestExistingDiskPath(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	workspaceFilesystemPath, err := nearestExistingDiskPath(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	workspaceRootFilesystemPath, err := nearestExistingDiskPath(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	observedPaths := []string{}
	daemon := &Daemon{
		stateRoot: stateRoot,
		store:     store,
		diskStat: func(path string) (diskFilesystemStat, error) {
			path = filepath.Clean(path)
			observedPaths = append(observedPaths, path)
			switch path {
			case stateFilesystemPath:
				return diskFilesystemStat{Blocks: 100, AvailableBlocks: 5, BlockSize: 1 << 30, FilesystemID: "state"}, nil
			case workspaceRootFilesystemPath:
				return diskFilesystemStat{Blocks: 100, AvailableBlocks: 5, BlockSize: 1 << 30, FilesystemID: "workspace-root"}, nil
			case workspaceFilesystemPath:
				return diskFilesystemStat{Blocks: 100, AvailableBlocks: 1, BlockSize: 1 << 30, FilesystemID: "workspace-mount"}, nil
			default:
				t.Fatalf("unexpected disk-pressure measurement path %q", path)
				return diskFilesystemStat{}, nil
			}
		},
		beforeRunLeaseClaim: func(RunStatus) {
			t.Fatal("disk-pressure guard reached lease claim instead of measuring the existing task workspace")
		},
	}
	blocked, persisted, err := daemon.dispatchRun(context.Background(), project, wfFile, note, run, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, persisted, "mounted workspace disk-pressure dispatch persistence")
	assertEqual(t, string(LeaseStateUnclaimed), blocked.LeaseState, "mounted workspace disk-pressure lease state")
	assertEqual(t, 0, blocked.AttemptCount, "mounted workspace disk-pressure attempts")
	assertEqual(t, []string{stateFilesystemPath, workspaceFilesystemPath}, observedPaths, "disk-pressure paths before lease claim")
}

func TestDiskPressureRuntimeLimitsAreConfigurable(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	output := captureStdout(t, func() {
		if err := daemonLimitsCmd(Args{
			"json":                           "true",
			"disk-pressure-enabled":          "false",
			"disk-pressure-min-free-bytes":   "3221225472",
			"disk-pressure-min-free-percent": "2.5",
		}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		DiskPressure DiskPressureConfig `json:"disk_pressure"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, payload.DiskPressure.Enabled, "configured guard enabled")
	assertEqual(t, uint64(3<<30), payload.DiskPressure.MinFreeBytes, "configured byte floor")
	assertEqual(t, 2.5, payload.DiskPressure.MinFreePercent, "configured percentage floor")
}

func TestDiskPressureStatusAggregatesWorstWorkspaceAndRejectsStaleConfig(t *testing.T) {
	tempDir := t.TempDir()
	stateRoot := filepath.Join(tempDir, "state")
	workspaceA := filepath.Join(tempDir, "workspace-a")
	workspaceB := filepath.Join(tempDir, "workspace-b")
	for _, path := range []string{stateRoot, workspaceA, workspaceB} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := defaultDiskPressureConfig()
	config.MinFreePercent = 0
	if err := store.SetDiskPressureConfig(config); err != nil {
		t.Fatal(err)
	}
	available := map[string]uint64{
		filepath.Base(stateRoot):  8,
		filepath.Base(workspaceA): 1,
		filepath.Base(workspaceB): 8,
	}
	daemon := &Daemon{
		stateRoot: stateRoot,
		store:     store,
		diskStat: func(path string) (diskFilesystemStat, error) {
			return diskFilesystemStat{Blocks: 100, AvailableBlocks: available[filepath.Base(path)], BlockSize: 1 << 30}, nil
		},
	}
	if status, err := daemon.checkDiskPressureForDispatch(workspaceA); err != nil {
		t.Fatal(err)
	} else if status.State != "paused" {
		t.Fatalf("workspace A state = %q, want paused", status.State)
	}
	if status, err := daemon.checkDiskPressureForDispatch(workspaceB); err != nil {
		t.Fatal(err)
	} else if status.State != "ok" || status.DispatchPaused {
		t.Fatalf("another workspace's pressure blocked healthy dispatch: %#v", status)
	}
	if status, err := store.DiskPressureStatus(); err != nil {
		t.Fatal(err)
	} else if status.State != "paused" || !status.DispatchPaused {
		t.Fatalf("global status lost another workspace's pressure: %#v", status)
	}
	available[filepath.Base(workspaceA)] = 8
	if status, err := daemon.checkDiskPressureForDispatch(workspaceA); err != nil {
		t.Fatal(err)
	} else if status.State != "recovered" || status.DispatchPaused {
		t.Fatalf("aggregate did not recover after pressured workspace cleared: %#v", status)
	}
	if status, err := daemon.checkDiskPressureForDispatch(workspaceB); err != nil {
		t.Fatal(err)
	} else if status.State != "ok" || status.Recovered {
		t.Fatalf("recovered state did not settle to ok: %#v", status)
	}

	staleObservedAt := time.Now().UTC().Add(-diskPressureObservationTTL - time.Second).Format(time.RFC3339Nano)
	staleInactive := DiskPressureStatus{
		State: "paused", Enabled: true, DispatchPaused: true, Config: config,
		Filesystems: []DiskPressureFilesystem{
			{Kind: "state_root", Path: stateRoot, State: "ok", CheckedAt: time.Now().UTC().Format(time.RFC3339Nano)},
			{Kind: "workspace", Path: workspaceA, State: "paused", CheckedAt: staleObservedAt},
		},
	}
	if err := store.writeDiskPressureStatus(staleInactive); err != nil {
		t.Fatal(err)
	}
	if status, err := daemon.checkDiskPressureForDispatch(workspaceB); err != nil {
		t.Fatal(err)
	} else if status.DispatchPaused {
		t.Fatalf("expired inactive workspace observation blocked healthy dispatch: %#v", status)
	}
	if status, err := store.DiskPressureStatus(); err != nil {
		t.Fatal(err)
	} else if status.State != "ok" || status.DispatchPaused || status.Recovered {
		t.Fatalf("expired inactive workspace produced a false recovery: %#v", status)
	}

	if err := store.writeDiskPressureStatus(staleInactive); err != nil {
		t.Fatal(err)
	}
	if status, err := store.DiskPressureStatus(); err != nil {
		t.Fatal(err)
	} else if status.State != "ok" || status.DispatchPaused || status.Recovered {
		t.Fatalf("status read retained an expired pause: %#v", status)
	}

	stale := DiskPressureStatus{
		State:          "paused",
		Enabled:        true,
		DispatchPaused: true,
		Config:         config,
		Filesystems:    []DiskPressureFilesystem{{Kind: "workspace", Path: workspaceA, State: "paused"}},
	}
	newConfig := config
	newConfig.MinFreeBytes = 3 << 30
	if err := store.SetDiskPressureConfig(newConfig); err != nil {
		t.Fatal(err)
	}
	if err := store.writeDiskPressureStatus(stale); err != nil {
		t.Fatal(err)
	}
	status, err := store.DiskPressureStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "unknown", status.State, "stale status hidden after config change")
	assertEqual(t, false, status.DispatchPaused, "stale pause hidden after config change")
	assertEqual(t, uint64(3<<30), status.MinFreeBytes, "new config remains visible")
}

func TestDiskPressureAggregationUsesPhysicalFilesystemIdentity(t *testing.T) {
	tempDir := t.TempDir()
	stateRoot := filepath.Join(tempDir, "state")
	workspaceA := filepath.Join(tempDir, "workspace-a")
	workspaceB := filepath.Join(tempDir, "workspace-b")
	for _, path := range []string{stateRoot, workspaceA, workspaceB} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := defaultDiskPressureConfig()
	config.MinFreePercent = 0
	if err := store.SetDiskPressureConfig(config); err != nil {
		t.Fatal(err)
	}
	available := map[string]uint64{
		filepath.Base(stateRoot):  8,
		filepath.Base(workspaceA): 1,
		filepath.Base(workspaceB): 8,
	}
	daemon := &Daemon{
		stateRoot: stateRoot,
		store:     store,
		diskStat: func(path string) (diskFilesystemStat, error) {
			return diskFilesystemStat{Blocks: 100, AvailableBlocks: available[filepath.Base(path)], BlockSize: 1 << 30, FilesystemID: "shared-volume"}, nil
		},
	}
	if status, err := daemon.checkDiskPressureForDispatch(workspaceA); err != nil {
		t.Fatal(err)
	} else if !status.DispatchPaused {
		t.Fatalf("workspace A should be paused: %#v", status)
	}
	if status, err := daemon.checkDiskPressureForDispatch(workspaceB); err != nil {
		t.Fatal(err)
	} else if status.DispatchPaused {
		t.Fatalf("fresh healthy measurement of the same filesystem stayed paused: %#v", status)
	}
	status, err := store.DiskPressureStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "recovered" || status.DispatchPaused || len(status.Filesystems) != 1 {
		t.Fatalf("physical filesystem recovery was not aggregated: %#v", status)
	}
}

func TestDiskPressureStatusMergeIsAtomicAcrossStores(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	seed, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	config := defaultDiskPressureConfig()
	config.MinFreePercent = 0
	if err := seed.SetDiskPressureConfig(config); err != nil {
		t.Fatal(err)
	}
	seed.Close()

	const writers = 12
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for index := 0; index < writers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			store, err := OpenRuntimeStore(stateRoot)
			if err != nil {
				errs <- err
				return
			}
			defer store.Close()
			<-start
			now := time.Now().UTC()
			_, err = store.mergeAndWriteDiskPressureStatus(DiskPressureStatus{
				State: "ok", Enabled: true, CheckedAt: now.Format(time.RFC3339Nano), Config: config,
				Filesystems: []DiskPressureFilesystem{{
					Kind: "workspace", Path: filepath.Join(stateRoot, "workspace-"+strconv.Itoa(index)),
					FilesystemPath: stateRoot, FilesystemID: "volume-" + strconv.Itoa(index), State: "ok",
					CheckedAt: now.Format(time.RFC3339Nano),
				}},
			}, now)
			if err != nil {
				errs <- err
			}
		}(index)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	status, err := store.DiskPressureStatus()
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Filesystems) != writers {
		t.Fatalf("concurrent status merge lost observations: got %d want %d", len(status.Filesystems), writers)
	}
}

func TestServeDiskPressureLimitsForwardRuntimeSettings(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	var result serveActionResult
	servePost(t, server, "/api/daemon/limits", `{"diskPressureEnabled":false,"diskPressureMinFreeBytes":3221225472,"diskPressureMinFreePercent":2.5}`, &result)
	if !result.OK || result.Refused {
		t.Fatalf("expected Serve disk limits update to succeed, got %#v", result)
	}
	config, err := server.store.DiskPressureConfig()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, config.Enabled, "Serve disk pressure enabled")
	assertEqual(t, uint64(3<<30), config.MinFreeBytes, "Serve disk pressure byte floor")
	assertEqual(t, 2.5, config.MinFreePercent, "Serve disk pressure percent floor")
	var daemonStatus serveDaemonStatus
	serveDecode(t, server, "/api/daemon", &daemonStatus)
	assertEqual(t, "disabled", daemonStatus.DiskPressure.State, "Serve disk pressure disabled status")
}
