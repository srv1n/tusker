package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Gate-tier proof economics, mechanized from
// skills/tusker/references/OPERATE.md § "Review and integration". Total cost
// is (compile/link cycles) x (cycle cost),
// so a gate invoked through Tusker must (1) refuse before paying a cycle it
// cannot use, (2) harvest the COMPLETE failure set in one pass instead of
// failing fast, and (3) hand back a defect list an agent can repair as one
// batch. Every language-specific detail — the harvest command, the profile
// name, the defect marker — is project configuration, never code here.
const (
	gateTierSchema             = "tusker.gate-tier/v1"
	gateTierModeHarvest        = "harvest"
	gateTierModeSelective      = "selective"
	gateTierDefaultDefectLines = 12
	gateTierDefaultExcerptLen  = 320
)

const (
	gateOutcomeLedgerHit = "ledger_hit"
	gateOutcomeRefused   = "refused"
	gateOutcomePassed    = "passed"
	gateOutcomeFailed    = "failed"
)

// Preflight causes, named in the doctrine's evaluation order.
const (
	gateRefusalDiskHeadroom  = "disk_headroom"
	gateRefusalBuildSlotHeld = "build_slot_held"
	// gateRefusalWorktreeCap is the refusal when opening another live work copy
	// would exceed the configured (measured) cap on concurrent worktrees.
	gateRefusalWorktreeCap   = "worktree_cap"
	gateRefusalProfileParity = "profile_parity"
	gateRefusalTreeNotFrozen = "tree_not_frozen"
	// gateRefusalDiffUnavailable is the selective gate's fail-closed refusal when
	// it cannot compute the set of paths a change touched. Passing on an
	// unavailable (hence possibly empty) diff would run nothing and green-light an
	// unverified change, so the gate refuses instead.
	gateRefusalDiffUnavailable = "diff_unavailable"
)

// GateRefusal is the structured "why not" a preflight returns instead of
// burning a cycle. Cause is stable and machine-routable; Remedy tells the agent
// what to change before rerunning.
type GateRefusal struct {
	Cause  string `json:"cause"`
	Detail string `json:"detail"`
	Remedy string `json:"remedy"`
}

// GateDefect is one harvested failure. Target is the failing test or build
// target when the project configured a defect marker, and the command itself
// otherwise.
type GateDefect struct {
	Command string `json:"command"`
	Target  string `json:"target"`
	Excerpt string `json:"excerpt"`
}

// GateTierResult is the whole outcome of one gate invocation.
type GateTierResult struct {
	Schema     string       `json:"schema"`
	Outcome    string       `json:"outcome"`
	Mode       string       `json:"mode"`
	Profile    string       `json:"profile,omitempty"`
	Toolchain  string       `json:"toolchain,omitempty"`
	TreeHash   string       `json:"tree_hash,omitempty"`
	Commands   []string     `json:"commands"`
	Touched    []string     `json:"touched,omitempty"`
	Scopes     []string     `json:"scopes,omitempty"`
	Fallback   string       `json:"fallback,omitempty"`
	Ran        []string     `json:"ran,omitempty"`
	LedgerHits []string     `json:"ledger_hits,omitempty"`
	Defects    []GateDefect `json:"defects,omitempty"`
	Refusal    *GateRefusal `json:"refusal,omitempty"`
	DurationMS int64        `json:"duration_ms"`
}

// gateLedgerReader is the seam onto the tree-keyed gate ledger.
// *RuntimeStore satisfies it; a nil reader simply disables the short-circuit,
// which keeps the gate usable in projects that have no ledger yet.
type gateLedgerReader interface {
	FindGateLedger(projectID, treeHash, command, profile, toolchain string) (*GateLedgerEntry, error)
}

// gateTierRuntime isolates the four real boundaries a gate touches: the ledger,
// the host (disk and build slot), git, and process execution. Production wires
// them in defaultGateTierRuntime; tests substitute them directly.
type gateTierRuntime struct {
	ProjectID  string
	Workspace  string
	Ledger     gateLedgerReader
	TreeHash   func(workspace string) (string, error)
	Toolchain  func(workspace string, commands []string) string
	FreeDiskGB func(path string) (float64, error)
	SlotHolder func(workspace string, locks []string) (string, bool)
	TreeStatus func(workspace string) (string, error)
	DiffPaths  func(workspace, base string) ([]string, error)
	Exec       func(workspace, command string) (string, error)
	RecordPass func(command, treeHash, profile, toolchain string, elapsed time.Duration)
	Now        func() time.Time
}

func (rt gateTierRuntime) now() time.Time {
	if rt.Now == nil {
		return time.Now()
	}
	return rt.Now()
}

// resolveGateTierPolicy reads the project's declared gate contract, falling
// back to the unattended batch-gate commands so a project declares its harvest
// command once.
func resolveGateTierPolicy(wf Workflow) GateTierPolicy {
	policy := wf.Orchestration.Gate
	if len(policy.HarvestCommands) == 0 {
		policy.HarvestCommands = wf.Orchestration.BatchGate.Commands
	}
	if strings.TrimSpace(policy.Profile) == "" {
		policy.Profile = wf.Orchestration.BatchGate.FeatureProfile
	}
	return policy
}

// runGateTier executes the project's gate contract in harvest mode behind
// preflight refusal. It never stops at the first failing command: the point of
// the gate tier is one pass, every defect.
func runGateTier(policy GateTierPolicy, requestedProfile string, rt gateTierRuntime) (GateTierResult, error) {
	started := rt.now()
	profile := strings.TrimSpace(requestedProfile)
	canonical := strings.TrimSpace(policy.Profile)
	if profile == "" {
		profile = canonical
	}
	result := GateTierResult{Schema: gateTierSchema, Mode: gateTierModeHarvest, Profile: profile, Commands: policy.HarvestCommands}
	if len(policy.HarvestCommands) == 0 {
		return result, tuskerError(errorInvalidArg, "gate tier requires orchestration.gate.harvest_commands (or orchestration.batch_gate.commands)")
	}
	if rt.TreeHash == nil || rt.Exec == nil {
		return result, tuskerError(errorInvalidArg, "gate tier runtime is missing a tree hash or execution boundary")
	}
	treeHash, err := rt.TreeHash(rt.Workspace)
	if err != nil {
		return result, tuskerError("TREE_HASH_FAILED", "cannot hash the gate tree state: "+err.Error())
	}
	result.TreeHash = treeHash
	if rt.Toolchain == nil {
		return result, tuskerError(errorInvalidArg, "gate tier runtime is missing a toolchain fingerprint boundary")
	}
	result.Toolchain = strings.TrimSpace(rt.Toolchain(rt.Workspace, policy.HarvestCommands))

	// Preflight 1: has this exact gate already passed on this exact tree?
	pending := make([]string, 0, len(policy.HarvestCommands))
	for _, command := range policy.HarvestCommands {
		if rt.Ledger != nil && result.Toolchain != "" {
			entry, lookupErr := rt.Ledger.FindGateLedger(rt.ProjectID, treeHash, command, profile, result.Toolchain)
			if lookupErr == nil && entry != nil {
				result.LedgerHits = append(result.LedgerHits, command)
				continue
			}
		}
		pending = append(pending, command)
	}
	if len(pending) == 0 {
		result.Outcome = gateOutcomeLedgerHit
		result.DurationMS = rt.now().Sub(started).Milliseconds()
		return result, nil
	}

	// Preflight 2-5: disk, build slot, profile parity, frozen tree.
	if refusal := gateTierPreflight(policy, requestedProfile, rt); refusal != nil {
		result.Outcome = gateOutcomeRefused
		result.Refusal = refusal
		result.DurationMS = rt.now().Sub(started).Milliseconds()
		return result, nil
	}

	beforeTreeHash := treeHash
	var beforeHashErr error
	for _, command := range pending {
		if beforeHashErr != nil {
			beforeTreeHash, beforeHashErr = rt.TreeHash(rt.Workspace)
		}
		commandStart := rt.now()
		output, runErr := rt.Exec(rt.Workspace, command)
		duration := rt.now().Sub(commandStart)
		result.Ran = append(result.Ran, command)
		afterTreeHash, afterHashErr := rt.TreeHash(rt.Workspace)
		if runErr == nil {
			if rt.RecordPass != nil && result.Toolchain != "" && beforeHashErr == nil && afterHashErr == nil && afterTreeHash == beforeTreeHash {
				rt.RecordPass(command, beforeTreeHash, profile, result.Toolchain, duration)
			}
		}
		if afterHashErr == nil {
			beforeTreeHash, beforeHashErr = afterTreeHash, nil
		} else {
			beforeTreeHash, beforeHashErr = "", afterHashErr
		}
		if runErr != nil {
			result.Defects = append(result.Defects, harvestGateDefects(command, output, runErr, policy)...)
		}
	}
	if len(result.Defects) == 0 {
		result.Outcome = gateOutcomePassed
	} else {
		result.Outcome = gateOutcomeFailed
	}
	result.DurationMS = rt.now().Sub(started).Milliseconds()
	return result, nil
}

// gateTierPreflight evaluates the cheap preconditions in doctrine order and
// returns the first that refuses. The ledger check happens in runGateTier
// because a hit is a pass, not a refusal.
func gateTierPreflight(policy GateTierPolicy, requestedProfile string, rt gateTierRuntime) *GateRefusal {
	if policy.MinFreeDiskGB > 0 && rt.FreeDiskGB != nil {
		free, err := rt.FreeDiskGB(rt.Workspace)
		remedy := "reclaim scratch space outside the build cache — deleting the cache buys back disk at the price of a full rebuild"
		if err != nil {
			return &GateRefusal{Cause: gateRefusalDiskHeadroom, Detail: fmt.Sprintf("cannot measure free disk on %s: %s", rt.Workspace, err.Error()), Remedy: remedy}
		}
		if free < policy.MinFreeDiskGB {
			return &GateRefusal{
				Cause:  gateRefusalDiskHeadroom,
				Detail: fmt.Sprintf("%.1f GB free on %s is below the configured %.1f GB floor", free, rt.Workspace, policy.MinFreeDiskGB),
				Remedy: remedy,
			}
		}
	}
	if len(policy.BuildSlotLocks) > 0 && rt.SlotHolder != nil {
		if lock, held := rt.SlotHolder(rt.Workspace, policy.BuildSlotLocks); held {
			return &GateRefusal{
				Cause:  gateRefusalBuildSlotHeld,
				Detail: fmt.Sprintf("project-local build slot lock %s is held, so another stream owns this host's build", lock),
				Remedy: "wait for the running build stream to finish, or clear this project-local lock if its owner is gone; it is not daemon-wide resource arbitration",
			}
		}
	}
	canonical := strings.TrimSpace(policy.Profile)
	requested := strings.TrimSpace(requestedProfile)
	if canonical != "" && requested != "" && requested != canonical {
		return &GateRefusal{
			Cause:  gateRefusalProfileParity,
			Detail: fmt.Sprintf("requested profile %q does not match the canonical gate profile %q", requested, canonical),
			Remedy: "rerun with the canonical profile; alternating feature profiles inside one wave discards the warm build",
		}
	}
	if !policy.AllowDirtyTree && rt.TreeStatus != nil {
		status, err := rt.TreeStatus(rt.Workspace)
		remedy := "commit or stash the working tree so the gate verdict is attributable to one revision"
		if err != nil {
			return &GateRefusal{Cause: gateRefusalTreeNotFrozen, Detail: fmt.Sprintf("cannot read the working tree state of %s: %s", rt.Workspace, err.Error()), Remedy: remedy}
		}
		if dirty := gateDirtyPaths(status); len(dirty) > 0 {
			return &GateRefusal{
				Cause:  gateRefusalTreeNotFrozen,
				Detail: fmt.Sprintf("working tree is not frozen: %d uncommitted path(s) including %s", len(dirty), strings.Join(dirty, ", ")),
				Remedy: remedy,
			}
		}
	}
	return nil
}

func gateDirtyPaths(status string) []string {
	var paths []string
	for _, line := range strings.Split(strings.ReplaceAll(status, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		paths = append(paths, fields[len(fields)-1])
		if len(paths) == 3 {
			break
		}
	}
	return paths
}

// harvestGateDefects turns one failing command's output into a defect per
// failing target. The marker that identifies a target is project configuration
// (`defect_target_regex`), so Tusker carries no per-language runner knowledge.
// Without a configured marker the command itself is the target and the excerpt
// is the first actionable diagnostic.
func harvestGateDefects(command, output string, runErr error, policy GateTierPolicy) []GateDefect {
	fallback := []GateDefect{{Command: command, Target: command, Excerpt: actionableGateFailure(output, runErr)}}
	expr := strings.TrimSpace(policy.DefectTargetRegex)
	if expr == "" {
		return fallback
	}
	marker, err := regexp.Compile(expr)
	if err != nil || marker.NumSubexp() < 1 {
		return fallback
	}
	limit := policy.DefectLineLimit
	if limit <= 0 {
		limit = gateTierDefaultDefectLines
	}
	var defects []GateDefect
	seen := map[string]int{}
	current := -1
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if match := marker.FindStringSubmatch(line); len(match) > 1 && strings.TrimSpace(match[1]) != "" {
			target := strings.TrimSpace(match[1])
			if at, repeat := seen[target]; repeat {
				current = at
				continue
			}
			defects = append(defects, GateDefect{Command: command, Target: target, Excerpt: safePacketText(line, gateTierDefaultExcerptLen)})
			seen[target] = len(defects) - 1
			current = len(defects) - 1
			continue
		}
		if current < 0 {
			continue
		}
		if strings.Count(defects[current].Excerpt, "\n")+1 >= limit {
			continue
		}
		defects[current].Excerpt += "\n" + safePacketText(line, gateTierDefaultExcerptLen)
	}
	if len(defects) == 0 {
		return fallback
	}
	return defects
}

func defaultGateTierRuntime(store *RuntimeStore, projectID, workspace string) gateTierRuntime {
	return defaultGateTierRuntimeWithContext(context.Background(), store, projectID, workspace)
}

func defaultGateTierRuntimeWithContext(ctx context.Context, store *RuntimeStore, projectID, workspace string) gateTierRuntime {
	if ctx == nil {
		ctx = context.Background()
	}
	rt := gateTierRuntime{
		ProjectID:  projectID,
		Workspace:  workspace,
		TreeHash:   workspaceTreeStateHash,
		Toolchain:  scheduledPromotionToolchainFingerprint,
		FreeDiskGB: freeDiskGB,
		SlotHolder: heldBuildSlot,
		TreeStatus: func(ws string) (string, error) {
			return gitFactOutput(ws, "status", "--porcelain", "--untracked-files=normal")
		},
		DiffPaths: changedGatePaths,
		Exec: func(workspace, command string) (string, error) {
			return runGateCommandContext(ctx, workspace, command)
		},
		Now: time.Now,
	}
	if store != nil {
		rt.Ledger = store
		rt.RecordPass = func(command, treeHash, profile, toolchain string, elapsed time.Duration) {
			_ = store.RecordGateLedger(GateLedgerEntry{
				ProjectID:  projectID,
				TreeHash:   treeHash,
				Command:    command,
				Profile:    profile,
				Toolchain:  toolchain,
				Host:       runtimeLeaseHost(),
				DurationMS: elapsed.Milliseconds(),
			})
		}
	}
	return rt
}

// runGateCommand is the single command-execution boundary shared by the
// Tusker-invoked gate and the unattended batch gate.
func runGateCommand(workspace, command string) (string, error) {
	return runGateCommandContext(context.Background(), workspace, command)
}

func runGateCommandContext(ctx context.Context, workspace, command string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", command)
	cmd.Dir = workspace
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil {
			return nil
		} else if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 2 * time.Second
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	return output.String(), err
}

func freeDiskGB(path string) (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return float64(stat.Bavail) * float64(stat.Bsize) / (1024 * 1024 * 1024), nil
}

func heldBuildSlot(workspace string, locks []string) (string, bool) {
	for _, lock := range locks {
		lock = strings.TrimSpace(lock)
		if lock == "" {
			continue
		}
		path := lock
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, filepath.FromSlash(lock))
		}
		if _, err := os.Lstat(path); err == nil {
			return lock, true
		}
	}
	return "", false
}

func gateRunCmd(args Args) (int, error) {
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return 1, err
	}
	defer ctx.Close()
	workspace := firstNonEmpty(strings.TrimSpace(args.String("workspace")), ctx.Project.RepoRoot)
	projectID := ctx.Project.ProjectID
	if projectID == "" {
		projectID, _ = resolveV7ProjectID(ctx.Project.VaultRoot)
	}
	policy := resolveGateTierPolicy(ctx.Workflow.Data)
	if command := strings.TrimSpace(args.String("command")); command != "" {
		policy.HarvestCommands = []string{command}
	}
	runtime := defaultGateTierRuntime(ctx.Store, projectID, workspace)

	// --changed runs the Stage 1 per-change (selective) gate: only the scopes the
	// change actually touched. It requires configured scopes; with none, refuse
	// with a clear message rather than silently running the full harvest.
	if args.Bool("changed") {
		scopes := ctx.Workflow.Data.Orchestration.Gate.Scopes
		if len(scopes) == 0 {
			return 1, tuskerError(errorInvalidArg,
				"--changed selective gate requires orchestration.gate.scopes; none are configured, so there is no per-change scoping to apply")
		}
		result, err := runSelectiveGateTier(policy, scopes, strings.TrimSpace(args.String("base")), args.String("profile"), runtime)
		if err != nil {
			return 1, err
		}
		emitJSON(result)
		if result.Outcome == gateOutcomeRefused || result.Outcome == gateOutcomeFailed {
			return 1, nil
		}
		return 0, nil
	}

	result, err := runGateTier(policy, args.String("profile"), runtime)
	if err != nil {
		return 1, err
	}
	emitJSON(result)
	if result.Outcome == gateOutcomeRefused || result.Outcome == gateOutcomeFailed {
		return 1, nil
	}
	return 0, nil
}
