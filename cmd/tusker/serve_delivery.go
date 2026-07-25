package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const serveDeliveryErrorSchema = "tusker.serve-delivery-error/v1"

// The production value is the canonical transaction. Tests substitute only its
// environment inspector so they can prove HTTP replay behavior without a daemon.
var serveDeliveryStartFn = func(args Args, source *deliveryPlanSource) (deliveryStartResult, error) {
	return deliveryStartWithPlanSource(args, nil, source)
}

// Hooks are deterministic test seams around the exact race boundaries. They
// are nil in production and never replace the rooted open or identity checks.
var serveDeliveryPlanOpenComponentHook func(relative string, component int)
var serveDeliveryPlanPhaseHook func(phase string)

// serveDeliveryError keeps refusals useful to a product client without making
// it scrape CLI prose. The canonical command remains the source of the code,
// hint, and (where applicable) stale/preflight context.
type serveDeliveryError struct {
	Schema string `json:"schema"`
	Error  Issue  `json:"error"`
}

func serveDeliveryFailure(w http.ResponseWriter, err error) {
	issue := errorToIssue(err)
	var typed *TuskerError
	if errors.As(err, &typed) {
		issue = errorToIssue(typed)
	}
	status := http.StatusUnprocessableEntity
	if issue.Code == errorNotFound {
		status = http.StatusNotFound
	} else if issue.Code == errorInvalidTransition || strings.Contains(strings.ToLower(issue.Message), "stale") || strings.Contains(strings.ToLower(issue.Message), "changed") {
		status = http.StatusConflict
	}
	serveJSON(w, status, serveDeliveryError{Schema: serveDeliveryErrorSchema, Error: issue})
}

type serveDeliveryPlanComponent struct {
	Name string
	Dev  uint64
	Ino  uint64
	Mode uint32
}

type serveDeliveryPlanSnapshot struct {
	Path        string
	Relative    string
	Raw         []byte
	Identity    string
	directories []int
	components  []serveDeliveryPlanComponent
	file        *os.File
	size        int64
	modTime     time.Time
}

func (snapshot *serveDeliveryPlanSnapshot) Close() {
	if snapshot == nil {
		return
	}
	if snapshot.file != nil {
		_ = snapshot.file.Close()
		snapshot.file = nil
	}
	for index := len(snapshot.directories) - 1; index >= 0; index-- {
		_ = unix.Close(snapshot.directories[index])
	}
	snapshot.directories = nil
}

func (snapshot *serveDeliveryPlanSnapshot) Verify() error {
	if snapshot == nil || len(snapshot.components) == 0 || len(snapshot.directories) != len(snapshot.components) {
		return tuskerError(errorInvalidTransition, "delivery plan snapshot is no longer available")
	}
	for index, expected := range snapshot.components {
		var current unix.Stat_t
		if err := unix.Fstatat(snapshot.directories[index], expected.Name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return tuskerError(errorInvalidTransition, "delivery plan path identity changed after review")
		}
		if uint64(current.Dev) != expected.Dev || uint64(current.Ino) != expected.Ino ||
			uint32(current.Mode)&unix.S_IFMT != expected.Mode&unix.S_IFMT {
			return tuskerError(errorInvalidTransition, "delivery plan path identity changed after review")
		}
	}
	current, err := snapshot.file.Stat()
	if err != nil || current.Size() != snapshot.size || !current.ModTime().Equal(snapshot.modTime) {
		return tuskerError(errorInvalidTransition, "delivery plan contents changed after its snapshot was read")
	}
	return nil
}

// serveDeliveryPlanSnapshotAt accepts only an existing repo-relative regular
// file. Every caller-controlled component is opened relative to the already
// opened repository descriptor with O_NOFOLLOW; no pathname is reopened to
// obtain plan bytes.
func serveDeliveryPlanSnapshotAt(project RegisteredProject, raw string, nestedRoots []string) (*serveDeliveryPlanSnapshot, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return nil, tuskerError(errorMissingArg, "delivery review requires a repo-relative plan path")
	}
	if filepath.IsAbs(path) {
		return nil, tuskerError(errorInvalidArg, "delivery plan path must be relative to the repository", withPath(path))
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, tuskerError(errorInvalidArg, "delivery plan path must stay inside the repository", withPath(path))
	}
	for _, nested := range nestedRoots {
		nested = filepath.Clean(nested)
		if clean == nested || strings.HasPrefix(clean, nested+string(filepath.Separator)) {
			return nil, tuskerError(errorInvalidArg, "delivery plan belongs to a different registered nested project", withPath(path))
		}
	}
	repoRoot, err := filepath.EvalSymlinks(project.RepoRoot)
	if err != nil {
		return nil, tuskerError(errorInvalidArg, "cannot resolve repository root for delivery plan", withPath(project.RepoRoot))
	}
	rootFD, err := unix.Open(repoRoot, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, tuskerError(errorInvalidArg, "cannot open authorized repository root for delivery plan", withPath(project.RepoRoot))
	}
	snapshot := &serveDeliveryPlanSnapshot{
		Path:        filepath.Join(repoRoot, clean),
		Relative:    clean,
		directories: []int{rootFD},
		components:  []serveDeliveryPlanComponent{},
	}
	fail := func(openErr error) (*serveDeliveryPlanSnapshot, error) {
		snapshot.Close()
		if errors.Is(openErr, unix.ENOENT) {
			return nil, tuskerError(errorNotFound, "delivery plan does not exist", withPath(clean))
		}
		return nil, tuskerError(errorInvalidArg, "delivery plan path must contain only real directories and a non-symlink regular file", withPath(clean))
	}
	var rootStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil {
		return fail(err)
	}
	parts := strings.Split(clean, string(filepath.Separator))
	for index, part := range parts {
		if serveDeliveryPlanOpenComponentHook != nil {
			serveDeliveryPlanOpenComponentHook(clean, index)
		}
		final := index == len(parts)-1
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if final {
			flags |= unix.O_NONBLOCK
		} else {
			flags |= unix.O_DIRECTORY
		}
		fd, openErr := unix.Openat(snapshot.directories[index], part, flags, 0)
		if openErr != nil {
			return fail(openErr)
		}
		var stat unix.Stat_t
		if statErr := unix.Fstat(fd, &stat); statErr != nil {
			_ = unix.Close(fd)
			return fail(statErr)
		}
		mode := uint32(stat.Mode)
		wantMode := uint32(unix.S_IFDIR)
		if final {
			wantMode = unix.S_IFREG
		}
		if mode&unix.S_IFMT != wantMode {
			_ = unix.Close(fd)
			return fail(unix.EINVAL)
		}
		snapshot.components = append(snapshot.components, serveDeliveryPlanComponent{Name: part, Dev: uint64(stat.Dev), Ino: uint64(stat.Ino), Mode: mode})
		if final {
			snapshot.file = os.NewFile(uintptr(fd), snapshot.Path)
		} else {
			snapshot.directories = append(snapshot.directories, fd)
		}
	}
	before, err := snapshot.file.Stat()
	if err != nil {
		return fail(err)
	}
	snapshot.Raw, err = io.ReadAll(snapshot.file)
	if err != nil {
		return fail(err)
	}
	after, err := snapshot.file.Stat()
	if err != nil {
		return fail(err)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		snapshot.Close()
		return nil, tuskerError(errorInvalidTransition, "delivery plan changed while its snapshot was being read; review it again", withPath(clean))
	}
	snapshot.size = after.Size()
	snapshot.modTime = after.ModTime()
	identity := sha256.New()
	_, _ = fmt.Fprintf(identity, "root:%d:%d\npath:%x\n", uint64(rootStat.Dev), uint64(rootStat.Ino), []byte(filepath.ToSlash(clean)))
	for _, component := range snapshot.components {
		_, _ = fmt.Fprintf(identity, "component:%x:%d:%d:%d\n", []byte(component.Name), component.Dev, component.Ino, component.Mode&unix.S_IFMT)
	}
	snapshot.Identity = fmt.Sprintf("sha256:%x", identity.Sum(nil))
	if err := snapshot.Verify(); err != nil {
		snapshot.Close()
		return nil, tuskerError(errorInvalidTransition, "delivery plan path changed while its snapshot was being opened; review it again", withPath(clean), withContext(map[string]any{"cause": err.Error()}))
	}
	return snapshot, nil
}

func serveDeliveryPlanPath(project RegisteredProject, raw string) (string, error) {
	snapshot, err := serveDeliveryPlanSnapshotAt(project, raw, nil)
	if err != nil {
		return "", err
	}
	defer snapshot.Close()
	return snapshot.Path, nil
}

func (s *serveServer) serveDeliveryPlanPath(project RegisteredProject, raw string) (string, error) {
	snapshot, err := s.serveDeliveryPlanSnapshot(project, raw)
	if err != nil {
		return "", err
	}
	defer snapshot.Close()
	return snapshot.Path, nil
}

func (s *serveServer) serveDeliveryPlanSnapshot(project RegisteredProject, raw string) (*serveDeliveryPlanSnapshot, error) {
	projects, err := s.store.ListProjects()
	if err != nil {
		return nil, err
	}
	selectedRepo := canonicalProjectPath(project.RepoRoot)
	nestedRoots := []string{}
	nestedProjects := map[string]RegisteredProject{}
	for _, other := range projects {
		if serveSameRegisteredProject(project, other) {
			continue
		}
		for _, root := range []string{other.RepoRoot, other.VaultRoot} {
			otherRoot := canonicalProjectPath(root)
			if otherRoot == "" || !pathWithin(selectedRepo, otherRoot) || sameCanonicalProjectPath(selectedRepo, otherRoot) {
				continue
			}
			relative, relativeErr := filepath.Rel(selectedRepo, otherRoot)
			if relativeErr == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				nestedRoots = append(nestedRoots, relative)
				nestedProjects[filepath.Clean(relative)] = other
			}
		}
	}
	snapshot, err := serveDeliveryPlanSnapshotAt(project, raw, nestedRoots)
	if err != nil && strings.Contains(err.Error(), "different registered nested project") {
		nestedProject := RegisteredProject{}
		clean := filepath.Clean(strings.TrimSpace(raw))
		longest := -1
		for nestedRoot, candidate := range nestedProjects {
			if (clean == nestedRoot || strings.HasPrefix(clean, nestedRoot+string(filepath.Separator))) && len(nestedRoot) > longest {
				nestedProject = candidate
				longest = len(nestedRoot)
			}
		}
		hint := "select the nested project before reviewing or starting this plan"
		context := map[string]any{"selectedProject": project.ProjectID}
		if nestedProject.ProjectID != "" {
			hint = "select project " + nestedProject.ProjectID + " before reviewing or starting this plan"
			context["nestedProject"] = nestedProject.ProjectID
		}
		return nil, tuskerError(
			errorInvalidArg,
			"delivery plan belongs to a different registered nested project",
			withPath(raw),
			withHint(hint),
			withContext(context),
		)
	}
	return snapshot, err
}

func serveSameRegisteredProject(left, right RegisteredProject) bool {
	if strings.TrimSpace(left.ProjectID) != "" && left.ProjectID == right.ProjectID {
		return true
	}
	return sameCanonicalProjectPath(left.RepoRoot, right.RepoRoot) ||
		sameCanonicalProjectPath(left.VaultRoot, right.VaultRoot)
}

func (s *serveServer) handleDeliveryReview(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForSnapshot(strings.TrimSpace(r.URL.Query().Get("project")))
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
	snapshot, err := s.serveDeliveryPlanSnapshot(project, r.URL.Query().Get("plan"))
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
	defer snapshot.Close()
	if serveDeliveryPlanPhaseHook != nil {
		serveDeliveryPlanPhaseHook("schema")
	}
	if schema, schemaErr := deliveryPlanSchemaBytes(snapshot.Raw); schemaErr != nil {
		serveDeliveryFailure(w, schemaErr)
		return
	} else if schema != deliveryPlanV2Schema {
		serveDeliveryFailure(w, tuskerError(errorInvalidArg, "Serve delivery review requires a tusker.delivery-plan/v2 plan"))
		return
	}
	if serveDeliveryPlanPhaseHook != nil {
		serveDeliveryPlanPhaseHook("review")
	}
	review, err := buildDeliveryReviewBytes(project.VaultRoot, snapshot.Path, snapshot.Raw, inspectWavePreflightEnvironmentReadOnly)
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
	review.Start.PlanIdentity = snapshot.Identity
	// Review is intentionally read-only. In particular, do not refresh, import,
	// arm, or notify anything from this path.
	serveJSON(w, http.StatusOK, review)
}

func (s *serveServer) handleDeliveryStart(w http.ResponseWriter, body serveActionBody) {
	args, project, err := serveBaseArgsForBody(s, body)
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
	snapshot, err := s.serveDeliveryPlanSnapshot(project, body.string("plan"))
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
	defer snapshot.Close()
	confirmedIdentity := strings.TrimSpace(body.string("planIdentity", "plan_identity"))
	if confirmedIdentity == "" {
		serveDeliveryFailure(w, tuskerError(errorMissingArg, "delivery start requires the exact reviewed plan identity"))
		return
	}
	if confirmedIdentity != snapshot.Identity {
		serveDeliveryFailure(w, tuskerError(errorInvalidTransition, "delivery plan identity changed; review and confirm the current plan again"))
		return
	}
	args["plan"] = snapshot.Path
	args["confirm"] = body.string("confirm", "planFingerprint", "plan_fingerprint")
	args["by"] = firstNonEmpty(body.string("actor", "by"), serveOperatorActor())
	if serveDeliveryPlanPhaseHook != nil {
		serveDeliveryPlanPhaseHook("start")
	}
	source := &deliveryPlanSource{
		Path: snapshot.Path,
		Raw:  append([]byte(nil), snapshot.Raw...),
		BeforeMutation: func() {
			if serveDeliveryPlanPhaseHook != nil {
				serveDeliveryPlanPhaseHook("import")
			}
		},
		Verify: snapshot.Verify,
	}
	result, err := serveDeliveryStartFn(args, source)
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
	// Start itself is the canonical import + preflight + arm transaction. This
	// adapter never changes automation, daemon, runner, release, or gate state.
	s.invalidateProjectSnapshot(project.ProjectID)
	serveJSON(w, http.StatusOK, result)
}
