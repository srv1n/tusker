package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func externalRoutingFixture(t *testing.T) (string, *RuntimeStore, string, string) {
	t.Helper()
	vault := v7DirectTestVault(t)
	if err := writeText(managedTuskerConfigPath(vault), "schema: tusker.config/v1\nproject_id: proj-ext\nmutation_mode: local\n"); err != nil {
		t.Fatal(err)
	}
	body := directAuthoringBodyPath(t, vault, "task.md", "# Fixture task\n\nSubstantive body.\n")
	if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Fixture task", "work-level": "standard", "body-file": body}); err != nil {
		t.Fatal(err)
	}
	request := `schema: tusker.wave-authoring/v1
title: Routing wave
outcome: Wave membership for contact inheritance.
tasks:
  - key: member
    title: Member task
    work_level: light
    body: "# Member\n\nMember body.\n"
`
	requestPath := directAuthoringBodyPath(t, vault, "wave.yaml", request)
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": requestPath, "request-key": "routing-v1"}); err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project := newRegisteredProject(v7RepoRoot(vault), vault)
	project.ProjectID = "proj-ext"
	project.Enabled = true
	if _, _, err := store.RegisterProject(project); err != nil {
		t.Fatal(err)
	}
	return vault, store, project.ProjectID, stateRoot
}

func externalRoutingInput(projectID, subjectID, subjectKind string) ExternalContactRegistrationInput {
	return ExternalContactRegistrationInput{
		ProjectID: projectID, SubjectID: subjectID, SubjectKind: subjectKind,
		Role: "architect", Harness: "codex", Provider: "openai",
		ConversationID: "conv-1", ConnectionID: "host-1",
		Source: "direct_codex", Actor: "operator:tester", ExpectedGeneration: 0,
	}
}

func TestExternalArchitectRoutingRegistersIdentityAndTruthfulCapabilities(t *testing.T) {
	_, store, projectID, _ := externalRoutingFixture(t)
	contact, record, err := store.RegisterExternalAgentContact(externalRoutingInput(projectID, "TSK-T-0001", "task"))
	if err != nil {
		t.Fatal(err)
	}
	if contact.Generation != 1 || contact.Address.Kind != "execution" || contact.Address.ID != record.ExecutionID {
		t.Fatalf("contact=%#v record=%#v", contact, record)
	}
	for _, key := range []string{"harness", "provider", "conversation_id", "connection_id"} {
		if contact.Endpoint[key] == "" {
			t.Fatalf("endpoint missing structured key %s: %#v", key, contact.Endpoint)
		}
	}
	if record.Provider != "codex" || record.ProviderSessionID != "conv-1" || record.SessionRef != "host-1" || record.TaskID != "TSK-T-0001" || record.AttemptID != "" {
		t.Fatalf("execution identity=%#v", record)
	}
	binding, err := store.ResolveAgentContactBinding(projectID, "TSK-T-0001", "architect", "")
	if err != nil {
		t.Fatal(err)
	}
	if binding.Contact.Address.ID != record.ExecutionID || binding.Harness != "codex" || binding.Provider != "openai" || binding.ConversationID != "conv-1" || binding.ConnectionID != "host-1" || binding.Generation != 1 || binding.InheritedFrom != "" {
		t.Fatalf("registered identity=%#v", binding)
	}
	if binding.State == "unbound" || binding.State == "" {
		t.Fatalf("registered endpoint reported missing/unbound: %#v", binding)
	}
	capabilities := binding.Capabilities
	if capabilities.State != "unsupported" || capabilities.MessageWhileRunning || capabilities.ContinueWhileIdle || capabilities.RetrieveResponse || capabilities.AttachmentValid || capabilities.Reason == "" {
		t.Fatalf("no-attempt endpoint capabilities=%#v", capabilities)
	}
}

func TestExternalArchitectRoutingWaveInheritanceAndTaskOverride(t *testing.T) {
	_, store, projectID, _ := externalRoutingFixture(t)
	waveContact, waveRecord, err := store.RegisterExternalAgentContact(externalRoutingInput(projectID, "W-0001", "wave"))
	if err != nil {
		t.Fatal(err)
	}
	if waveRecord.WaveID != "W-0001" || waveRecord.TaskID != "" {
		t.Fatalf("wave subject record=%#v", waveRecord)
	}
	inherited, err := store.ResolveAgentContactBinding(projectID, "TSK-T-0002", "architect", "")
	if err != nil {
		t.Fatal(err)
	}
	if inherited.Contact.Address.ID != waveRecord.ExecutionID || inherited.InheritedFrom != "W-0001" || inherited.Contact.TaskID != "W-0001" {
		t.Fatalf("wave contact was not inherited: %#v", inherited)
	}
	if inherited.Capabilities.State != "unsupported" {
		t.Fatalf("inherited external endpoint capabilities=%#v", inherited.Capabilities)
	}
	override := externalRoutingInput(projectID, "TSK-T-0002", "task")
	override.ConversationID = "conv-override"
	taskContact, taskRecord, err := store.RegisterExternalAgentContact(override)
	if err != nil {
		t.Fatal(err)
	}
	if taskContact.TaskID != "TSK-T-0002" {
		t.Fatalf("task contact=%#v", taskContact)
	}
	binding, err := store.ResolveAgentContactBinding(projectID, "TSK-T-0002", "architect", "")
	if err != nil {
		t.Fatal(err)
	}
	if binding.Contact.Address.ID != taskRecord.ExecutionID || binding.InheritedFrom != "" || binding.ConversationID != "conv-override" {
		t.Fatalf("task contact did not override wave contact: %#v", binding)
	}
	address, inheritedFrom, err := store.ResolveEffectiveAgentContactAddress(projectID, "TSK-T-0002", "architect", "")
	if err != nil || address.ID != taskRecord.ExecutionID || inheritedFrom != "" {
		t.Fatalf("effective address=%#v inherited=%q err=%v", address, inheritedFrom, err)
	}
	_ = waveContact
}

func TestExternalArchitectRoutingRefusalsLeaveNoPartialRows(t *testing.T) {
	vault, store, projectID, stateRoot := externalRoutingFixture(t)
	countRows := func(table string) int {
		var n int
		if err := store.queryRowScan(`SELECT COUNT(*) FROM `+table, nil, &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	base := externalRoutingInput(projectID, "TSK-T-0001", "task")
	for name, mutate := range map[string]func(*ExternalContactRegistrationInput){
		"agent actor":       func(i *ExternalContactRegistrationInput) { i.Actor = "agent:worker" },
		"reviewer actor":    func(i *ExternalContactRegistrationInput) { i.Actor = "reviewer:critic" },
		"daemon actor":      func(i *ExternalContactRegistrationInput) { i.Actor = "daemon" },
		"bad harness":       func(i *ExternalContactRegistrationInput) { i.Harness = "irc" },
		"missing provider":  func(i *ExternalContactRegistrationInput) { i.Provider = "" },
		"missing conv":      func(i *ExternalContactRegistrationInput) { i.ConversationID = "" },
		"missing conn":      func(i *ExternalContactRegistrationInput) { i.ConnectionID = "" },
		"non-peer name":     func(i *ExternalContactRegistrationInput) { i.Name = "named" },
		"peer missing name": func(i *ExternalContactRegistrationInput) { i.Role = "peer" },
		"bad source":        func(i *ExternalContactRegistrationInput) { i.Source = "daemon" },
		"codex mismatch":    func(i *ExternalContactRegistrationInput) { i.Source = "direct_claude" },
		"claude mismatch":   func(i *ExternalContactRegistrationInput) { i.Harness = "claude-code"; i.Source = "direct_codex" },
		"devin mismatch":    func(i *ExternalContactRegistrationInput) { i.Harness = "devin"; i.Source = "direct_codex" },
		"bad subject kind":  func(i *ExternalContactRegistrationInput) { i.SubjectKind = "epic" },
	} {
		input := base
		mutate(&input)
		if _, _, err := store.RegisterExternalAgentContact(input); err == nil {
			t.Fatalf("%s registration was accepted", name)
		}
	}
	if n := countRows("agent_contacts"); n != 0 {
		t.Fatalf("refused registrations wrote %d contacts", n)
	}
	if n := countRows("execution_records"); n != 0 {
		t.Fatalf("refused registrations wrote %d executions", n)
	}
	contactArgs := Args{
		"vault": vault, "state-root": stateRoot, "quiet": "true",
		"task": "TSK-T-9999", "contact-role": "architect", "harness": "codex", "provider": "openai",
		"conversation-id": "conv-x", "connection-id": "host-x", "source": "direct_codex",
		"if-generation": "0", "by": "operator:tester",
	}
	if err := executionCmd(contactArgs, "register"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown subject error=%v, want refusal", err)
	}
	contactArgs["task"], contactArgs["wave"] = "", "W-9999"
	if err := executionCmd(contactArgs, "register"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown wave error=%v, want refusal", err)
	}
	contactArgs["task"], contactArgs["wave"] = "TSK-T-0001", ""
	contactArgs["by"] = "agent:worker"
	if err := executionCmd(contactArgs, "register"); err == nil || !strings.Contains(err.Error(), "human") {
		t.Fatalf("agent actor error=%v, want refusal", err)
	}
	delete(contactArgs, "by")
	if err := executionCmd(contactArgs, "register"); err == nil || !strings.Contains(err.Error(), "--by") {
		t.Fatalf("missing --by error=%v, want refusal", err)
	}
	contactArgs["by"] = "  "
	if err := executionCmd(contactArgs, "register"); err == nil || !strings.Contains(err.Error(), "--by") {
		t.Fatalf("blank --by error=%v, want refusal", err)
	}
	contactArgs["by"] = "operator:tester"
	delete(contactArgs, "if-generation")
	if err := executionCmd(contactArgs, "register"); err == nil || !strings.Contains(err.Error(), "if-generation") {
		t.Fatalf("missing if-generation error=%v, want refusal", err)
	}
	if n := countRows("agent_contacts"); n != 0 || countRows("execution_records") != 0 {
		t.Fatalf("CLI refusals wrote rows: contacts=%d executions=%d", n, countRows("execution_records"))
	}
}

func TestExternalArchitectRoutingRegistrationSeamRequiresOwnedSubject(t *testing.T) {
	_, store, projectID, _ := externalRoutingFixture(t)
	input := externalRoutingInput(projectID, "TSK-T-9999", "task")
	if _, _, err := store.RegisterExternalAgentContact(input); err == nil || !strings.Contains(err.Error(), "task not found") {
		t.Fatalf("unknown task registration error=%v, want authoritative refusal", err)
	}
	var contacts, executions int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM agent_contacts`, nil, &contacts); err != nil || contacts != 0 {
		t.Fatalf("unknown task wrote contacts=%d err=%v", contacts, err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_records`, nil, &executions); err != nil || executions != 0 {
		t.Fatalf("unknown task wrote executions=%d err=%v", executions, err)
	}
}

func TestExternalArchitectRoutingGenerationFencingAndAttachmentScope(t *testing.T) {
	_, store, projectID, _ := externalRoutingFixture(t)
	input := externalRoutingInput(projectID, "TSK-T-0001", "task")
	first, firstRecord, err := store.RegisterExternalAgentContact(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.RegisterExternalAgentContact(input); err == nil {
		t.Fatal("same-generation create race was accepted")
	}
	stale := input
	stale.ExpectedGeneration = 5
	if _, _, err := store.RegisterExternalAgentContact(stale); err == nil {
		t.Fatal("stale replacement generation was accepted")
	}
	var contacts, records, attachments int
	for _, scan := range []struct {
		query string
		dest  *int
	}{
		{`SELECT COUNT(*) FROM agent_contacts`, &contacts},
		{`SELECT COUNT(*) FROM execution_records`, &records},
		{`SELECT COUNT(*) FROM execution_attachment_events`, &attachments},
	} {
		if err := store.queryRowScan(scan.query, nil, scan.dest); err != nil {
			t.Fatal(err)
		}
	}
	if contacts != 1 || records != 1 || attachments != 1 {
		t.Fatalf("stale refusals wrote rows: contacts=%d records=%d attachments=%d", contacts, records, attachments)
	}
	replacement := input
	replacement.ExpectedGeneration = first.Generation
	replacement.ConversationID = "conv-2"
	second, secondRecord, err := store.RegisterExternalAgentContact(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation != 2 || second.PredecessorID != firstRecord.ExecutionID || second.Address.ID == firstRecord.ExecutionID {
		t.Fatalf("replacement contact=%#v", second)
	}
	binding, err := store.ResolveAgentContactBinding(projectID, "TSK-T-0001", "architect", "")
	if err != nil || binding.Generation != 2 || binding.ConversationID != "conv-2" {
		t.Fatalf("binding after replacement=%#v err=%v", binding, err)
	}
	dup := externalRoutingInput(projectID, "TSK-T-0002", "task")
	dup.ConversationID = "conv-2"
	if _, _, err := store.RegisterExternalAgentContact(dup); err == nil {
		t.Fatal("provider session identity moved to a second subject attachment")
	}
	var stray int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM agent_contacts WHERE task_id='TSK-T-0002'`, nil, &stray); err != nil || stray != 0 {
		t.Fatalf("cross-attachment refusal wrote a contact: %d %v", stray, err)
	}
	if secondRecord.ExecutionID == "" {
		t.Fatal("replacement execution id was empty")
	}
}

func TestExternalArchitectRoutingConnectorSelectionStaysTruthful(t *testing.T) {
	_, store, projectID, _ := externalRoutingFixture(t)
	harnesses := []struct{ harness, source, provider string }{
		{"codex", "direct_codex", "openai"},
		{"claude-code", "direct_claude", "anthropic"},
		{"devin", "direct_devin", "devin"},
	}
	for i, tc := range harnesses {
		input := externalRoutingInput(projectID, "TSK-T-0001", "task")
		input.Role = "peer"
		input.Name = "ext-" + tc.harness
		input.Harness, input.Source, input.Provider = tc.harness, tc.source, tc.provider
		input.ConversationID = "conv-" + tc.harness
		_, record, err := store.RegisterExternalAgentContact(input)
		if err != nil {
			t.Fatalf("%s: %v", tc.harness, err)
		}
		if record.Provider != tc.harness || record.Source != tc.source {
			t.Fatalf("%s record=%#v", tc.harness, record)
		}
		binding, err := store.ResolveAgentContactBinding(projectID, "TSK-T-0001", "peer", "ext-"+tc.harness)
		if err != nil {
			t.Fatal(err)
		}
		if binding.Harness != tc.harness || binding.Provider != tc.provider {
			t.Fatalf("%s structured identity=%#v", tc.harness, binding)
		}
		if binding.Capabilities.State != "unsupported" || !strings.Contains(binding.Capabilities.Reason, tc.harness) {
			t.Fatalf("%s capabilities=%#v", tc.harness, binding.Capabilities)
		}
		_ = i
	}
	var records int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_records`, nil, &records); err != nil {
		t.Fatal(err)
	}
	if records != len(harnesses) {
		t.Fatalf("routing fabricated replacement executions or chats: %d", records)
	}
}

func TestExternalArchitectRoutingLiveCodexFixtureCapabilities(t *testing.T) {
	_, store, projectID, _ := externalRoutingFixture(t)
	record := ExecutionRecord{
		ExecutionID: newExecutionID(), ProjectID: projectID, NodeKind: ExecutionNodeRoot,
		DisplayName: "codex:conv-live", TaskID: "TSK-T-0001", AttemptID: "attempt-live-1",
		Source: "direct_codex", Provider: "codex", ProviderSessionID: "conv-live", SessionRef: "host-live",
		Creator: "operator:tester", CreatedAt: executionNow(),
	}
	record.RootExecutionID = record.ExecutionID
	record.SearchLabel = normalizeExecutionLabel(record.DisplayName, record.ExecutionID)
	if err := store.insertExecutionRecord(record); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: projectID, RecordID: "TSK-T-0001", ItemID: "TSK-T-0001", ActiveAttemptID: "attempt-live-1", LeaseState: string(LeaseStateRunning), LeaseGeneration: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutAgentContact(AgentContact{ProjectID: projectID, TaskID: "TSK-T-0001", Role: "architect", Address: AgentAddress{Kind: "execution", ID: record.ExecutionID}, Endpoint: map[string]string{"harness": "codex", "provider": "openai", "conversation_id": "conv-live", "connection_id": "host-live"}}, 0); err != nil {
		t.Fatal(err)
	}
	live := &codexLiveHandle{projectID: projectID, recordID: "TSK-T-0001", itemID: "TSK-T-0001", attemptID: "attempt-live-1", threadID: "thread-1", turnID: "turn-1"}
	liveRegistry.Register(live)
	t.Cleanup(func() { liveRegistry.Unregister("attempt-live-1") })
	binding, err := store.ResolveAgentContactBinding(projectID, "TSK-T-0001", "architect", "")
	if err != nil {
		t.Fatal(err)
	}
	if binding.State != AgentContactBindingBound {
		t.Fatalf("live codex fixture binding=%#v", binding)
	}
	capabilities := binding.Capabilities
	if capabilities.State != "available" || !capabilities.MessageWhileRunning || !capabilities.AttachmentValid || capabilities.ContinueWhileIdle || capabilities.RetrieveResponse {
		t.Fatalf("live codex fixture capabilities=%#v", capabilities)
	}
	live.setTurnID("")
	busy, err := store.ResolveAgentContactBinding(projectID, "TSK-T-0001", "architect", "")
	if err != nil {
		t.Fatal(err)
	}
	if busy.State != "busy" || busy.Capabilities.State != "busy" || busy.Capabilities.MessageWhileRunning {
		t.Fatalf("missing steerable turn was not busy: %#v", busy)
	}
	live.setTurnID("turn-1")
	if _, err := store.PutAgentContact(AgentContact{ProjectID: projectID, TaskID: "TSK-T-0001", Role: "peer", Name: "stale", Address: AgentAddress{Kind: "execution", ID: record.ExecutionID}, Endpoint: map[string]string{"harness": "codex", "provider": "openai", "conversation_id": "conv-replaced", "connection_id": "host-live"}}, 0); err != nil {
		t.Fatal(err)
	}
	stale, err := store.ResolveAgentContactBinding(projectID, "TSK-T-0001", "peer", "stale")
	if err != nil {
		t.Fatal(err)
	}
	if stale.State != "stale" || stale.Capabilities.State != "stale" {
		t.Fatalf("mismatched attachment was not stale: %#v", stale)
	}
	if busy.State == stale.State {
		t.Fatal("busy and stale states collapsed")
	}
}

func TestExternalArchitectRoutingMessageIdempotencyAndUnsupportedRoute(t *testing.T) {
	_, store, projectID, _ := externalRoutingFixture(t)
	_, record, err := store.RegisterExternalAgentContact(externalRoutingInput(projectID, "TSK-T-0001", "task"))
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: store}
	question := AgentMessage{IdempotencyKey: "q-1", ProjectID: projectID, Sender: "task:TSK-T-0001", Recipient: AgentAddress{Kind: "execution", ID: record.ExecutionID}, Kind: "question", Body: "External architect decision needed.", ReplyRequired: true}
	stored, duplicate, err := store.PutAgentMessage(question)
	if err != nil || duplicate {
		t.Fatalf("first question stored=%#v duplicate=%v err=%v", stored, duplicate, err)
	}
	again, duplicate, err := store.PutAgentMessage(question)
	if err != nil || !duplicate || again.ID != stored.ID {
		t.Fatalf("duplicate question=%#v duplicate=%v err=%v", again, duplicate, err)
	}
	var wakeups int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM agent_wakeups WHERE project_id=?`, []any{projectID}, &wakeups); err != nil || wakeups != 1 {
		t.Fatalf("duplicate question queued %d wakeups err=%v", wakeups, err)
	}
	if err := d.processAgentWakeups(projectID); err != nil {
		t.Fatal(err)
	}
	var state, transport string
	if err := store.queryRowScan(`SELECT state FROM agent_wakeups WHERE project_id=?`, []any{projectID}, &state); err != nil || state != "unsupported" {
		t.Fatalf("no-attempt external wakeup state=%q err=%v", state, err)
	}
	if err := store.queryRowScan(`SELECT transport_state FROM agent_messages WHERE id=?`, []any{stored.ID}, &transport); err != nil || transport != "unsupported" {
		t.Fatalf("external message transport=%q err=%v", transport, err)
	}
	if err := d.processAgentWakeups(projectID); err != nil {
		t.Fatal(err)
	}
	var againState, againTransport string
	if err := store.queryRowScan(`SELECT state FROM agent_wakeups WHERE project_id=?`, []any{projectID}, &againState); err != nil || againState != "unsupported" {
		t.Fatalf("terminal unsupported wakeup was reprocessed: state=%q err=%v", againState, err)
	}
	if err := store.queryRowScan(`SELECT transport_state FROM agent_messages WHERE id=?`, []any{stored.ID}, &againTransport); err != nil || againTransport != "unsupported" {
		t.Fatalf("terminal unsupported message transport changed: %q err=%v", againTransport, err)
	}
	answer := AgentMessage{IdempotencyKey: "a-1", ProjectID: projectID, Sender: "execution:" + record.ExecutionID, Recipient: AgentAddress{Kind: "task", ID: "TSK-T-0001"}, Kind: "answer", Body: "Ship it.", ReplyTo: stored.ID}
	if _, _, err := store.PutAgentMessageAsOperator(answer); err != nil {
		t.Fatal(err)
	}
	correlated, err := store.AgentMessage(projectID, stored.ID)
	if err != nil || correlated.AnsweredAt == "" {
		t.Fatalf("reply did not correlate to the original question: %#v err=%v", correlated, err)
	}
	uncertainWake, _, err := store.QueueAgentWakeup(projectID, AgentAddress{Kind: "execution", ID: record.ExecutionID}, "question", "uncertain-fixture", []string{stored.ID})
	if err != nil {
		t.Fatal(err)
	}
	claimID := store.ClaimAgentWakeup(uncertainWake.ID)
	if claimID == "" {
		t.Fatal("fixture wakeup was not claimable")
	}
	if err := store.SetAgentWakeupClaimState(uncertainWake.ID, claimID, "uncertain"); err != nil {
		t.Fatal(err)
	}
	queued, err := store.ListQueuedAgentWakeups(projectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range queued {
		if w.ID == uncertainWake.ID {
			t.Fatal("uncertain delivery was blindly re-queued")
		}
	}
}

func TestExternalArchitectRoutingAdmittedNonCodexWithoutHandleIsTerminalUnsupported(t *testing.T) {
	_, store, projectID, _ := externalRoutingFixture(t)
	record := ExecutionRecord{
		ExecutionID: newExecutionID(), ProjectID: projectID, NodeKind: ExecutionNodeRoot,
		DisplayName: "claude:conv-admitted", TaskID: "TSK-T-0001", AttemptID: "attempt-claude-1",
		Source: "direct_claude", Provider: "claude", ProviderSessionID: "conv-admitted", SessionRef: "host-1",
		Creator: "operator:tester", CreatedAt: executionNow(),
	}
	record.RootExecutionID = record.ExecutionID
	record.SearchLabel = normalizeExecutionLabel(record.DisplayName, record.ExecutionID)
	if err := store.insertExecutionRecord(record); err != nil {
		t.Fatal(err)
	}
	message, _, err := store.PutAgentMessage(AgentMessage{
		IdempotencyKey: "non-codex-1", ProjectID: projectID, Sender: "task:TSK-T-0001",
		Recipient: AgentAddress{Kind: "execution", ID: record.ExecutionID}, Kind: "instruction", Body: "unsupported route",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := (&Daemon{store: store}).processAgentWakeups(projectID); err != nil {
		t.Fatal(err)
	}
	var wakeupState, transportState string
	if err := store.queryRowScan(`SELECT state FROM agent_wakeups WHERE project_id=?`, []any{projectID}, &wakeupState); err != nil {
		t.Fatal(err)
	}
	if err := store.queryRowScan(`SELECT transport_state FROM agent_messages WHERE id=?`, []any{message.ID}, &transportState); err != nil {
		t.Fatal(err)
	}
	if wakeupState != "unsupported" || transportState != "unsupported" {
		t.Fatalf("non-Codex admitted route was not terminal unsupported: wakeup=%q transport=%q", wakeupState, transportState)
	}
	if err := (&Daemon{store: store}).processAgentWakeups(projectID); err != nil {
		t.Fatal(err)
	}
	var retryState, retryTransport string
	if err := store.queryRowScan(`SELECT state FROM agent_wakeups WHERE project_id=?`, []any{projectID}, &retryState); err != nil {
		t.Fatal(err)
	}
	if err := store.queryRowScan(`SELECT transport_state FROM agent_messages WHERE id=?`, []any{message.ID}, &retryTransport); err != nil {
		t.Fatal(err)
	}
	if retryState != "unsupported" || retryTransport != "unsupported" {
		t.Fatalf("terminal unsupported route was reprocessed: wakeup=%q transport=%q", retryState, retryTransport)
	}
}

func TestExternalArchitectRoutingRecipientGenerationFenceMarksStale(t *testing.T) {
	_, store, projectID, _ := externalRoutingFixture(t)
	_, record, err := store.RegisterExternalAgentContact(externalRoutingInput(projectID, "TSK-T-0001", "task"))
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: store}
	message := AgentMessage{IdempotencyKey: "g-1", ProjectID: projectID, Sender: "task:TSK-T-0001", Recipient: AgentAddress{Kind: "execution", ID: record.ExecutionID}, RecipientGeneration: 1, Kind: "instruction", Body: "Fenced update."}
	stored, _, err := store.PutAgentMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	replacement := externalRoutingInput(projectID, "TSK-T-0001", "task")
	replacement.ExpectedGeneration = 1
	replacement.ConversationID = "conv-gen2"
	second, secondRecord, err := store.RegisterExternalAgentContact(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation != 2 || secondRecord.ExecutionID == record.ExecutionID {
		t.Fatalf("replacement did not advance generation: %#v", second)
	}
	if err := d.processAgentWakeups(projectID); err != nil {
		t.Fatal(err)
	}
	var state, transport string
	if err := store.queryRowScan(`SELECT state FROM agent_wakeups WHERE project_id=?`, []any{projectID}, &state); err != nil || state != "stale" {
		t.Fatalf("replaced-generation wakeup state=%q err=%v", state, err)
	}
	if err := store.queryRowScan(`SELECT transport_state FROM agent_messages WHERE id=?`, []any{stored.ID}, &transport); err != nil || transport != "stale" {
		t.Fatalf("replaced-generation message transport=%q err=%v", transport, err)
	}
}

func TestExternalArchitectRoutingPublicCommandRejectsMismatchedProject(t *testing.T) {
	_, store, projectID, stateRoot := externalRoutingFixture(t)
	if _, _, err := store.RegisterExternalAgentContact(externalRoutingInput(projectID, "TSK-T-0001", "task")); err != nil {
		t.Fatal(err)
	}
	vaultB := v7DirectTestVault(t)
	if err := writeText(managedTuskerConfigPath(vaultB), "schema: tusker.config/v1\nproject_id: proj-b\nmutation_mode: local\n"); err != nil {
		t.Fatal(err)
	}
	projectB := newRegisteredProject(v7RepoRoot(vaultB), vaultB)
	projectB.ProjectID = "proj-b"
	projectB.Enabled = true
	if _, _, err := store.RegisterProject(projectB); err != nil {
		t.Fatal(err)
	}
	err := executionCmd(Args{
		"vault": vaultB, "state-root": stateRoot, "quiet": "true",
		"task": "TSK-T-0001", "contact-role": "architect", "harness": "codex", "provider": "openai",
		"conversation-id": "conv-other", "connection-id": "host-other", "source": "direct_codex",
		"if-generation": "0", "by": "operator:tester",
	}, "register")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("cross-project subject attach error=%v, want refusal", err)
	}
	var contacts, records int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM agent_contacts WHERE project_id='proj-b'`, nil, &contacts); err != nil || contacts != 0 {
		t.Fatalf("mismatched registration wrote contacts: %d err=%v", contacts, err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_records WHERE project_id='proj-b'`, nil, &records); err != nil || records != 0 {
		t.Fatalf("mismatched registration wrote executions: %d err=%v", records, err)
	}
}
