package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	runnercore "tusker/internal/runner"
)

const coordinatorSchema = "tusker.batch/v1"
const coordinatorReportSchema = "tusker.batch-report/v1"

var coordinatorID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$`)

type coordinatorBatch struct {
	SourceCommit       string             `json:"source_commit"`
	Schema             string             `json:"schema"`
	ID                 string             `json:"id"`
	Epic               string             `json:"epic,omitempty"`
	Name               string             `json:"name"`
	Vault              string             `json:"vault"`
	Workspace          string             `json:"workspace"`
	Created            time.Time          `json:"created"`
	Finished           time.Time          `json:"finished,omitempty"`
	Generation         int                `json:"generation"`
	NotifiedGeneration int                `json:"notified_generation"`
	MaxParallel        int                `json:"max_parallel"`
	Capacities         map[string]int     `json:"capacities,omitempty"`
	Tasks              []*coordinatorTask `json:"tasks"`
}
type coordinatorTask struct {
	Author             string           `json:"author,omitempty"`
	ProfileFingerprint string           `json:"profile_fingerprint,omitempty"`
	InputTokens        int              `json:"input_tokens,omitempty"`
	OutputTokens       int              `json:"output_tokens,omitempty"`
	WallSeconds        float64          `json:"wall_seconds,omitempty"`
	AccountedAttempt   int              `json:"accounted_attempt,omitempty"`
	RemoteRefs         string           `json:"remote_refs,omitempty"`
	ID                 string           `json:"id"`
	Title              string           `json:"title"`
	State              string           `json:"state"`
	Contract           string           `json:"contract"`
	Profile            string           `json:"profile"`
	Model              string           `json:"model"`
	Effort             string           `json:"effort"`
	Resources          map[string]int   `json:"resources"`
	Dependencies       []string         `json:"dependencies"`
	Owned              []string         `json:"owned_paths"`
	Workspace          string           `json:"workspace"`
	Budget             time.Duration    `json:"budget"`
	Attempt            int              `json:"attempt"`
	Started            time.Time        `json:"started,omitempty"`
	Finished           time.Time        `json:"finished,omitempty"`
	RetryAt            time.Time        `json:"retry_at,omitempty"`
	Resume             string           `json:"resume,omitempty"`
	Base               string           `json:"base,omitempty"`
	Branch             string           `json:"branch,omitempty"`
	Policy             map[string]int   `json:"policy"`
	Blocker            string           `json:"first_blocker,omitempty"`
	Audit              coordinatorAudit `json:"audit"`
	Commits            []string         `json:"commits,omitempty"`
	Followups          []string         `json:"followups,omitempty"`
}
type coordinatorGate struct {
	Acceptance      string `json:"acceptance"`
	Command         string `json:"command"`
	Result          string `json:"result"`
	Host            string `json:"host"`
	FinishedIST     string `json:"finished_ist,omitempty"`
	Test            string `json:"test,omitempty"`
	IsolatedCommand string `json:"isolated_command,omitempty"`
}
type coordinatorQuestion struct {
	Text string `json:"text"`
	Fork string `json:"fork,omitempty"`
}
type coordinatorRemaining struct {
	Title    string `json:"title"`
	Why      string `json:"why"`
	Evidence string `json:"evidence"`
}
type coordinatorException struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}
type coordinatorReport struct {
	Schema       string                 `json:"schema"`
	Task         string                 `json:"task"`
	Verdict      string                 `json:"verdict"`
	Commits      []string               `json:"commits"`
	Gates        []coordinatorGate      `json:"gates"`
	Questions    []coordinatorQuestion  `json:"questions"`
	Remaining    []coordinatorRemaining `json:"remaining"`
	Boxes        []coordinatorBox       `json:"boxes"`
	Exceptions   []coordinatorException `json:"path_exceptions,omitempty"`
	FirstBlocker string                 `json:"first_blocker"`
}
type coordinatorAudit struct {
	Passed   bool     `json:"passed"`
	Failures []string `json:"failures"`
	Warnings []string `json:"warnings"`
}
type coordinatorFork struct {
	ID           string    `json:"id"`
	Question     string    `json:"question"`
	Ruling       string    `json:"ruling"`
	By           string    `json:"by"`
	Date         time.Time `json:"date"`
	Doc          string    `json:"doc,omitempty"`
	SupersededBy string    `json:"superseded_by,omitempty"`
}
type coordinatorFlaky struct {
	Test   string `json:"test"`
	Reason string `json:"reason"`
	Until  string `json:"until"`
}
type coordinatorBox struct {
	ID      string     `json:"id"`
	Type    string     `json:"type"`
	Batch   string     `json:"batch,omitempty"`
	Epic    string     `json:"epic,omitempty"`
	Created time.Time  `json:"created"`
	Deleted *time.Time `json:"deleted,omitempty"`
	Expires *time.Time `json:"expires,omitempty"`
	Rate    float64    `json:"eur_per_hour"`
	EUR     float64    `json:"eur"`
}
type coordinatorRegistry struct {
	Schema     string             `json:"schema"`
	Flaky      []coordinatorFlaky `json:"flaky"`
	Boxes      []coordinatorBox   `json:"boxes"`
	LastDigest time.Time          `json:"last_digest_at"`
}
type coordinatorLaunch struct {
	CheckArgs  []string                     `json:"check_args,omitempty"`
	CheckID    string                       `json:"check_id,omitempty"`
	Schema     string                       `json:"schema"`
	Vault      string                       `json:"vault"`
	Batch      string                       `json:"batch"`
	Task       string                       `json:"task"`
	Dir        string                       `json:"dir"`
	Workspace  string                       `json:"workspace"`
	Prompt     string                       `json:"prompt"`
	Profile    ResolvedRunnerProfile        `json:"profile"`
	Definition runnercore.HarnessDefinition `json:"definition"`
	Budget     time.Duration                `json:"budget"`
	FDCount    int                          `json:"fd_count"`
}
type coordinatorProcess struct {
	PID     int    `json:"pid"`
	PGID    int    `json:"pgid"`
	Started string `json:"started"`
}

func coordinatorHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:24]
}
func coordinatorBatchPath(vault, id string) (string, error) {
	if !coordinatorID.MatchString(id) {
		return "", fmt.Errorf("invalid batch ID %q", id)
	}
	return filepath.Join(vault, "work", "batches", id+".json"), nil
}
func coordinatorRunRoot(vault, id string) string {
	return filepath.Join(DefaultStateRoot(), "coordinator", coordinatorHash(vault), id)
}
func coordinatorAttemptDir(b *coordinatorBatch, t *coordinatorTask) string {
	return filepath.Join(coordinatorRunRoot(b.Vault, b.ID), t.ID, strconv.Itoa(t.Attempt))
}

// Reject symlinks in managed paths before following or replacing them.
func coordinatorSafePath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for p := abs; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed path contains symlink: %s", p)
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}
func coordinatorWriteJSON(path string, value any) error {
	if err := coordinatorSafePath(path); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return coordinatorAtomicWrite(path, string(raw)+"\n")
}
func coordinatorReadJSON(path string, value any) error {
	if err := coordinatorSafePath(path); err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return fmt.Errorf("JSON must be a regular file of at most 4 MiB: %s", path)
	}
	raw, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(raw) > 4<<20 {
		return fmt.Errorf("JSON exceeds 4 MiB: %s", path)
	}
	return json.Unmarshal(raw, value)
}
func coordinatorLoadBatch(vault, id string) (*coordinatorBatch, error) {
	p, err := coordinatorBatchPath(vault, id)
	if err != nil {
		return nil, err
	}
	var b coordinatorBatch
	if err = coordinatorReadJSON(p, &b); err != nil {
		return nil, err
	}
	if b.Schema != coordinatorSchema || b.ID != id || b.Vault != vault {
		return nil, fmt.Errorf("batch identity/schema mismatch")
	}
	seen := map[string]bool{}
	for _, t := range b.Tasks {
		if t == nil || !coordinatorID.MatchString(t.ID) || seen[t.ID] {
			return nil, fmt.Errorf("invalid or duplicate batch task")
		}
		seen[t.ID] = true
	}
	return &b, nil
}
func coordinatorSaveBatch(b *coordinatorBatch) error {
	p, e := coordinatorBatchPath(b.Vault, b.ID)
	if e != nil {
		return e
	}
	return coordinatorWriteJSON(p, b)
}
func coordinatorRegistryPath(vault string) string { return filepath.Join(vault, "coordinator.json") }
func coordinatorLoadRegistry(vault string) (coordinatorRegistry, error) {
	r := coordinatorRegistry{Schema: "tusker.coordinator/v1"}
	err := coordinatorReadJSON(coordinatorRegistryPath(vault), &r)
	if os.IsNotExist(err) {
		err = nil
	}
	if err == nil && r.Schema != "tusker.coordinator/v1" {
		err = fmt.Errorf("unsupported coordinator registry schema")
	}
	return r, err
}
func coordinatorMutateRegistry(vault string, fn func(*coordinatorRegistry) error) error {
	path := coordinatorRegistryPath(vault)
	l, e := coordinatorDocumentLock(path, v7DocumentLockTimeout)
	if e != nil {
		return e
	}
	defer l.Close()
	r, e := coordinatorLoadRegistry(vault)
	if e != nil {
		return e
	}
	if e = fn(&r); e != nil {
		return e
	}
	return coordinatorWriteJSON(path, r)
}
func coordinatorLock(path string) (*os.File, error) {
	if err := coordinatorSafePath(path); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	fd, e := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), path)
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
func coordinatorLockBusy(err error) bool {
	return errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN)
}
func coordinatorLocksClose(files []*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}
func coordinatorResources(values []string) (map[string]int, error) {
	out := map[string]int{}
	for _, s := range values {
		if s == "docs-only" || strings.TrimSpace(s) == "" {
			continue
		}
		key, raw, found := strings.Cut(s, "=")
		n := 1
		if found {
			v, e := strconv.Atoi(raw)
			if e != nil || v < 1 {
				return nil, fmt.Errorf("invalid resource %q", s)
			}
			n = v
		}
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.-]{0,99}$`).MatchString(key) {
			return nil, fmt.Errorf("invalid resource name %q", key)
		}
		if _, ok := out[key]; ok {
			return nil, fmt.Errorf("duplicate resource %q", key)
		}
		out[key] = n
	}
	return out, nil
}
func coordinatorDuration(raw string, fallback time.Duration) (time.Duration, error) {
	if raw == "" {
		return fallback, nil
	}
	d, e := time.ParseDuration(raw)
	if e != nil || d <= 0 {
		return 0, fmt.Errorf("invalid positive duration %q", raw)
	}
	return d, nil
}
func coordinatorTaskMetadata(data map[string]any, args Args) error {
	for _, field := range []string{"runner-profile", "resources", "budget", "workspace-path"} {
		raw, ok := args[field]
		if !ok {
			continue
		}
		key := strings.ReplaceAll(field, "-", "_")
		switch field {
		case "resources":
			if _, e := coordinatorResources(splitCSV(raw)); e != nil {
				return e
			}
			data[key] = splitCSV(raw)
		case "budget":
			if _, e := coordinatorDuration(raw, 0); e != nil {
				return e
			}
			data[key] = raw
		case "workspace-path":
			if !filepath.IsAbs(raw) {
				return fmt.Errorf("workspace-path must be absolute")
			}
			data[key] = filepath.Clean(raw)
		default:
			if raw == "" {
				return fmt.Errorf("runner-profile must be explicit")
			}
			data[key] = raw
		}
	}
	return nil
}
func coordinatorTaskState(vault, id, state, blocker string) error {
	note, e := resolveV7Note(vault, id, "task")
	if e != nil {
		return e
	}
	_, _, e = mutateV7DocumentLocked(note.AbsolutePath, v7FrontmatterOrder["task"], func(d map[string]any, body string) (map[string]any, string, bool, error) {
		d["batch_state"] = state
		d["batch_blocker"] = blocker
		d["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		switch state {
		case "running":
			d["status"] = "in_progress"
		case "review":
			d["status"] = "review"
		case "parked", "stalled", "failed":
			d["status"] = "blocked"
		case "ready":
			d["status"] = "ready"
		case "done":
			d["status"] = "done"
		case "discarded":
			d["status"] = "cancelled"
		}
		return d, body, true, nil
	})
	return e
}
func coordinatorProcessAlive(p coordinatorProcess) bool {
	return processIdentityMatches(RunStatus{ProcessPID: p.PID, ProcessPGID: p.PGID, ProcessStartedAt: p.Started})
}

func coordinatorContract(note Note) string {
	raw, _ := json.Marshal(map[string]any{"contract": directWaveTaskContractFingerprint(note.Data, note.Body), "resources": normalizeList(note.Data["resources"]), "profile": firstNonEmpty(stringField(note.Data, "runner_profile"), stringField(note.Data, "execute_profile")), "budget": stringField(note.Data, "budget"), "workspace": stringField(note.Data, "workspace_path")})
	return coordinatorHash(string(raw))
}

func coordinatorDocumentLock(path string, timeout time.Duration) (*v7DocumentLock, error) {
	if err := coordinatorSafePath(path); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return acquireV7LockForIdentity(abs, abs, timeout)
}

func coordinatorAtomicWrite(path, content string) error {
	if err := coordinatorSafePath(path); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("managed file is not regular")
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
			return fmt.Errorf("managed file is hard-linked")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".batch-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err = temp.WriteString(content); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), path); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
