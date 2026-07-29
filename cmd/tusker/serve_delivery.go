package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const serveDeliveryErrorSchema = "tusker.serve-delivery-error/v1"
const serveDeliveryPlanListSchema = "tusker.delivery-plan-list/v1"

// Delivery plans are authored product input, not runtime state. Keep the
// inbox rooted to the conventional project directory so merely opening Serve
// cannot wander through arbitrary files or trigger any delivery work.
const serveDeliveryPlanMaxBytes int64 = 1 << 20

type serveDeliveryPlanList struct {
	Schema   string                     `json:"schema"`
	ReadOnly bool                       `json:"readOnly"`
	Plans    []serveDeliveryPlanSummary `json:"plans"`
}

type serveDeliveryPlanSummary struct {
	Path                string                         `json:"path"`
	Title               string                         `json:"title"`
	Summary             string                         `json:"summary,omitempty"`
	SpecRefs            []string                       `json:"specRefs"`
	TaskCount           int                            `json:"taskCount"`
	ExpectedConcurrency int                            `json:"expectedConcurrency"`
	RunnerProfile       string                         `json:"runnerProfile,omitempty"`
	Tasks               []serveDeliveryPlanTaskSummary `json:"tasks"`
	State               string                         `json:"state"`
	Issue               string                         `json:"issue,omitempty"`
}

type serveDeliveryPlanTaskSummary struct {
	SourceKey     string `json:"sourceKey"`
	Title         string `json:"title"`
	RunnerProfile string `json:"runnerProfile,omitempty"`
	Complexity    string `json:"complexity,omitempty"`
	Risk          string `json:"risk,omitempty"`
}

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
	issue := serveErrorIssue(err)
	status := http.StatusUnprocessableEntity
	if issue.Code == errorNotFound {
		status = http.StatusNotFound
	} else if issue.Code == errorInvalidTransition || strings.Contains(strings.ToLower(issue.Message), "stale") || strings.Contains(strings.ToLower(issue.Message), "changed") {
		status = http.StatusConflict
	}
	serveJSON(w, status, serveDeliveryError{Schema: serveDeliveryErrorSchema, Error: issue})
}

type serveDeliveryPlanHandle interface {
	Close()
	Verify() error
}

type serveDeliveryPlanSnapshot struct {
	Path     string
	Relative string
	Raw      []byte
	Identity string
	handle   serveDeliveryPlanHandle
}

func (snapshot *serveDeliveryPlanSnapshot) Close() {
	if snapshot != nil && snapshot.handle != nil {
		snapshot.handle.Close()
		snapshot.handle = nil
	}
}

func (snapshot *serveDeliveryPlanSnapshot) Verify() error {
	if snapshot == nil || snapshot.handle == nil {
		return tuskerError(errorInvalidTransition, "delivery plan snapshot is no longer available")
	}
	return snapshot.handle.Verify()
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
	loaded, err := loadRegisteredProjects(s.store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil {
		return nil, err
	}
	projects := loadedRegisteredProjects(loaded)
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

func (s *serveServer) handleDeliveryPlans(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForSnapshot(strings.TrimSpace(r.URL.Query().Get("project")))
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
	plans, err := serveDeliveryPlans(project)
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
	serveJSON(w, http.StatusOK, serveDeliveryPlanList{Schema: serveDeliveryPlanListSchema, ReadOnly: true, Plans: plans})
}

func serveDeliveryPlans(project RegisteredProject) ([]serveDeliveryPlanSummary, error) {
	root := filepath.Join(project.RepoRoot, "docs", "plans")
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return []serveDeliveryPlanSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, tuskerError(errorInvalidArg, "delivery plans path is not a directory", withPath(root))
	}
	plans := []serveDeliveryPlanSummary{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		raw, readErr := serveReadDeliveryPlan(path)
		if readErr != nil {
			plans = append(plans, serveDeliveryPlanSummary{Path: filepath.ToSlash(relativeDeliveryPlanPath(project.RepoRoot, path)), Title: strings.TrimSuffix(entry.Name(), ext), SpecRefs: []string{}, Tasks: []serveDeliveryPlanTaskSummary{}, State: "invalid", Issue: readErr.Error()})
			return nil
		}
		schema, schemaErr := deliveryPlanSchemaBytes(raw)
		if schemaErr != nil {
			if bytes.Contains(raw, []byte(deliveryPlanV2Schema)) {
				plans = append(plans, serveDeliveryPlanSummary{Path: filepath.ToSlash(relativeDeliveryPlanPath(project.RepoRoot, path)), Title: strings.TrimSuffix(entry.Name(), ext), SpecRefs: []string{}, Tasks: []serveDeliveryPlanTaskSummary{}, State: "invalid", Issue: schemaErr.Error()})
			}
			return nil
		}
		if schema != deliveryPlanV2Schema {
			return nil
		}
		relative, relErr := filepath.Rel(project.RepoRoot, path)
		if relErr != nil {
			return relErr
		}
		plan, _, planErr := readDeliveryReviewPlanBytes(project.VaultRoot, raw)
		if planErr != nil {
			plans = append(plans, serveDeliveryPlanSummary{
				Path: filepath.ToSlash(relative), Title: strings.TrimSuffix(entry.Name(), ext), SpecRefs: []string{}, Tasks: []serveDeliveryPlanTaskSummary{}, State: "invalid", Issue: planErr.Error(),
			})
			return nil
		}
		summary := serveDeliveryPlanSummary{Path: filepath.ToSlash(relative), Title: plan.Title, SpecRefs: append([]string(nil), plan.SpecRefs...), TaskCount: len(plan.Tasks), ExpectedConcurrency: deliveryExpectedConcurrency(plan, nil), RunnerProfile: plan.RunnerProfile, Tasks: []serveDeliveryPlanTaskSummary{}, State: "available"}
		if plan.v2 != nil {
			summary.Summary = plan.v2.Summary
		}
		for _, task := range plan.Tasks {
			summary.Tasks = append(summary.Tasks, serveDeliveryPlanTaskSummary{SourceKey: task.SourceKey, Title: task.Title, RunnerProfile: firstNonEmpty(task.RunnerProfile, plan.RunnerProfile), Complexity: task.Complexity, Risk: task.Risk})
		}
		plans = append(plans, summary)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].Path < plans[j].Path })
	return plans, nil
}

func relativeDeliveryPlanPath(repoRoot, path string) string {
	relative, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return filepath.Base(path)
	}
	return relative
}

func serveReadDeliveryPlan(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, serveDeliveryPlanMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > serveDeliveryPlanMaxBytes {
		return nil, tuskerError(errorInvalidArg, "delivery plan exceeds the 1 MiB inbox limit", withPath(path))
	}
	return raw, nil
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
		BeforeImportCommit: func() {
			if serveDeliveryPlanPhaseHook != nil {
				serveDeliveryPlanPhaseHook("commit")
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
