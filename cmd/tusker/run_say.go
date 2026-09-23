package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func runsSayCmd(args Args) error {
	return runOperatorCommand(args, true)
}

func runsContinueCmd(args Args) error {
	return runOperatorCommand(args, false)
}

func runOperatorCommand(args Args, say bool) error {
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	actor := strings.TrimSpace(args.String("by"))
	if actor == "" {
		return tuskerError(errorMissingArg, "runs say and continue require --by <actor>")
	}
	message, err := runOperatorMessage(args, say)
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := findRunScopedOrAmbiguous(store, args.String("project"), id)
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "run not found: "+id)
	}
	project, task, wave, err := runSayContext(store, *run)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if say {
		result, err := sayRuntimeRun(store, DefaultStateRoot(), project, task, wave, *run, actor, message, args.String("key"), now)
		if err != nil {
			return err
		}
		if args.Bool("json") {
			emitJSON(result)
		} else {
			fmt.Printf("Say %s for %s: %s\n", result.Route, id, result.Continuation.Reason)
		}
		return nil
	}
	result, err := continueRuntimeRun(store, project, task, wave, *run, actor, message, now)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(result)
	} else {
		fmt.Printf("Continue %s: %s\n", id, result.Continuation.Reason)
	}
	return nil
}

// runOperatorMessage keeps CLI input handling identical for Say and Continue.
// A file named "-" reads stdin, as with the other file-backed CLI inputs.
func runOperatorMessage(args Args, required bool) (string, error) {
	message, file := args.String("message"), args.String("message-file")
	_, hasMessage := args["message"]
	_, hasFile := args["message-file"]
	if hasMessage && hasFile {
		return "", tuskerError(errorInvalidArg, "use --message or --message-file, not both")
	}
	if file != "" {
		var input io.Reader = os.Stdin
		var err error
		if file != "-" {
			var opened *os.File
			opened, err = os.Open(file)
			if err != nil {
				return "", err
			}
			defer opened.Close()
			input = opened
		}
		data, err := io.ReadAll(io.LimitReader(input, workerDeliveryBodyLimit+1))
		if err != nil {
			return "", err
		}
		message = string(data)
	}
	if required && strings.TrimSpace(message) == "" {
		return "", tuskerError(errorMissingArg, "runs say requires --message or --message-file")
	}
	if message != "" && strings.TrimSpace(message) == "" {
		return "", tuskerError(errorInvalidArg, "operator message must contain text")
	}
	if len(message) > workerDeliveryBodyLimit {
		return "", tuskerError(errorInvalidArg, "operator message exceeds the 32KiB bound")
	}
	return message, nil
}

type runSayResult struct {
	Delivery      WorkerDelivery      `json:"delivery"`
	Route         string              `json:"route"`
	OperatorState runOperatorState    `json:"operator_state"`
	Continuation  serveRecoveryResult `json:"continuation"`
	Duplicate     bool                `json:"duplicate"`
}

func sayRuntimeRun(store *RuntimeStore, stateRoot string, project RegisteredProject, task, wave Note, run RunStatus, actor, body, key string, now time.Time) (runSayResult, error) {
	if nativeResumeRunnerCapabilities(RunnerName(run.Runner)).SoftSay {
		soft, fallback, err := runSaySoft(store, run, actor, body, key, now)
		if err != nil || !fallback {
			return soft, err
		}
		return runSayHardStored(store, stateRoot, project, task, wave, run, actor, body, key, now, &soft.Delivery)
	}
	return runSayHard(store, stateRoot, project, task, wave, run, actor, body, key, now)
}

func continueRuntimeRun(store *RuntimeStore, project RegisteredProject, task, wave Note, run RunStatus, actor, message string, now time.Time) (runSayResult, error) {
	result := runSayResult{Route: "continue"}
	current, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil {
		return result, err
	}
	if current == nil {
		return result, tuskerError(errorNotFound, "run not found")
	}
	run = *current
	_, err, reason := nativeContinuationPreflight(store, project, wave, run)
	if err != nil {
		return result, err
	}
	if reason != "" && reason != "native continuation is already queued" {
		return result, tuskerError(errorInvalidTransition, reason)
	}
	state, err := runOperatorStateForRun(store, run, now)
	if err != nil {
		return result, err
	}
	result.OperatorState = state
	if message != "" {
		identity, err := runContinuationIdentity(store, run)
		if err != nil {
			return result, err
		}
		if identity == nil {
			return result, tuskerError(errorInvalidTransition, "runs continue message requires a retained native session identity")
		}
		key := "continue:" + strings.TrimPrefix(runSayIdempotencyKey(actor, message, *identity), "say:")
		result.Delivery, result.Duplicate, err = store.PutWorkerDelivery(WorkerDelivery{Identity: *identity, Kind: "instruction", Body: message, IdempotencyKey: key})
		if err != nil {
			return result, err
		}
	}
	options := nativeContinuationOptions{FailureSummary: runContinuationFailureSummary(run)}
	if result.Delivery.DeliveryID != "" {
		options.DeliveryIDs = []string{result.Delivery.DeliveryID}
	}
	result.Continuation, err = queueNativeSessionContinuation(store, project, task, wave, run, actor, now, options)
	if err != nil {
		return result, err
	}
	if result.Continuation.Refused {
		return result, tuskerError(errorInvalidTransition, result.Continuation.Reason)
	}
	return result, nil
}

func runContinuationFailureSummary(run RunStatus) string {
	if run.ReasonCode == "" && run.LastError == "" {
		return ""
	}
	var lines []string
	if run.ReasonCode != "" {
		lines = append(lines, "Code: "+run.ReasonCode)
		if spec, ok := runFailureReason(RunFailureReasonCode(run.ReasonCode)); ok {
			lines = append(lines, "Guidance: "+spec.Guidance)
		}
	}
	if run.LastError != "" {
		lines = append(lines, "Last error: "+run.LastError)
	}
	if event := latestJSONLEvent(run.EventSinkPath); event != nil {
		if raw, err := json.Marshal(event); err == nil {
			lines = append(lines, "Last event: "+string(raw))
		}
	}
	return strings.Join(lines, "\n")
}

func runSayHard(store *RuntimeStore, stateRoot string, project RegisteredProject, task, wave Note, run RunStatus, actor, body, key string, now time.Time) (runSayResult, error) {
	return runSayHardStored(store, stateRoot, project, task, wave, run, actor, body, key, now, nil)
}

func runSayHardStored(store *RuntimeStore, stateRoot string, project RegisteredProject, task, wave Note, run RunStatus, actor, body, key string, now time.Time, stored *WorkerDelivery) (runSayResult, error) {
	result := runSayResult{Route: "hard"}
	if stored == nil {
		prior, err := runSayPriorDelivery(store, run, actor, body, key)
		if err != nil {
			return result, err
		}
		if prior != nil {
			result.Delivery, result.Duplicate = *prior, true
			result.OperatorState, err = runOperatorStateForRun(store, run, now)
			if err != nil || prior.State != "stored" {
				return result, err
			}
			current, err := store.FindRunScoped(run.ProjectID, run.RecordID)
			if err != nil {
				return result, err
			}
			if current == nil {
				return result, tuskerError(errorNotFound, "run disappeared before Say retry")
			}
			if current.ActiveAttemptID == prior.Identity.AttemptID && current.LeaseGeneration == prior.Identity.AttemptGeneration {
				retried, err := runSayHardStored(store, stateRoot, project, task, wave, *current, actor, body, key, now, prior)
				retried.Duplicate = true
				return retried, err
			}
			if current.LeaseGeneration > prior.Identity.AttemptGeneration && prior.State == "stored" {
				return result, tuskerError(errorInvalidTransition, "operator message remains stored for the next native continuation; the child attempt was already claimed")
			}
			if current.LeaseGeneration == prior.Identity.AttemptGeneration && runSessionControlSettled(*current) {
				result.Continuation, err = queueNativeSessionContinuation(store, project, task, wave, *current, actor, now)
				if result.Continuation.Refused {
					return result, tuskerError(errorInvalidTransition, result.Continuation.Reason)
				}
			}
			return result, err
		}
	}
	state, err := runOperatorStateForRun(store, run, now)
	if err != nil {
		return result, err
	}
	result.OperatorState = state
	switch state.State {
	case "working", "quiet":
	case "waiting_on_you":
		return result, tuskerError(errorInvalidTransition, "run is waiting on you; use tusker message reply")
	case "blocked", "failed", "lost":
		return result, tuskerError(errorInvalidTransition, "run is "+state.State+"; use tusker runs continue --message")
	default:
		return result, tuskerError(errorInvalidTransition, "runs say requires a working or quiet run; current state is "+state.State)
	}
	identity, err := store.WorkerIdentityForRun(run)
	if err != nil {
		return result, err
	}
	if identity == nil {
		return result, tuskerError(errorInvalidTransition, "runs say requires a current native session identity")
	}
	if strings.TrimSpace(run.SessionRef) == "" {
		return result, tuskerError(errorInvalidTransition, "runs say requires a saved native session reference")
	}
	capability := nativeResumeRunnerCapabilities(RunnerName(run.Runner))
	if !capability.HardSay || !capability.ResumeSession {
		return result, tuskerError(errorInvalidTransition, "runner does not declare hard Say support")
	}
	if key == "" {
		key = runSayIdempotencyKey(actor, body, *identity)
	}
	var delivery WorkerDelivery
	duplicate := false
	if stored != nil {
		if stored.Identity != *identity || stored.Body != body {
			return result, tuskerError(errorInvalidTransition, "soft Say delivery does not match current attempt")
		}
		delivery = *stored
	} else {
		delivery, duplicate, err = store.PutWorkerDelivery(WorkerDelivery{Identity: *identity, Kind: "instruction", Body: body, IdempotencyKey: key})
		if err != nil {
			return result, err
		}
	}
	result.Delivery, result.Duplicate = delivery, duplicate
	current, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil {
		return result, err
	}
	if current == nil {
		return result, tuskerError(errorNotFound, "run disappeared before Say interrupt")
	}
	if current.LeaseGeneration != identity.AttemptGeneration {
		if current.LeaseGeneration > identity.AttemptGeneration && current.SessionRef == run.SessionRef {
			return result, tuskerError(errorInvalidTransition, "operator message remains stored for the next native continuation; the child attempt was already claimed")
		}
		_ = store.markWorkerDeliveryState(delivery.DeliveryID, map[string]bool{"stored": true}, "stale", "attempt changed before interrupt")
		return result, tuskerError(errorInvalidTransition, "attempt changed before Say could interrupt it")
	}
	if current.ActiveAttemptID == identity.AttemptID {
		_, _, err = interruptRuntimeRunScoped(stateRoot, store, run.ProjectID, run.RecordID)
		if err != nil {
			return result, err
		}
	} else if !runSessionControlSettled(*current) && current.LeaseState != string(LeaseStateRetryQueued) {
		return result, tuskerError(errorInvalidTransition, "run changed before Say could interrupt it")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		current, err = store.FindRunScoped(run.ProjectID, run.RecordID)
		if err != nil {
			return result, err
		}
		if current == nil {
			return result, tuskerError(errorNotFound, "run disappeared while waiting for Say interrupt")
		}
		if current.LeaseGeneration != identity.AttemptGeneration && current.LeaseState != string(LeaseStateRetryQueued) {
			if current.LeaseGeneration > identity.AttemptGeneration && current.SessionRef == run.SessionRef {
				return result, tuskerError(errorInvalidTransition, "operator message remains stored for the next native continuation; the child attempt was already claimed")
			}
			_ = store.markWorkerDeliveryState(delivery.DeliveryID, map[string]bool{"stored": true}, "stale", "attempt changed while interrupting")
			return result, tuskerError(errorInvalidTransition, "attempt changed while Say was interrupting")
		}
		if projectedAttemptOutcome(current.AttemptOutcome, current.LastError) == AttemptOutcomeSucceeded {
			_ = store.markWorkerDeliveryState(delivery.DeliveryID, map[string]bool{"stored": true}, "stale", "run completed before delivery")
			return result, tuskerError(errorInvalidTransition, "run completed before delivery")
		}
		if runSessionControlSettled(*current) {
			break
		}
		if time.Now().After(deadline) {
			return result, tuskerError(errorInvalidTransition, "interrupt is still settling; retry runs say or use runs continue after it settles")
		}
		time.Sleep(50 * time.Millisecond)
	}
	result.Continuation, err = queueNativeSessionContinuation(store, project, task, wave, *current, actor, now)
	if err != nil {
		return result, err
	}
	if result.Continuation.Refused {
		return result, tuskerError(errorInvalidTransition, result.Continuation.Reason)
	}
	result.OperatorState, err = runOperatorStateForRun(store, *current, time.Now().UTC())
	return result, err
}

func runSayIdempotencyKey(actor, body string, identity WorkerAttemptIdentity) string {
	digest := sha256.Sum256([]byte(actor + "\x00" + body + "\x00" + identity.AttemptID + "\x00" +
		strconv.Itoa(identity.AttemptGeneration)))
	return "say:" + hex.EncodeToString(digest[:])
}

func runSayPriorDelivery(store *RuntimeStore, run RunStatus, actor, body, key string) (*WorkerDelivery, error) {
	attemptID := run.ActiveAttemptID
	if attemptID == "" && run.SessionRef != "" {
		session, err := store.FindSessionByRef(run.ProjectID, run.SessionRef)
		if err != nil {
			return nil, err
		}
		if session != nil {
			attemptID = session.LastAttemptID
		}
	}
	if attemptID == "" {
		return nil, nil
	}
	rows, err := store.query(`SELECT delivery_id, idempotency_key, project_id, task_id, work_revision, attempt_id, attempt_generation, provider, native_session_id,
		kind, body, state, stored_at, provider_accepted_at, worker_activity_observed_at, correlated_reply_event_id, correlated_reply_at, decision_applied_at, provider_receipt_json, last_error
		FROM worker_deliveries WHERE project_id = ? AND task_id = ? AND work_revision = ? AND kind = 'instruction' ORDER BY stored_at DESC, delivery_id DESC`,
		run.ProjectID, firstNonEmpty(run.ItemID, run.RecordID), run.WorkRevision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deliveries, err := scanWorkerDeliveries(rows)
	if err != nil {
		return nil, err
	}
	for _, delivery := range deliveries {
		if delivery.Identity.AttemptID != attemptID && delivery.State != "stored" {
			continue
		}
		candidate := key
		if candidate == "" {
			candidate = runSayIdempotencyKey(actor, body, delivery.Identity)
		}
		if delivery.IdempotencyKey == candidate && delivery.Body == body {
			return &delivery, nil
		}
	}
	return nil, nil
}

func runSayContext(store *RuntimeStore, run RunStatus) (RegisteredProject, Note, Note, error) {
	projects, err := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, ProjectID: run.ProjectID})
	if err != nil {
		return RegisteredProject{}, Note{}, Note{}, err
	}
	for _, loaded := range projects {
		project := loaded.Project
		if project.ProjectID != run.ProjectID {
			continue
		}
		task, err := resolveV7Note(project.VaultRoot, run.ItemID, "task")
		if err != nil {
			return project, Note{}, Note{}, err
		}
		wave, err := resolveV7Note(project.VaultRoot, stringField(task.Data, "wave"), "wave")
		return project, task, wave, err
	}
	return RegisteredProject{}, Note{}, Note{}, tuskerError(errorNotFound, "registered project for run not found")
}

func runContinuationIdentity(store *RuntimeStore, run RunStatus) (*WorkerAttemptIdentity, error) {
	if run.SessionRef == "" || run.LeaseGeneration <= 0 || run.WorkRevision <= 0 {
		return nil, nil
	}
	session, err := store.FindSessionByRef(run.ProjectID, run.SessionRef)
	if err != nil || session == nil || session.LastAttemptID == "" {
		return nil, err
	}
	rows, err := store.query(`SELECT execution_id, provider FROM execution_records WHERE project_id = ? AND attempt_id = ? AND lease_generation = ?`,
		run.ProjectID, session.LastAttemptID, run.LeaseGeneration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type match struct{ id, provider string }
	var matches []match
	for rows.Next() {
		var item match
		if err := rows.Scan(&item.id, &item.provider); err != nil {
			return nil, err
		}
		matches = append(matches, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(matches) != 1 {
		return nil, nil
	}
	view, err := store.ExecutionView(matches[0].id)
	if err != nil || view == nil || view.ProviderSessionID == "" {
		return nil, err
	}
	provider, err := store.ExecutionProvider(matches[0].id)
	if err != nil {
		return nil, err
	}
	return &WorkerAttemptIdentity{
		ProjectID: run.ProjectID, TaskID: firstNonEmpty(run.ItemID, run.RecordID),
		AttemptID: session.LastAttemptID, AttemptGeneration: run.LeaseGeneration,
		WorkRevision: run.WorkRevision, Provider: strings.ToLower(firstNonEmpty(provider, matches[0].provider)),
		NativeSessionID: view.ProviderSessionID,
	}, nil
}

func pendingRunContinuationDeliveries(store *RuntimeStore, run RunStatus, parentAttemptID, sessionRef string) ([]WorkerDelivery, error) {
	if store == nil || parentAttemptID == "" || sessionRef == "" {
		return nil, nil
	}
	rows, err := store.query(`SELECT d.delivery_id, d.idempotency_key, d.project_id, d.task_id, d.work_revision, d.attempt_id, d.attempt_generation, d.provider, d.native_session_id,
		d.kind, d.body, d.state, d.stored_at, d.provider_accepted_at, d.worker_activity_observed_at, d.correlated_reply_event_id, d.correlated_reply_at, d.decision_applied_at, d.provider_receipt_json, d.last_error
		FROM worker_deliveries d JOIN attempts a ON a.attempt_id = d.attempt_id AND a.project_id = d.project_id AND a.record_id = ?
		WHERE d.project_id = ? AND d.task_id = ? AND d.work_revision = ? AND a.session_ref = ? AND d.kind = 'instruction' AND d.state = 'stored'
		ORDER BY d.stored_at, d.delivery_id`, run.RecordID, run.ProjectID, firstNonEmpty(run.ItemID, run.RecordID), run.WorkRevision, sessionRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkerDeliveries(rows)
}

func acceptRunContinuationDeliveries(store *RuntimeStore, deliveries []WorkerDelivery, childAttemptID string) error {
	for _, delivery := range deliveries {
		_, won, err := store.ClaimWorkerDelivery(delivery.DeliveryID)
		if err != nil {
			return err
		}
		if !won {
			continue
		}
		if err := store.MarkWorkerDeliveryAccepted(delivery.DeliveryID, map[string]any{"child_attempt_id": childAttemptID, "route": "hard"}, workerNow()); err != nil {
			return err
		}
	}
	return nil
}
