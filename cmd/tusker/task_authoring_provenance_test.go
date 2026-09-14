package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskAuthoringIdentityCaptureOnce(t *testing.T) {
	data := map[string]any{}
	first := TaskAuthoringContext{Source: "codex", ConversationID: "conversation-1", Host: "local"}
	if err := CaptureTaskAuthoringProvenance(data, first); err != nil {
		t.Fatal(err)
	}
	prior, ok, err := TaskAuthoringProvenanceFromTask(data)
	if err != nil || !ok || prior.ConversationID != first.ConversationID || prior.BindingState != taskAuthoringBindingUnbound {
		t.Fatalf("captured provenance=%#v present=%t err=%v", prior, ok, err)
	}
	// A retry/import from another conversation must preserve the authoring
	// fact; it is not a replacement-author operation.
	if err := CaptureTaskAuthoringProvenance(data, TaskAuthoringContext{Source: "claude", ConversationID: "conversation-2", Host: "local"}); err != nil {
		t.Fatal(err)
	}
	after, _, err := TaskAuthoringProvenanceFromTask(data)
	if err != nil || after != prior {
		t.Fatalf("retry changed authoring provenance before=%#v after=%#v err=%v", prior, after, err)
	}
	if err := CaptureTaskAuthoringProvenance(data, TaskAuthoringContext{}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskAuthoringIdentityUnknownSourceIsRecorded(t *testing.T) {
	data := map[string]any{}
	if err := CaptureTaskAuthoringProvenanceFromEnvironment(data); err != nil {
		t.Fatal(err)
	}
	provenance, ok, err := TaskAuthoringProvenanceFromTask(data)
	if err != nil || !ok || provenance.Source != "unknown" || provenance.BindingState != taskAuthoringBindingUnbound {
		t.Fatalf("unknown provenance=%#v present=%t err=%v", provenance, ok, err)
	}
}

func TestTaskAuthoringIdentityEnvironmentContext(t *testing.T) {
	t.Setenv("CODEX_SESSION_ID", "native-session")
	t.Setenv("CODEX_THREAD_ID", "provider-thread")
	t.Setenv("TUSKER_HOST_ID", "local")
	context := TaskAuthoringContextFromEnvironment()
	if context.Source != "codex" || context.ConversationID != "native-session" || context.Host != "local" {
		t.Fatalf("environment context=%#v", context)
	}
	trigger := SelfImplementationTrigger(context)
	if !SameAuthoringConversation(trigger, context) {
		t.Fatalf("same native conversation was not detected: %q", trigger)
	}
	if SameAuthoringConversation(trigger, TaskAuthoringContext{Source: "codex", ConversationID: "other"}) {
		t.Fatalf("different native conversation was treated as independent=false")
	}
}

func TestTaskAuthoringIdentityRequiresKnownNativeConversation(t *testing.T) {
	if nativeConversationKnown(TaskAuthoringContext{Source: "claude"}) {
		t.Fatal("Claude marker without a native conversation was accepted")
	}
	if nativeConversationKnown(TaskAuthoringContext{Source: "unknown", ConversationID: "invented"}) {
		t.Fatal("unknown provider conversation was accepted")
	}
	if !nativeConversationKnown(TaskAuthoringContext{Source: "codex", ConversationID: "native-session"}) {
		t.Fatal("known Codex conversation was rejected")
	}
}

func TestTaskAuthoringIdentityContactBinding(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.PutAgentContact(AgentContact{ProjectID: "project", TaskID: "TASK-1", Role: "architect", Address: AgentAddress{Kind: "execution", ID: "missing-execution"}}, 0); err != nil {
		t.Fatal(err)
	}
	binding, err := store.ResolveAgentContactBinding("project", "TASK-1", "architect", "")
	if err != nil {
		t.Fatal(err)
	}
	if binding.State != AgentContactBindingUnbound || !strings.Contains(binding.Reason, "missing") {
		t.Fatalf("missing execution was not visibly unbound: %#v", binding)
	}
	if _, err := store.PutAgentContact(AgentContact{ProjectID: "project", TaskID: "TASK-1", Role: "origin", Address: AgentAddress{Kind: "task", ID: "UNREGISTERED-TASK"}}, 0); err != nil {
		t.Fatal(err)
	}
	logical, err := store.ResolveAgentContactBinding("project", "TASK-1", "origin", "")
	if err != nil {
		t.Fatal(err)
	}
	if logical.State != AgentContactBindingUnbound || !strings.Contains(logical.Reason, "not registered") {
		t.Fatalf("unregistered task route was treated as live: %#v", logical)
	}
	execution, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project", Provider: "codex", Source: "direct_codex"})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := store.ExecutionProvider(execution.ExecutionID)
	if err != nil || provider != "codex" {
		t.Fatalf("execution provider=%q err=%v", provider, err)
	}
	otherTaskExecution, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project", TaskID: "TASK-2", Provider: "codex", ProviderSessionID: "native-session", Source: "direct_codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutAgentContact(AgentContact{ProjectID: "project", TaskID: "TASK-1", Role: "peer", Name: "other-task", Address: AgentAddress{Kind: "execution", ID: otherTaskExecution.ExecutionID}}, 0); err != nil {
		t.Fatal(err)
	}
	crossTask, err := store.ResolveAgentContactBinding("project", "TASK-1", "peer", "other-task")
	if err != nil {
		t.Fatal(err)
	}
	if crossTask.State != AgentContactBindingUnbound || !strings.Contains(crossTask.Reason, "another task") {
		t.Fatalf("cross-task execution route was treated as live: %#v", crossTask)
	}
}

func TestTaskAuthoringIdentityKeepsUnregisteredAuthoredContacts(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.PutAgentContact(AgentContact{ProjectID: "project", TaskID: "TASK-1", Role: "architect", Address: AgentAddress{Kind: "task", ID: "TASK-2"}}, 0); err != nil {
		t.Fatal(err)
	}
	task := Note{Data: map[string]any{"id": "TASK-1", "project": "project", "architect": "task:TASK-2", "origin": "task:TASK-3"}}
	identity, err := TaskAuthoringIdentityForTask(store, "project", task)
	if err != nil {
		t.Fatal(err)
	}
	if len(identity.Contacts) != 2 || identity.Contacts[1].Contact.Role != "origin" || identity.Contacts[1].State != AgentContactBindingUnbound {
		t.Fatalf("partial registration dropped authored contact: %#v", identity.Contacts)
	}
}

func TestTaskAuthoringIdentityCurrentWorkspaceValidation(t *testing.T) {
	repo := t.TempDir()
	if err := exec.Command("git", "init", "-q", repo).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("identity test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"config", "user.email", "test@example.invalid"}, {"config", "user.name", "Task Identity Test"}, {"add", "README"}, {"commit", "-qm", "initial"}} {
		git := exec.Command("git", append([]string{"-C", repo}, command...)...)
		if err := git.Run(); err != nil {
			t.Fatal(err)
		}
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	workspace, branch, err := currentConversationWorkspace(filepath.Clean(repo))
	if err != nil {
		t.Fatal(err)
	}
	if workspace != canonicalPath(repo) || branch == "" || branch == "HEAD" {
		t.Fatalf("workspace=%q branch=%q", workspace, branch)
	}
	if _, _, err := currentConversationWorkspace(filepath.Join(repo, "other")); err == nil {
		t.Fatal("mismatched repository was accepted")
	}
}
