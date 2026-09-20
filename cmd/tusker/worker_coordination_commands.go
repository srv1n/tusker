package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const workerVisibilityIntervalDefault = 10 * time.Minute

func workerIdentityFromArgs(args Args) (WorkerAttemptIdentity, error) {
	generation, _ := strconv.Atoi(strings.TrimSpace(args.String("generation")))
	revision, _ := strconv.Atoi(strings.TrimSpace(args.String("revision")))
	identity := WorkerAttemptIdentity{
		ProjectID:         strings.TrimSpace(args.String("project")),
		TaskID:            strings.TrimSpace(args.String("task")),
		AttemptID:         strings.TrimSpace(args.String("attempt")),
		Provider:          strings.ToLower(strings.TrimSpace(args.String("provider"))),
		NativeSessionID:   strings.TrimSpace(args.String("session")),
		WorkRevision:      revision,
		AttemptGeneration: generation,
	}
	return identity, identity.validate()
}

func workerVisibilityInterval(args Args) (time.Duration, error) {
	raw := strings.TrimSpace(args.String("visibility-interval"))
	if raw == "" {
		return workerVisibilityIntervalDefault, nil
	}
	interval, err := time.ParseDuration(raw)
	if err != nil || interval <= 0 {
		return 0, tuskerError(errorInvalidArg, "--visibility-interval must be a positive Go duration")
	}
	return interval, nil
}

func workerCmd(args Args, sub string) error {
	switch sub {
	case "checkpoint":
		return workerCheckpointCmd(args)
	case "status":
		return workerStatusCmd(args)
	case "message":
		return workerMessageCmd(args)
	case "reconcile":
		return workerReconcileCmd(args)
	case "qualify":
		return workerQualifyCmd(args)
	default:
		printWorkerHelp()
		return nil
	}
}

func workerCheckpointCmd(args Args) error {
	identity, err := workerIdentityFromArgs(args)
	if err != nil {
		return err
	}
	kind := strings.TrimSpace(args.String("kind"))
	if !validWorkerCoordinationKind(kind) {
		return tuskerError(errorMissingArg, "worker checkpoint requires --kind progress|question|blocked|completed")
	}
	var evidence []string
	for _, item := range strings.Split(args.String("evidence"), ",") {
		if item = strings.TrimSpace(item); item != "" {
			evidence = append(evidence, item)
		}
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	event, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{
		Identity:      identity,
		Kind:          kind,
		Milestone:     strings.TrimSpace(args.String("milestone")),
		Status:        strings.TrimSpace(args.String("status")),
		NextMilestone: strings.TrimSpace(args.String("next-milestone")),
		Evidence:      evidence,
		OccurredAt:    workerNow(),
	})
	if err != nil {
		return err
	}
	emitJSON(map[string]any{"ok": true, "event": event})
	return nil
}

func workerStatusCmd(args Args) error {
	identity, err := workerIdentityFromArgs(args)
	if err != nil {
		return err
	}
	interval, err := workerVisibilityInterval(args)
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	attention, events, deliveries, err := store.WorkerAttemptStatus(identity, time.Now().UTC(), interval)
	if err != nil {
		return err
	}
	emitJSON(map[string]any{"ok": true, "attention": attention, "events": events, "deliveries": deliveries})
	return nil
}

func workerMessageCmd(args Args) error {
	identity, err := workerIdentityFromArgs(args)
	if err != nil {
		return err
	}
	kind := strings.TrimSpace(args.String("kind"))
	if !validWorkerDeliveryKind(kind) {
		return tuskerError(errorMissingArg, "worker message requires --kind question|instruction|answer")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	delivery, duplicate, err := store.PutWorkerDelivery(WorkerDelivery{
		Identity:       identity,
		Kind:           kind,
		Body:           args.String("body"),
		IdempotencyKey: strings.TrimSpace(args.String("key")),
	})
	if err != nil {
		return err
	}
	if duplicate && delivery.State != "stored" {
		emitJSON(map[string]any{"ok": true, "duplicate": true, "delivery": delivery})
		return nil
	}
	switch identity.Provider {
	case "devin":
		client, err := NewDevinSessionsClient()
		if err != nil {
			return err
		}
		if err := SendDevinWorkerDelivery(context.Background(), store, delivery.DeliveryID, client); err != nil {
			return err
		}
	case "muse":
		qualification, found, err := store.workerProviderQualification("muse")
		if err != nil {
			return err
		}
		if !found || !qualification.Capabilities.DeliverActive {
			if markErr := store.markWorkerDeliveryState(delivery.DeliveryID, map[string]bool{"stored": true}, "unsupported", "muse active delivery is not qualified"); markErr != nil {
				return markErr
			}
			return tuskerError(errorInvalidTransition, "muse active delivery is not proven qualified; delivery stored as unsupported")
		}
		if err := SendMuseWorkerDelivery(context.Background(), store, delivery.DeliveryID, museCLICommandRunner); err != nil {
			return err
		}
	default:
		if markErr := store.markWorkerDeliveryState(delivery.DeliveryID, map[string]bool{"stored": true}, "unsupported", "provider "+identity.Provider+" has no delivery transport"); markErr != nil {
			return markErr
		}
		return tuskerError(errorInvalidTransition, "provider "+identity.Provider+" has no delivery transport")
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	emitJSON(map[string]any{"ok": true, "duplicate": duplicate, "delivery": stored})
	return nil
}

func workerReconcileCmd(args Args) error {
	identity, err := workerIdentityFromArgs(args)
	if err != nil {
		return err
	}
	interval, err := workerVisibilityInterval(args)
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	switch identity.Provider {
	case "devin":
		client, err := NewDevinSessionsClient()
		if err != nil {
			return err
		}
		if err := ReconcileDevinWorker(context.Background(), store, identity, client, time.Now().UTC(), interval); err != nil {
			return err
		}
	case "muse":
		qualification, found, err := store.workerProviderQualification("muse")
		if err != nil {
			return err
		}
		if !found || !qualification.Capabilities.ObserveTurn {
			return tuskerError(errorInvalidTransition, "muse reconcile is not proven qualified; failing closed")
		}
		if err := ReconcileMuseWorker(context.Background(), store, identity, museCLICommandRunner, time.Now().UTC(), interval); err != nil {
			return err
		}
	default:
		return tuskerError(errorInvalidArg, "worker reconcile is unsupported for provider "+identity.Provider)
	}
	attention, events, deliveries, err := store.WorkerAttemptStatus(identity, time.Now().UTC(), interval)
	if err != nil {
		return err
	}
	emitJSON(map[string]any{"ok": true, "attention": attention, "events": events, "deliveries": deliveries})
	return nil
}

func workerQualifyCmd(args Args) error {
	provider := strings.TrimSpace(firstNonEmpty(args.String("provider"), args.String("_pos0")))
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	switch provider {
	case "muse":
		sessionID := strings.TrimSpace(args.String("session"))
		if sessionID == "" {
			return tuskerError(errorMissingArg, "worker qualify muse requires --session <exact session id>")
		}
		qualification, err := QualifyMuseEndpoint(context.Background(), museCLICommandRunner, sessionID)
		if err != nil {
			return err
		}
		if err := store.SaveWorkerProviderQualification(qualification); err != nil {
			return err
		}
		emitJSON(map[string]any{"ok": true, "qualification": qualification})
		return nil
	case "devin":
		sessionID := strings.TrimSpace(args.String("session"))
		if !validDevinSessionID(sessionID) {
			return tuskerError(errorMissingArg, "worker qualify devin requires --session <devin-...>")
		}
		client, err := NewDevinSessionsClient()
		if err != nil {
			return err
		}
		session, err := client.GetSession(context.Background(), sessionID)
		if err != nil {
			return tuskerError(errorInvalidTransition, "devin authenticated exact-session probe failed")
		}
		qualification := WorkerProviderQualification{
			Provider: "devin", Endpoint: client.BaseURL + "/v3/organizations/" + client.OrgID + "/sessions", Version: "v3",
			QualifiedAt: workerNow(),
		}
		qualification.Capabilities.Endpoint = qualification.Endpoint
		qualification.Capabilities.Version = "v3"
		qualification.Capabilities.AttachExternal = true
		qualification.Capabilities.ObserveTurn = true
		qualification.Capabilities.Evidence = "authenticated exact-session probe of " + session.SessionID + "; send/reply capabilities not live-qualified"
		if args.Bool("live") {
			identity, idErr := workerIdentityFromArgs(args)
			if idErr != nil {
				return tuskerError(errorMissingArg, "worker qualify devin --live requires a full current identity (--project --task --attempt --generation --revision --provider devin --session)")
			}
			ctx := context.Background()
			interval, err := workerVisibilityInterval(args)
			if err != nil {
				return err
			}
			delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{
				Identity:       identity,
				Kind:           "question",
				Body:           "tusker live qualification probe; reply with ok",
				IdempotencyKey: "qualify-devin-" + newRecordID(),
			})
			if err != nil {
				return err
			}
			if err := SendDevinWorkerDelivery(ctx, store, delivery.DeliveryID, client); err != nil {
				return err
			}
			stored, _, err := store.WorkerDelivery(delivery.DeliveryID)
			if err != nil {
				return err
			}
			if stored.State == "accepted" {
				qualification.Capabilities.DeliverActive = true
				qualification.Capabilities.ResumeIdle = true
			}
			if err := ReconcileDevinWorker(ctx, store, identity, client, time.Now().UTC(), interval); err != nil {
				return err
			}
			stored, _, _ = store.WorkerDelivery(delivery.DeliveryID)
			if stored.CorrelatedReplyEventID != "" {
				qualification.Capabilities.CaptureReply = true
				qualification.Capabilities.ReconcileDelivery = true
			}
			qualification.Capabilities.Evidence = "live-qualified via tagged delivery " + delivery.DeliveryID + " state=" + stored.State
		}
		if err := store.SaveWorkerProviderQualification(qualification); err != nil {
			return err
		}
		emitJSON(map[string]any{"ok": true, "qualification": qualification})
		return nil
	default:
		return tuskerError(errorInvalidArg, "worker qualify requires provider muse or devin")
	}
}

func printWorkerHelp() {
	fmt.Println(`Usage:
  tusker worker checkpoint --project <id> --task <id> --attempt <id> --generation <n> --revision <n> --provider <name> --session <native-id> --kind progress|question|blocked|completed [--milestone <m>] [--status <s>] [--next-milestone <m>] [--evidence a,b]
  tusker worker status --project <id> --task <id> --attempt <id> --generation <n> --revision <n> --provider <name> --session <native-id> [--visibility-interval 10m]
  tusker worker message --project <id> --task <id> --attempt <id> --generation <n> --revision <n> --provider <name> --session <native-id> --kind question|instruction|answer --body <text> --key <idempotency-key>
  tusker worker reconcile --project <id> --task <id> --attempt <id> --generation <n> --revision <n> --provider devin --session <devin-id> [--visibility-interval 10m]
  tusker worker qualify muse --session <exact session id>
  tusker worker qualify devin --session <devin-id>

Purpose:
  Record worker checkpoints, inspect bounded attention, and send or reconcile
  deliveries against the exact fenced attempt identity. Stale evidence is
  stored but never mutates the current attempt, lease, or task.`)
}
