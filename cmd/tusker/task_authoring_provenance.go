package main

// Task authoring provenance is deliberately a small, capture-once projection
// of the host conversation that produced a task.  It is not an execution
// record and it is not a contact: the execution ledger and agent_contacts
// remain authoritative for routable endpoints.

import (
	"errors"
	"os"
	"strings"
	"time"
)

const taskAuthoringProvenanceKey = "authoring_provenance"

const (
	taskAuthoringBindingBound   = "bound"
	taskAuthoringBindingUnbound = "unbound"
)

// TaskAuthoringContext is supplied by the host environment. A native
// conversation identifier is useful host-reported provenance, but by itself
// is never an execution/contact binding or an authenticated identity.
type TaskAuthoringContext struct {
	Source         string `json:"source"`
	ConversationID string `json:"conversation_id"`
	Host           string `json:"host"`
}

// TaskAuthoringProvenance is the durable task-data projection.  BindingState
// starts unbound because importing or authoring a task does not register a
// live execution endpoint.
type TaskAuthoringProvenance struct {
	Source         string `json:"source"`
	ConversationID string `json:"conversation_id,omitempty"`
	Host           string `json:"host,omitempty"`
	CapturedAt     string `json:"captured_at"`
	BindingState   string `json:"binding_state"`
}

// TaskAuthoringIdentity combines the immutable authoring projection with the
// effective contacts. It is a read-only view assembled from task data and
// the existing runtime contact/execution authorities.
type TaskAuthoringIdentity struct {
	Provenance *TaskAuthoringProvenance `json:"provenance,omitempty"`
	Contacts   []AgentContactBinding    `json:"contacts,omitempty"`
}

// TaskAuthoringContextFromEnvironment reads only the existing host markers.
// CODEX_SESSION_ID is the native conversation/session identity provided by
// Codex Desktop; CODEX_THREAD_ID remains a useful fallback for test/CLI
// hosts.  No identifier is derived from a task or execution id.
func TaskAuthoringContextFromEnvironment() TaskAuthoringContext {
	// A dispatched worker may inherit provider environment markers, but it is
	// not the task authoring host. Never project its inherited identity into a
	// new task.
	if strings.TrimSpace(os.Getenv("TUSKER_ATTEMPT_ID")) != "" {
		return TaskAuthoringContext{}
	}
	source, conversationID := "", ""
	if value := strings.TrimSpace(os.Getenv("CODEX_SESSION_ID")); value != "" {
		source, conversationID = "codex", value
	} else if value := strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")); value != "" {
		source, conversationID = "codex", value
	} else if value := firstNonEmpty(strings.TrimSpace(os.Getenv("CLAUDE_CODE_SESSION_ID")), strings.TrimSpace(os.Getenv("CLAUDE_SESSION_ID"))); value != "" {
		source, conversationID = "claude", value
	} else if strings.TrimSpace(os.Getenv("CLAUDECODE")) != "" || strings.TrimSpace(os.Getenv("CLAUDE_CODE_ENTRYPOINT")) != "" {
		source = "claude"
	} else if strings.TrimSpace(os.Getenv("CHISEL_SESSION_DB")) != "" {
		// Devin exports only the session database path, not a per-process
		// session identifier. cwd/recency resolution against that database is
		// not an identity boundary (two sessions can share a directory), so a
		// Devin session is classified but never carries a native conversation
		// id: --current-workspace and same-conversation review fencing fail
		// closed until Devin exports an immutable id (e.g. DEVIN_SESSION_ID).
		source = "devin"
	}
	if source == "" {
		return TaskAuthoringContext{}
	}
	// The CLI is currently local-only.  The host override lets a future
	// connected host provide its own stable host identity without making one
	// up from a provider/session id.
	host := firstNonEmpty(strings.TrimSpace(os.Getenv("TUSKER_HOST_ID")), strings.TrimSpace(os.Getenv("CODEX_HOST_ID")), "local")
	return TaskAuthoringContext{Source: source, ConversationID: conversationID, Host: host}
}

// CaptureTaskAuthoringProvenance adds the provenance projection once.  It is
// safe for retry/import: an existing projection is validated and preserved
// byte-for-byte in the task map. A later different host context is treated
// as an importer, never as a replacement author.
func CaptureTaskAuthoringProvenance(data map[string]any, context TaskAuthoringContext) error {
	if data == nil {
		return errors.New("task authoring provenance requires task data")
	}
	if raw, exists := data[taskAuthoringProvenanceKey]; exists && raw != nil {
		// Validate the existing object, but never overwrite it. Imports and
		// retries may run in a different conversation; that conversation is
		// not the original author and must not replace the captured fact.
		if _, err := taskAuthoringProvenanceFromValue(raw); err != nil {
			return err
		}
		return nil
	}

	context = normalizeTaskAuthoringContext(context)
	provenance := TaskAuthoringProvenance{Source: context.Source, ConversationID: context.ConversationID, Host: context.Host, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano), BindingState: taskAuthoringBindingUnbound}
	data[taskAuthoringProvenanceKey] = map[string]any{
		"source":          provenance.Source,
		"conversation_id": provenance.ConversationID,
		"host":            provenance.Host,
		"captured_at":     provenance.CapturedAt,
		"binding_state":   provenance.BindingState,
	}
	return nil
}

// CaptureTaskAuthoringProvenanceFromEnvironment is the authoring/import hook
// for callers that already run inside a Codex/Claude host. Environment values
// remain descriptive host-reported provenance; they do not authenticate a
// caller or grant contact delivery authority.
func CaptureTaskAuthoringProvenanceFromEnvironment(data map[string]any) error {
	return CaptureTaskAuthoringProvenance(data, TaskAuthoringContextFromEnvironment())
}

// TaskAuthoringProvenanceFromTask returns the existing projection without
// changing it.  The bool distinguishes an absent optional projection from a
// malformed one, so projections can report unknown/unbound honestly.
func TaskAuthoringProvenanceFromTask(data map[string]any) (TaskAuthoringProvenance, bool, error) {
	if data == nil {
		return TaskAuthoringProvenance{}, false, errors.New("task authoring provenance requires task data")
	}
	raw, ok := data[taskAuthoringProvenanceKey]
	if !ok || raw == nil {
		return TaskAuthoringProvenance{}, false, nil
	}
	value, err := taskAuthoringProvenanceFromValue(raw)
	return value, true, err
}

// TaskAuthoringIdentityForTask projects provenance and contact binding state
// for an inspector or packet. Authored contacts without a runtime contact
// row remain visibly unbound; the fallback is shown for diagnosis but is not
// upgraded into an operational registration.
func TaskAuthoringIdentityForTask(store *RuntimeStore, projectID string, task Note) (TaskAuthoringIdentity, error) {
	identity := TaskAuthoringIdentity{}
	if provenance, ok, err := TaskAuthoringProvenanceFromTask(task.Data); err != nil {
		return identity, err
	} else if ok {
		identity.Provenance = &provenance
	}
	taskID := stringField(task.Data, "id")
	if projectID == "" {
		projectID = stringField(task.Data, "project")
	}
	if store == nil {
		for _, contact := range authoredAgentContacts(task) {
			identity.Contacts = append(identity.Contacts, AgentContactBinding{Contact: contact, State: AgentContactBindingUnbound, Reason: "authored contact is not registered"})
		}
		return identity, nil
	}
	contacts, err := store.AgentContacts(projectID, taskID)
	if err != nil {
		return identity, err
	}
	registered := map[string]bool{}
	for _, contact := range contacts {
		binding, resolveErr := store.ResolveAgentContactBinding(projectID, taskID, contact.Role, contact.Name)
		if resolveErr != nil {
			return identity, resolveErr
		}
		identity.Contacts = append(identity.Contacts, binding)
		registered[contact.Role+"\x00"+contact.Name] = true
	}
	for _, contact := range authoredAgentContacts(task) {
		if !registered[contact.Role+"\x00"+contact.Name] {
			identity.Contacts = append(identity.Contacts, AgentContactBinding{Contact: contact, State: AgentContactBindingUnbound, Reason: "authored contact is not registered"})
		}
	}
	return identity, nil
}

// SelfImplementationTrigger is stored in the existing run authorization
// trigger column. It records the current-conversation claim without adding a
// second identity table or pretending that a native conversation is a live
// provider execution.
func SelfImplementationTrigger(context TaskAuthoringContext) string {
	context = normalizeTaskAuthoringContext(context)
	parts := []string{"self_implementation", "source=" + context.Source}
	if context.ConversationID != "" {
		parts = append(parts, "conversation="+context.ConversationID)
	}
	if context.Host != "" {
		parts = append(parts, "host="+context.Host)
	}
	return strings.Join(parts, ";")
}

func selfImplementationTriggerContext(trigger string) TaskAuthoringContext {
	parts := strings.Split(strings.TrimSpace(trigger), ";")
	if len(parts) == 0 || parts[0] != "self_implementation" {
		return TaskAuthoringContext{}
	}
	values := map[string]string{}
	for _, part := range parts[1:] {
		key, value, ok := strings.Cut(part, "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return TaskAuthoringContext{Source: values["source"], ConversationID: values["conversation"], Host: values["host"]}
}

func nativeConversationKnown(context TaskAuthoringContext) bool {
	context = normalizeTaskAuthoringContext(context)
	return (context.Source == "codex" || context.Source == "claude" || context.Source == "devin") && context.ConversationID != ""
}

// SameAuthoringConversation reports whether an implementation self claim and
// a reviewer are the same known native conversation. Unknown legacy/session
// values remain compatible and return false.
func SameAuthoringConversation(implementationTrigger string, reviewer TaskAuthoringContext) bool {
	implementation := selfImplementationTriggerContext(implementationTrigger)
	if implementation.ConversationID == "" || strings.TrimSpace(reviewer.ConversationID) == "" {
		return false
	}
	if implementation.Source != "" && strings.TrimSpace(reviewer.Source) != "" && implementation.Source != strings.ToLower(strings.TrimSpace(reviewer.Source)) {
		return false
	}
	if implementation.Host != "" && strings.TrimSpace(reviewer.Host) != "" && implementation.Host != strings.TrimSpace(reviewer.Host) {
		return false
	}
	return implementation.ConversationID == strings.TrimSpace(reviewer.ConversationID)
}

func normalizeTaskAuthoringContext(context TaskAuthoringContext) TaskAuthoringContext {
	context.Source = strings.ToLower(strings.TrimSpace(context.Source))
	context.ConversationID = strings.TrimSpace(context.ConversationID)
	context.Host = strings.TrimSpace(context.Host)
	if context.Source == "" {
		context.Source = "unknown"
	}
	if context.Host == "" && context.ConversationID != "" {
		context.Host = "local"
	}
	return context
}

func taskAuthoringProvenanceFromValue(raw any) (TaskAuthoringProvenance, error) {
	values, ok := raw.(map[string]any)
	if !ok {
		if stringsMap, stringsOK := raw.(map[string]string); stringsOK {
			values = make(map[string]any, len(stringsMap))
			for key, value := range stringsMap {
				values[key] = value
			}
			ok = true
		}
	}
	if !ok {
		return TaskAuthoringProvenance{}, errors.New("task authoring provenance must be an object")
	}
	value := TaskAuthoringProvenance{
		Source:         strings.TrimSpace(stringValue(values["source"])),
		ConversationID: strings.TrimSpace(stringValue(values["conversation_id"])),
		Host:           strings.TrimSpace(stringValue(values["host"])),
		CapturedAt:     strings.TrimSpace(stringValue(values["captured_at"])),
		BindingState:   strings.TrimSpace(stringValue(values["binding_state"])),
	}
	if value.Source == "" || value.CapturedAt == "" {
		return TaskAuthoringProvenance{}, errors.New("task authoring provenance requires source and captured_at")
	}
	if value.BindingState != taskAuthoringBindingBound && value.BindingState != taskAuthoringBindingUnbound {
		return TaskAuthoringProvenance{}, errors.New("task authoring provenance binding_state must be bound or unbound")
	}
	return value, nil
}
