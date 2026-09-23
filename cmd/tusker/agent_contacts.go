package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type AgentContact struct {
	ProjectID     string            `json:"projectId"`
	TaskID        string            `json:"taskId"`
	Role          string            `json:"role"`
	Name          string            `json:"name,omitempty"`
	Address       AgentAddress      `json:"address"`
	Generation    int               `json:"generation"`
	PredecessorID string            `json:"predecessorId,omitempty"`
	Endpoint      map[string]string `json:"endpoint,omitempty"`
	UpdatedAt     string            `json:"updatedAt"`
}

const (
	AgentContactBindingBound   = "bound"
	AgentContactBindingUnbound = "unbound"
)

type AgentConnectorCapabilities struct {
	MessageWhileRunning bool   `json:"messageWhileRunning"`
	ContinueWhileIdle   bool   `json:"continueWhileIdle"`
	RetrieveResponse    bool   `json:"retrieveResponse"`
	AttachmentValid     bool   `json:"attachmentValid"`
	State               string `json:"state"`
	Reason              string `json:"reason,omitempty"`
}

type AgentContactBinding struct {
	Contact        AgentContact               `json:"contact"`
	State          string                     `json:"state"`
	Reason         string                     `json:"reason,omitempty"`
	Provider       string                     `json:"provider,omitempty"`
	Harness        string                     `json:"harness,omitempty"`
	ConversationID string                     `json:"conversation_id,omitempty"`
	ConnectionID   string                     `json:"connection_id,omitempty"`
	InheritedFrom  string                     `json:"inherited_from,omitempty"`
	Generation     int                        `json:"generation,omitempty"`
	Capabilities   AgentConnectorCapabilities `json:"capabilities"`
	Execution      *ExecutionView             `json:"execution,omitempty"`
}

func (c AgentContact) validate() error {
	if strings.TrimSpace(c.ProjectID) == "" || strings.TrimSpace(c.TaskID) == "" || strings.TrimSpace(c.Address.ID) == "" {
		return errors.New("contact project, task and address are required")
	}
	if c.Role != "architect" && c.Role != "origin" && c.Role != "peer" {
		return errors.New("contact role must be architect, origin or peer")
	}
	if c.Role == "peer" && strings.TrimSpace(c.Name) == "" {
		return errors.New("peer contact requires a name")
	}
	if c.Address.Kind != "task" && c.Address.Kind != "execution" {
		return errors.New("contact address must be task or execution")
	}
	return nil
}

type ExternalContactRegistrationInput struct {
	ProjectID, SubjectID, SubjectKind string
	Role, Name                        string
	Harness, Provider                 string
	ConversationID, ConnectionID      string
	Source, Actor                     string
	ExpectedGeneration                int
}

func (s *RuntimeStore) RegisterExternalAgentContact(input ExternalContactRegistrationInput) (AgentContact, ExecutionRecord, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.SubjectID = strings.TrimSpace(input.SubjectID)
	input.Role = strings.TrimSpace(input.Role)
	input.Name = strings.TrimSpace(input.Name)
	input.Harness = strings.ToLower(strings.TrimSpace(input.Harness))
	input.Provider = strings.TrimSpace(input.Provider)
	input.ConversationID = strings.TrimSpace(input.ConversationID)
	input.ConnectionID = strings.TrimSpace(input.ConnectionID)
	input.Source = strings.TrimSpace(input.Source)
	input.Actor = strings.TrimSpace(input.Actor)
	if input.ProjectID == "" || input.SubjectID == "" || (input.SubjectKind != "task" && input.SubjectKind != "wave") {
		return AgentContact{}, ExecutionRecord{}, tuskerError(errorInvalidArg, "external contact registration requires a durable task or wave subject")
	}
	if err := s.validateExternalContactSubject(input.ProjectID, input.SubjectID, input.SubjectKind); err != nil {
		return AgentContact{}, ExecutionRecord{}, err
	}
	if expected, ok := externalContactSource[input.Harness]; !ok || input.Source != expected {
		return AgentContact{}, ExecutionRecord{}, tuskerError(errorInvalidArg, "external contact harness/source pairing must be codex/direct_codex, claude-code/direct_claude, or devin/direct_devin")
	}
	if input.Provider == "" || input.ConversationID == "" || input.ConnectionID == "" {
		return AgentContact{}, ExecutionRecord{}, tuskerError(errorMissingField, "external contact registration requires provider, conversation_id, and connection_id")
	}
	if input.Role != "peer" && input.Name != "" {
		return AgentContact{}, ExecutionRecord{}, tuskerError(errorInvalidArg, "contact-name is only valid for peer contacts")
	}
	if !strings.HasPrefix(input.Actor, "human:") && !strings.HasPrefix(input.Actor, "operator:") {
		return AgentContact{}, ExecutionRecord{}, tuskerError(errorInvalidArg, "external contact registration requires an explicit human or operator actor")
	}
	now := executionNow()
	record := ExecutionRecord{ExecutionID: newExecutionID(), ProjectID: input.ProjectID, NodeKind: ExecutionNodeRoot, DisplayName: input.Harness + ":" + input.ConversationID, Source: input.Source, Provider: input.Harness, ProviderSessionID: input.ConversationID, SessionRef: input.ConnectionID, Creator: input.Actor, CreatedAt: now}
	if input.SubjectKind == "task" {
		record.TaskID = input.SubjectID
	} else {
		record.WaveID = input.SubjectID
	}
	record.RootExecutionID = record.ExecutionID
	record.SearchLabel = normalizeExecutionLabel(record.DisplayName, record.ProviderSessionID, record.ExecutionID)
	contact := AgentContact{ProjectID: input.ProjectID, TaskID: input.SubjectID, Role: input.Role, Name: input.Name, Address: AgentAddress{Kind: "execution", ID: record.ExecutionID}, Endpoint: map[string]string{
		"harness":         input.Harness,
		"provider":        input.Provider,
		"conversation_id": input.ConversationID,
		"connection_id":   input.ConnectionID,
	}}
	if err := contact.validate(); err != nil {
		return AgentContact{}, ExecutionRecord{}, tuskerError(errorInvalidArg, err.Error())
	}
	endpoint, _ := json.Marshal(contact.Endpoint)
	err := s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var currentGeneration int
		var predecessor string
		lookupErr := tx.QueryRow(`SELECT generation,address_id FROM agent_contacts WHERE project_id=? AND task_id=? AND role=? AND name=?`, contact.ProjectID, contact.TaskID, contact.Role, contact.Name).Scan(&currentGeneration, &predecessor)
		if input.ExpectedGeneration == 0 {
			if lookupErr == nil {
				return tuskerError(errorInvalidTransition, "contact already exists; pass --if-generation with its current generation to replace")
			}
			if lookupErr != sql.ErrNoRows {
				return lookupErr
			}
			predecessor = ""
		} else {
			if lookupErr == sql.ErrNoRows {
				return tuskerError(errorNotFound, "contact does not exist; use --if-generation 0 to create")
			}
			if lookupErr != nil {
				return lookupErr
			}
			if currentGeneration != input.ExpectedGeneration {
				return tuskerError(errorInvalidTransition, "contact generation changed; reload before replacing")
			}
		}
		contact.Generation = input.ExpectedGeneration + 1
		contact.PredecessorID = predecessor
		contact.UpdatedAt = now
		if err := insertExecutionWithEdgeTx(tx, record, ExecutionEdge{}); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO execution_attachment_events(event_id, execution_id, project_id, provider, provider_session_id, session_ref, source, actor, created_at) VALUES(?,?,?,?,?,?,?,?,?)`, "exec-attach-"+strings.ToLower(newRecordID()), record.ExecutionID, record.ProjectID, input.Harness, input.ConversationID, input.ConnectionID, input.Source, input.Actor, now); err != nil {
			return err
		}
		var result sql.Result
		if input.ExpectedGeneration == 0 {
			result, err = tx.Exec(`INSERT OR IGNORE INTO agent_contacts(project_id,task_id,role,name,address_kind,address_id,generation,predecessor_id,endpoint_json,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, contact.ProjectID, contact.TaskID, contact.Role, contact.Name, contact.Address.Kind, contact.Address.ID, contact.Generation, contact.PredecessorID, string(endpoint), contact.UpdatedAt)
		} else {
			result, err = tx.Exec(`UPDATE agent_contacts SET address_kind=?,address_id=?,generation=?,predecessor_id=?,endpoint_json=?,updated_at=? WHERE project_id=? AND task_id=? AND role=? AND name=? AND generation=?`, contact.Address.Kind, contact.Address.ID, contact.Generation, contact.PredecessorID, string(endpoint), contact.UpdatedAt, contact.ProjectID, contact.TaskID, contact.Role, contact.Name, input.ExpectedGeneration)
		}
		if err != nil {
			return err
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return tuskerError(errorInvalidTransition, "contact generation changed; reload before replacing")
		}
		if input.ExpectedGeneration > 0 && (input.Harness == "claude-code" || input.Harness == "codex") {
			// Messages the old session has not seen follow the replacement contact.
			if _, err := tx.Exec(`UPDATE agent_messages SET recipient_id=?,recipient_generation=? WHERE project_id=? AND recipient_kind='execution' AND recipient_id=? AND transport_state='pending' AND consumed_at=''`, record.ExecutionID, contact.Generation, contact.ProjectID, predecessor); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE agent_wakeups SET recipient_id=? WHERE project_id=? AND recipient_kind='execution' AND recipient_id=? AND state IN ('queued','held')`, record.ExecutionID, contact.ProjectID, predecessor); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	if err != nil {
		return AgentContact{}, ExecutionRecord{}, err
	}
	return contact, record, nil
}

func (s *RuntimeStore) validateExternalContactSubject(projectID, subjectID, subjectKind string) error {
	project, err := projectByID(s, projectID)
	if err != nil {
		return err
	}
	idx, err := loadV7Index(project.VaultRoot)
	if err != nil {
		return err
	}
	var subject Note
	var found bool
	switch subjectKind {
	case "task":
		subject, found = idx.Tasks[subjectID]
	case "wave":
		subject, found = idx.Waves[subjectID]
	}
	if !found || strings.TrimSpace(stringField(subject.Data, "project")) != projectID {
		return tuskerError(errorNotFound, subjectKind+" not found: "+subjectID)
	}
	return nil
}

func (s *RuntimeStore) PutAgentContact(c AgentContact, expectedGeneration int) (AgentContact, error) {
	if err := c.validate(); err != nil {
		return AgentContact{}, err
	}
	c.Generation = expectedGeneration + 1
	if c.UpdatedAt == "" {
		c.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	endpoint, _ := json.Marshal(c.Endpoint)
	var result sql.Result
	var err error
	if expectedGeneration == 0 {
		result, err = s.exec(`INSERT OR IGNORE INTO agent_contacts(project_id,task_id,role,name,address_kind,address_id,generation,predecessor_id,endpoint_json,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, c.ProjectID, c.TaskID, c.Role, c.Name, c.Address.Kind, c.Address.ID, c.Generation, c.PredecessorID, string(endpoint), c.UpdatedAt)
	} else {
		result, err = s.exec(`UPDATE agent_contacts SET address_kind=?,address_id=?,generation=?,predecessor_id=?,endpoint_json=?,updated_at=? WHERE project_id=? AND task_id=? AND role=? AND name=? AND generation=?`, c.Address.Kind, c.Address.ID, c.Generation, c.PredecessorID, string(endpoint), c.UpdatedAt, c.ProjectID, c.TaskID, c.Role, c.Name, expectedGeneration)
	}
	if err != nil {
		return AgentContact{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return AgentContact{}, errors.New("contact generation changed; reload before replacing")
	}
	return c, nil
}

func (s *RuntimeStore) AgentContacts(projectID, taskID string) ([]AgentContact, error) {
	rows, err := s.query(`SELECT project_id,task_id,role,name,address_kind,address_id,generation,predecessor_id,endpoint_json,updated_at FROM agent_contacts WHERE project_id=? AND task_id=? ORDER BY CASE role WHEN 'architect' THEN 0 WHEN 'origin' THEN 1 ELSE 2 END,name`, projectID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentContact{}
	for rows.Next() {
		var c AgentContact
		var endpoint string
		if err := rows.Scan(&c.ProjectID, &c.TaskID, &c.Role, &c.Name, &c.Address.Kind, &c.Address.ID, &c.Generation, &c.PredecessorID, &endpoint, &c.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(endpoint), &c.Endpoint)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) ResolveAgentContact(projectID, taskID, role, name string) (AgentAddress, error) {
	contacts, err := s.AgentContacts(projectID, taskID)
	if err != nil {
		return AgentAddress{}, err
	}
	var found []AgentAddress
	for _, c := range contacts {
		if c.Role == role && (role != "peer" || c.Name == name) {
			found = append(found, c.Address)
		}
	}
	if len(found) != 1 {
		return AgentAddress{}, errors.New("contact does not resolve to one current address")
	}
	return found[0], nil
}

func (s *RuntimeStore) ResolveAgentContactBinding(projectID, taskID, role, name string) (AgentContactBinding, error) {
	projectID, taskID, role, name = strings.TrimSpace(projectID), strings.TrimSpace(taskID), strings.TrimSpace(role), strings.TrimSpace(name)
	contacts, err := s.AgentContacts(projectID, taskID)
	if err != nil {
		return AgentContactBinding{}, err
	}
	var found []AgentContact
	for _, contact := range contacts {
		if contact.Role == role && (role != "peer" || contact.Name == name) {
			found = append(found, contact)
		}
	}
	inheritedFrom := ""
	if len(found) == 0 {
		if waveID, waveErr := s.agentContactSubjectWave(projectID, taskID); waveErr == nil && waveID != "" {
			if waveContacts, contactsErr := s.AgentContacts(projectID, waveID); contactsErr == nil {
				for _, contact := range waveContacts {
					if contact.Role == role && (role != "peer" || contact.Name == name) {
						found = append(found, contact)
					}
				}
				if len(found) > 0 {
					inheritedFrom = waveID
				}
			}
		}
	}
	if len(found) != 1 {
		return AgentContactBinding{}, errors.New("contact does not resolve to one current address")
	}
	binding := AgentContactBinding{Contact: found[0], State: AgentContactBindingUnbound, InheritedFrom: inheritedFrom, Generation: found[0].Generation}
	binding.Harness = strings.TrimSpace(found[0].Endpoint["harness"])
	binding.ConversationID = strings.TrimSpace(found[0].Endpoint["conversation_id"])
	binding.ConnectionID = strings.TrimSpace(found[0].Endpoint["connection_id"])
	if endpointProvider := strings.TrimSpace(found[0].Endpoint["provider"]); endpointProvider != "" {
		binding.Provider = endpointProvider
	}
	if found[0].Address.Kind == "task" {
		project, projectErr := projectByID(s, projectID)
		if projectErr != nil {
			binding.Reason = "task contact project is not registered"
			return binding, nil
		}
		idx, indexErr := loadV7Index(project.VaultRoot)
		if indexErr != nil {
			binding.Reason = "task contact target cannot be verified"
			return binding, nil
		}
		if _, exists := idx.Tasks[found[0].Address.ID]; !exists {
			binding.Reason = "task contact target is missing"
			return binding, nil
		}
		binding.Reason = "task continuation route is logical; live session unverified"
		binding.Capabilities = AgentConnectorCapabilities{State: "unbound", Reason: binding.Reason}
		return binding, nil
	}

	unbound := func(reason string) AgentContactBinding {
		binding.Reason = reason
		binding.Capabilities = AgentConnectorCapabilities{State: "unbound", Reason: reason}
		return binding
	}
	view, err := s.ExecutionView(found[0].Address.ID)
	if err != nil {
		return AgentContactBinding{}, err
	}
	if view == nil {
		return unbound("execution is missing"), nil
	}
	binding.Execution = view
	if view.ProjectID != projectID {
		return unbound("execution belongs to another project"), nil
	}
	if view.TaskID != found[0].TaskID && view.WaveID != found[0].TaskID {
		if found[0].TaskID == taskID {
			return unbound("execution belongs to another task"), nil
		}
		return unbound("execution belongs to another subject"), nil
	}
	registeredExternal := binding.Harness != ""
	if registeredExternal && (strings.TrimSpace(view.ProviderSessionID) != binding.ConversationID || strings.TrimSpace(view.SessionRef) != binding.ConnectionID) {
		binding.Reason = "registered endpoint attachment no longer matches the contact generation"
		binding.State = "stale"
		binding.Capabilities = AgentConnectorCapabilities{State: "stale", Reason: binding.Reason}
		return binding, nil
	}
	if strings.TrimSpace(view.AttemptID) == "" {
		if registeredExternal {
			if binding.Harness == "claude-code" || binding.Harness == "codex" {
				binding.State = "inbox"
				binding.Reason = "delivered when that session's Tusker inbox hook runs (next prompt or turn end); not a live push"
				binding.Capabilities = AgentConnectorCapabilities{State: "inbox", Reason: binding.Reason, RetrieveResponse: true}
				return binding, nil
			}
			binding.State = "unsupported"
			binding.Reason = externalConnectorUnsupportedReason(binding.Harness)
			binding.Capabilities = AgentConnectorCapabilities{State: "unsupported", Reason: binding.Reason}
			return binding, nil
		}
		return unbound("execution has no admitted attempt"), nil
	}
	if strings.TrimSpace(view.ProviderSessionID) == "" && strings.TrimSpace(view.SessionRef) == "" {
		return unbound("execution has no attached provider session"), nil
	}
	provider, providerErr := s.ExecutionProvider(view.ExecutionID)
	if providerErr != nil {
		return AgentContactBinding{}, providerErr
	}
	if binding.Provider == "" {
		binding.Provider = provider
	}
	if provider != "codex" {
		if provider == "" {
			return unbound("execution provider is unknown"), nil
		}
		return unbound("execution provider is unsupported: " + provider), nil
	}
	run, runErr := s.FindRunScoped(projectID, view.TaskID)
	if runErr != nil {
		return AgentContactBinding{}, runErr
	}
	if run == nil || run.ActiveAttemptID != view.AttemptID || run.Terminal || (run.LeaseState != string(LeaseStateClaimed) && run.LeaseState != string(LeaseStateRunning)) {
		binding.Reason = "execution attempt is not a live task owner"
		binding.State = "stale"
		binding.Capabilities = AgentConnectorCapabilities{State: "stale", Reason: binding.Reason}
		return binding, nil
	}
	handle, _ := liveRegistry.FindAttempt(view.AttemptID).(*codexLiveHandle)
	if handle == nil || handle.ProjectID() != projectID || handle.AttemptID() != view.AttemptID {
		binding.Reason = "live execution is not attached to a steerable codex handle"
		binding.State = "busy"
		binding.Capabilities = AgentConnectorCapabilities{State: "busy", Reason: binding.Reason, AttachmentValid: true}
		return binding, nil
	}
	if _, _, turnID, _ := handle.liveState(); turnID == "" {
		binding.Reason = "live codex conversation has no active steerable turn"
		binding.State = "busy"
		binding.Capabilities = AgentConnectorCapabilities{State: "busy", Reason: binding.Reason, AttachmentValid: true}
		return binding, nil
	}
	binding.State = AgentContactBindingBound
	binding.Reason = "live codex execution route"
	binding.Capabilities = AgentConnectorCapabilities{State: "available", MessageWhileRunning: true, AttachmentValid: true}
	return binding, nil
}

func externalConnectorUnsupportedReason(harness string) string {
	return "no installed connector can deliver to a pre-existing " + harness + " conversation; route the work through a Tusker-admitted attempt or contact the endpoint out-of-band"
}

func (s *RuntimeStore) ResolveEffectiveAgentContact(projectID, taskID, role, name string) (AgentContact, string, error) {
	match := func(contacts []AgentContact) (AgentContact, bool) {
		var found []AgentContact
		for _, contact := range contacts {
			if contact.Role == role && (role != "peer" || contact.Name == name) {
				found = append(found, contact)
			}
		}
		if len(found) == 1 {
			return found[0], true
		}
		return AgentContact{}, false
	}
	contacts, err := s.AgentContacts(projectID, taskID)
	if err != nil {
		return AgentContact{}, "", err
	}
	if contact, ok := match(contacts); ok {
		return contact, "", nil
	}
	waveID, err := s.agentContactSubjectWave(projectID, taskID)
	if err != nil || waveID == "" {
		return AgentContact{}, "", errors.New("contact does not resolve to one current address")
	}
	waveContacts, err := s.AgentContacts(projectID, waveID)
	if err != nil {
		return AgentContact{}, "", err
	}
	if contact, ok := match(waveContacts); ok {
		return contact, waveID, nil
	}
	return AgentContact{}, "", errors.New("contact does not resolve to one current address")
}

func (s *RuntimeStore) ResolveEffectiveAgentContactAddress(projectID, taskID, role, name string) (AgentAddress, string, error) {
	contact, inheritedFrom, err := s.ResolveEffectiveAgentContact(projectID, taskID, role, name)
	if err != nil {
		return AgentAddress{}, "", err
	}
	return contact.Address, inheritedFrom, nil
}

func (s *RuntimeStore) agentContactSubjectWave(projectID, taskID string) (string, error) {
	project, err := projectByID(s, projectID)
	if err != nil {
		return "", nil
	}
	idx, err := loadV7Index(project.VaultRoot)
	if err != nil {
		return "", nil
	}
	found := ""
	for waveID, wave := range idx.Waves {
		for _, member := range normalizeList(wave.Data["members"]) {
			if member != taskID {
				continue
			}
			if found != "" && found != waveID {
				return "", nil
			}
			found = waveID
		}
	}
	return found, nil
}

func parseAgentAddress(value string) (AgentAddress, error) {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	if len(parts) != 2 || parts[1] == "" || (parts[0] != "task" && parts[0] != "execution") {
		return AgentAddress{}, errors.New("contact address must be task:<id> or execution:<id>")
	}
	return AgentAddress{Kind: parts[0], ID: parts[1]}, nil
}

func authoredAgentContacts(task Note) []AgentContact {
	project, taskID := stringField(task.Data, "project"), stringField(task.Data, "id")
	out := []AgentContact{}
	add := func(role, name, value string) {
		address, err := parseAgentAddress(value)
		if err == nil {
			out = append(out, AgentContact{ProjectID: project, TaskID: taskID, Role: role, Name: name, Address: address, Generation: 1})
		}
	}
	add("architect", "", stringField(task.Data, "architect"))
	add("origin", "", stringField(task.Data, "origin"))
	for name, value := range normalizeStringMap(task.Data["peer_contacts"]) {
		add("peer", name, value)
	}
	return out
}
