package main

import "testing"

func TestExecutionGraphProjection(t *testing.T) {
	store := executionLedgerStore(t)
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "Release audit", Source: "direct_codex", Provider: "codex", ProviderSessionID: "thread-1", AgentType: "coordinator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: root.ExecutionID, Provider: "codex", ProviderSessionID: "thread-1", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	result, err := store.ApplyProviderExecutionEvent(providerEvent(1, "graph-1"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.ExecutionGraph("project-1", ExecutionGraphFilter{Name: "release"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Schema != executionGraphSchema || len(page.Nodes) != 1 || page.Nodes[0].ExecutionID != root.ExecutionID {
		t.Fatalf("first graph page=%#v", page)
	}
	page, err = store.ExecutionGraph("project-1", ExecutionGraphFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var child *ExecutionGraphNode
	for i := range page.Nodes {
		if page.Nodes[i].ExecutionID == result.ChildExecutionID {
			child = &page.Nodes[i]
		}
	}
	if child == nil || !child.ProviderOwned || child.ProviderStatus != "running" || len(child.ProviderCapabilities) != 1 {
		t.Fatalf("child graph node=%#v", page.Nodes)
	}
	if len(page.Edges) != 1 || page.Edges[0].Kind != ExecutionProviderChildOf || page.Edges[0].ParentExecutionID != root.ExecutionID {
		t.Fatalf("graph edges=%#v", page.Edges)
	}
	limited, err := store.ExecutionGraph("project-1", ExecutionGraphFilter{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.Nodes) != 1 || len(limited.Edges) != 0 || limited.NextCursor == "" || !limited.TopologyPartial {
		t.Fatalf("page must disclose omitted topology: %#v", limited)
	}
}

func TestExecutionGraphFiltersAndReadOnly(t *testing.T) {
	store := executionLedgerStore(t)
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "Unbound Codex", Source: "direct_codex", Provider: "codex", ProviderSessionID: "thread-2", AgentType: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.ExecutionGraph("project-1", ExecutionGraphFilter{ProviderID: "thread-2", AgentType: "review", Attention: "false"})
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Nodes) != 1 || before.Nodes[0].ExecutionID != root.ExecutionID || before.Nodes[0].Lifecycle.DeliveryState != "unbound" {
		t.Fatalf("filtered graph=%#v", before)
	}
	if _, err := store.BindExecution(ExecutionBindingInput{ProjectID: "project-1", ExecutionID: root.ExecutionID, TaskID: "ORC-T-0071", WaveID: "W-0007", Actor: "operator"}, "bind"); err != nil {
		t.Fatal(err)
	}
	bound, err := store.ExecutionGraph("project-1", ExecutionGraphFilter{TaskID: "ORC-T-0071", WaveID: "W-0007"})
	if err != nil || len(bound.Nodes) != 1 || bound.Nodes[0].ExecutionID != root.ExecutionID || bound.Nodes[0].BoundTaskID != "ORC-T-0071" {
		t.Fatalf("bound direct filter=%#v err=%v", bound, err)
	}
	unboundRoot, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "Still unbound", Source: "direct_codex", Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := store.ExecutionGraph("project-1", ExecutionGraphFilter{Lifecycle: "bound"})
	if err != nil || len(lifecycle.Nodes) != 1 || lifecycle.Nodes[0].ExecutionID != root.ExecutionID {
		t.Fatalf("delivery lifecycle filter=%#v err=%v", lifecycle, err)
	}
	unboundLifecycle, err := store.ExecutionGraph("project-1", ExecutionGraphFilter{Lifecycle: "unbound"})
	if err != nil || len(unboundLifecycle.Nodes) != 1 || unboundLifecycle.Nodes[0].ExecutionID != unboundRoot.ExecutionID {
		t.Fatalf("inverse delivery lifecycle filter=%#v err=%v", unboundLifecycle, err)
	}
	derived, err := store.ExecutionGraph("project-1", ExecutionGraphFilter{TaskID: "ORC-T-0071", Lifecycle: "unsettled"})
	if err != nil || len(derived.Nodes) != 1 || derived.Nodes[0].ExecutionID != root.ExecutionID {
		t.Fatalf("derived lifecycle filter=%#v err=%v", derived, err)
	}
	var bindings, runs int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_binding_events`, nil, &bindings); err != nil {
		t.Fatal(err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM runs`, nil, &runs); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecutionGraph("project-1", ExecutionGraphFilter{}); err != nil {
		t.Fatal(err)
	}
	var afterBindings, afterRuns int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_binding_events`, nil, &afterBindings); err != nil {
		t.Fatal(err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM runs`, nil, &afterRuns); err != nil {
		t.Fatal(err)
	}
	if bindings != afterBindings || runs != afterRuns {
		t.Fatalf("read graph mutated authority: bindings %d/%d runs %d/%d", bindings, afterBindings, runs, afterRuns)
	}
}
