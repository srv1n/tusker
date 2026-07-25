package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const serveDeliveryErrorSchema = "tusker.serve-delivery-error/v1"

// The production value is the canonical transaction. Tests substitute only its
// environment inspector so they can prove HTTP replay behavior without a daemon.
var serveDeliveryStartFn = func(args Args) (deliveryStartResult, error) { return deliveryStart(args, nil) }

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

// serveDeliveryPlanPath accepts only an existing, repo-relative file. EvalSymlinks
// is deliberate: lexical cleaning alone would let plans/inside.yaml escape via a
// symlink. The CLI retains its broader local-path contract; Serve is narrower.
func serveDeliveryPlanPath(project RegisteredProject, raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", tuskerError(errorMissingArg, "delivery review requires a repo-relative plan path")
	}
	if filepath.IsAbs(path) {
		return "", tuskerError(errorInvalidArg, "delivery plan path must be relative to the repository", withPath(path))
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", tuskerError(errorInvalidArg, "delivery plan path must stay inside the repository", withPath(path))
	}
	repoRoot, err := filepath.EvalSymlinks(project.RepoRoot)
	if err != nil {
		return "", tuskerError(errorInvalidArg, "cannot resolve repository root for delivery plan", withPath(project.RepoRoot))
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(repoRoot, clean))
	if err != nil {
		if os.IsNotExist(err) {
			return "", tuskerError(errorNotFound, "delivery plan does not exist", withPath(clean))
		}
		return "", tuskerError(errorInvalidArg, "cannot resolve delivery plan path", withPath(clean))
	}
	rel, err := filepath.Rel(repoRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", tuskerError(errorInvalidArg, "delivery plan path escapes the repository", withPath(clean))
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", tuskerError(errorInvalidArg, "delivery plan path must name a file", withPath(clean))
	}
	return resolved, nil
}

func (s *serveServer) serveDeliveryPlanPath(project RegisteredProject, raw string) (string, error) {
	resolved, err := serveDeliveryPlanPath(project, raw)
	if err != nil {
		return "", err
	}
	projects, err := s.store.ListProjects()
	if err != nil {
		return "", err
	}
	selectedRepo := canonicalProjectPath(project.RepoRoot)
	for _, other := range projects {
		if serveSameRegisteredProject(project, other) {
			continue
		}
		for _, root := range []string{other.RepoRoot, other.VaultRoot} {
			otherRoot := canonicalProjectPath(root)
			if otherRoot == "" || !pathWithin(selectedRepo, otherRoot) || sameCanonicalProjectPath(selectedRepo, otherRoot) {
				continue
			}
			if pathWithin(otherRoot, resolved) {
				return "", tuskerError(
					errorInvalidArg,
					"delivery plan belongs to a different registered nested project",
					withPath(raw),
					withHint("select project "+other.ProjectID+" before reviewing or starting this plan"),
					withContext(map[string]any{"selectedProject": project.ProjectID, "nestedProject": other.ProjectID}),
				)
			}
		}
	}
	return resolved, nil
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
	planPath, err := s.serveDeliveryPlanPath(project, r.URL.Query().Get("plan"))
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
	if schema, schemaErr := deliveryPlanSchemaAt(planPath); schemaErr != nil {
		serveDeliveryFailure(w, schemaErr)
		return
	} else if schema != deliveryPlanV2Schema {
		serveDeliveryFailure(w, tuskerError(errorInvalidArg, "Serve delivery review requires a tusker.delivery-plan/v2 plan"))
		return
	}
	review, err := buildDeliveryReview(project.VaultRoot, planPath)
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
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
	planPath, err := s.serveDeliveryPlanPath(project, body.string("plan"))
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
	args["plan"] = planPath
	args["confirm"] = body.string("confirm", "planFingerprint", "plan_fingerprint")
	args["by"] = firstNonEmpty(body.string("actor", "by"), serveOperatorActor())
	result, err := serveDeliveryStartFn(args)
	if err != nil {
		serveDeliveryFailure(w, err)
		return
	}
	// Start itself is the canonical import + preflight + arm transaction. This
	// adapter never changes automation, daemon, runner, release, or gate state.
	s.invalidateProjectSnapshot(project.ProjectID)
	serveJSON(w, http.StatusOK, result)
}
