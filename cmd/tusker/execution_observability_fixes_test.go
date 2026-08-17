package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestExecutionLifecycleUnknownSessionMergesPriorEvidence(t *testing.T) {
	store := executionLedgerStore(t)
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "TASK-1", ItemID: "TASK-1", ActiveAttemptID: "attempt-1", LeaseGeneration: 1, LeaseState: string(LeaseStateRunning)}); err != nil {
		t.Fatal(err)
	}
	managed, err := store.CreateManagedExecution(ManagedExecutionInput{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, TaskID: "TASK-1", AttemptID: "attempt-1", LeaseGeneration: 1, Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := store.ExecutionLifecycle(managed.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if facts.SessionState != "unknown" {
		t.Fatalf("empty current session refs did not default to unknown: %#v", facts)
	}
	if err := store.appendExecutionLifecycleEvidence(ExecutionLifecycleEvidence{ExecutionID: managed.ExecutionID, SessionState: "known"}); err != nil {
		t.Fatal(err)
	}
	facts, err = store.ExecutionLifecycle(managed.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if facts.SessionState != "known" {
		t.Fatalf("empty current session refs discarded prior evidence: %#v", facts)
	}
	if err := store.appendExecutionLifecycleEvidence(ExecutionLifecycleEvidence{ExecutionID: managed.ExecutionID}); err != nil {
		t.Fatal(err)
	}
	facts, err = store.ExecutionLifecycle(managed.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if facts.ProviderState != "unknown" || facts.OutcomeState != "unknown" || facts.SessionState != "unknown" {
		t.Fatalf("empty prior evidence overwrote lifecycle defaults: %#v", facts)
	}
}

func TestCodexPayloadStringBoundsDepthAndVisits(t *testing.T) {
	deep := any(map[string]any{"wanted": "found"})
	for i := 0; i < codexPayloadMaxDepth; i++ {
		deep = map[string]any{"nested": deep}
	}
	if got := codexPayloadString(deep, "wanted"); got != "found" {
		t.Fatalf("value within depth bound=%q", got)
	}
	deep = map[string]any{"nested": deep}
	if got := codexPayloadString(deep, "wanted"); got != "" {
		t.Fatalf("value beyond depth bound=%q", got)
	}
	if got := claudePayloadString(deep, "wanted"); got != "" {
		t.Fatalf("claude alias exceeded depth bound=%q", got)
	}
	pruned := make([]any, 2)
	pruned[0] = deep
	pruned[1] = map[string]any{"wanted": "found after pruned branch"}
	if got := codexPayloadString(pruned, "wanted"); got != "found after pruned branch" {
		t.Fatalf("depth-limited branch aborted sibling search=%q", got)
	}

	wide := make([]any, codexPayloadMaxNodes+1)
	for i := range wide {
		wide[i] = map[string]any{"other": "value"}
	}
	wide[len(wide)-1] = map[string]any{"wanted": "found"}
	if got := codexPayloadString(wide, "wanted"); got != "" {
		t.Fatalf("value beyond node bound=%q", got)
	}
}

func TestExecutionGraphMemoizedWalkMatchesNaiveWalk(t *testing.T) {
	store := executionLedgerStore(t)
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "root", Source: "direct_codex", Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "TASK-G", ItemID: "TASK-G", ActiveAttemptID: "attempt-g", LeaseGeneration: 4, LeaseState: string(LeaseStateRunning)}); err != nil {
		t.Fatal(err)
	}
	managed, err := store.CreateManagedExecution(ManagedExecutionInput{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, TaskID: "TASK-G", AttemptID: "attempt-g", LeaseGeneration: 4, Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "TASK-G-2", ItemID: "TASK-G-2", ActiveAttemptID: "attempt-g-2", LeaseGeneration: 5, LeaseState: string(LeaseStateRunning)}); err != nil {
		t.Fatal(err)
	}
	managed2, err := store.CreateManagedExecution(ManagedExecutionInput{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, TaskID: "TASK-G-2", AttemptID: "attempt-g-2", LeaseGeneration: 5, Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	child, created, err := store.UpsertProviderChildExecution(ProviderChildExecutionInput{ProjectID: "project-1", ParentExecutionID: managed.ExecutionID, Provider: "codex", ProviderChildHandle: "child-g"})
	if err != nil || !created {
		t.Fatalf("provider child created=%t err=%v", created, err)
	}
	if _, err := store.exec(`DROP INDEX execution_edges_one_parent`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`INSERT INTO execution_edges (parent_execution_id, child_execution_id, kind, created_at) VALUES (?, ?, ?, ?)`, managed2.ExecutionID, child.ExecutionID, string(ExecutionProviderChildOf), executionNow()); err != nil {
		t.Fatal(err)
	}
	if _, err := (CodexExecutionAdapter{Store: store}).Observe(CodexExecutionObservation{
		ProjectID: "project-1", ParentExecutionID: managed.ExecutionID, ThreadID: "diamond-thread", SourceEventID: "diamond-degraded", Status: "running", OccurredAt: executionNow(), ChildID: "child-g", VisibilityDegradedReason: "fixture-degraded",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "second root", Source: "direct_claude", Provider: "claude"}); err != nil {
		t.Fatal(err)
	}

	want, err := naiveExecutionGraphForTest(store, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.ExecutionGraph("project-1", ExecutionGraphFilter{})
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("memoized graph changed output\nwant: %s\n got: %s", wantJSON, gotJSON)
	}
	var shared *ExecutionGraphNode
	for i := range got.Nodes {
		if got.Nodes[i].ExecutionID == child.ExecutionID {
			shared = &got.Nodes[i]
			break
		}
	}
	if shared == nil || !shared.PartialVisibility {
		t.Fatalf("diamond child did not retain degraded visibility: %#v", got.Nodes)
	}
	incoming := 0
	for _, edge := range got.Edges {
		if edge.ChildExecutionID == child.ExecutionID {
			incoming++
		}
	}
	if incoming != 2 {
		t.Fatalf("fixture did not create a diamond: child incoming edges=%d edges=%#v", incoming, got.Edges)
	}
}

func TestExecutionGraphCycleFixtureDoesNotRecurse(t *testing.T) {
	store := executionLedgerStore(t)
	left, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "left", Source: "direct_codex", Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "right", Source: "direct_codex", Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`DROP TRIGGER execution_edges_validate_insert`); err != nil {
		t.Fatal(err)
	}
	for _, edge := range [][2]string{{left.ExecutionID, right.ExecutionID}, {right.ExecutionID, left.ExecutionID}} {
		if _, err := store.exec(`INSERT INTO execution_edges (parent_execution_id, child_execution_id, kind, created_at) VALUES (?, ?, ?, ?)`, edge[0], edge[1], string(ExecutionManagedChildOf), executionNow()); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ExecutionGraph("project-1", ExecutionGraphFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Nodes) != 2 || len(page.Edges) != 2 {
		t.Fatalf("cycle fixture graph=%#v", page)
	}
}

func naiveExecutionGraphForTest(store *RuntimeStore, projectID string) (ExecutionGraphPage, error) {
	page := ExecutionGraphPage{Schema: executionGraphSchema, Nodes: []ExecutionGraphNode{}, Edges: []ExecutionEdge{}}
	rows, err := store.query(`SELECT execution_id FROM execution_records WHERE project_id = ? ORDER BY created_at DESC, execution_id DESC`, projectID)
	if err != nil {
		return page, err
	}
	var executionIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return page, err
		}
		executionIDs = append(executionIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return page, err
	}
	if err := rows.Close(); err != nil {
		return page, err
	}

	// Deliberately memo-free: this is an independent projection oracle for the
	// production memoized walk. It recomputes shared descendants on each path.
	var projectNode func(string) (ExecutionGraphNode, error)
	projectNode = func(id string) (ExecutionGraphNode, error) {
		view, err := store.ExecutionView(id)
		if err != nil || view == nil {
			return ExecutionGraphNode{}, err
		}
		node := ExecutionGraphNode{ExecutionView: *view, ProviderOwned: view.NodeKind == ExecutionNodeProviderChild, ProviderCapabilities: []ProviderCapabilityFact{}, Diagnostics: []string{}, Controls: []ExecutionControlAvailability{}}
		obs, err := store.executionLatestObservation(view.ExecutionID)
		if err != nil {
			return node, err
		}
		if obs != nil {
			node.ProviderStatus, node.ProviderCapabilities, node.PartialVisibility = obs.status, obs.caps, obs.degraded
			if obs.reason != "" {
				node.Diagnostics = append(node.Diagnostics, obs.reason)
			}
		}
		if run, err := store.executionGraphRun(*view); err != nil {
			return node, err
		} else if run != nil {
			node.Lifecycle.LeaseState, node.Lifecycle.AttemptOutcome = run.LeaseState, run.AttemptOutcome
			node.Lifecycle.ProcessObserved = run.ProcessPID > 0
			node.Lifecycle.DeliveryState = "runtime"
		}
		node.Lifecycle.SessionRef, node.Lifecycle.ProviderStatus = view.SessionRef, node.ProviderStatus
		if node.Lifecycle.DeliveryState == "" {
			node.Lifecycle.DeliveryState = "unbound"
			if view.ProofEligible {
				node.Lifecycle.DeliveryState = "bound"
			}
		}
		childRows, err := store.query(`SELECT child_execution_id FROM execution_edges WHERE parent_execution_id = ? ORDER BY child_execution_id`, node.ExecutionID)
		if err != nil {
			return node, err
		}
		var childIDs []string
		for childRows.Next() {
			var childID string
			if err := childRows.Scan(&childID); err != nil {
				_ = childRows.Close()
				return node, err
			}
			childIDs = append(childIDs, childID)
		}
		if err := childRows.Err(); err != nil {
			_ = childRows.Close()
			return node, err
		}
		if err := childRows.Close(); err != nil {
			return node, err
		}
		for _, childID := range childIDs {
			child, err := projectNode(childID)
			if err != nil {
				return node, err
			}
			switch child.ProviderStatus {
			case "failed":
				node.FailedChildren++
				node.AttentionChildren++
			case "interrupt_requested", "unknown":
				node.AttentionChildren++
			case "starting", "running", "acknowledged":
				node.ActiveChildren++
			}
			if child.PartialVisibility {
				node.PartialVisibility = true
			}
		}
		if facts, err := store.ExecutionLifecycle(id); err != nil {
			return node, err
		} else {
			node.Lifecycle.DeliveryState, node.Lifecycle.AdmissionState, node.Lifecycle.ProcessState = facts.DeliveryState, facts.AdmissionState, facts.ProcessState
			node.Lifecycle.ProviderStatus, node.Lifecycle.OutcomeState, node.Lifecycle.SessionState = facts.ProviderState, facts.OutcomeState, facts.SessionState
			node.Lifecycle.ChildAttentionState, node.Lifecycle.DerivedPhase = facts.ChildAttentionState, facts.DerivedPhase
		}
		control, err := store.ExecutionControlAvailability(id)
		if err != nil {
			return node, err
		}
		node.Controls = append(node.Controls, control)
		return node, nil
	}

	all := make([]ExecutionGraphNode, 0, len(executionIDs))
	for _, id := range executionIDs {
		node, err := projectNode(id)
		if err != nil {
			return page, err
		}
		all = append(all, node)
	}
	limit := 100
	if limit > 250 {
		limit = 250
	}
	end := limit
	if end > len(all) {
		end = len(all)
	}
	page.Nodes = append(page.Nodes, all[:end]...)
	if end < len(all) {
		page.NextCursor = all[end-1].ExecutionID
	}
	ids := make(map[string]bool, len(page.Nodes))
	for _, node := range page.Nodes {
		ids[node.ExecutionID] = true
		page.Partial = page.Partial || node.PartialVisibility
	}
	edges, topologyPartial, err := store.executionGraphEdges(projectID, ids)
	if err != nil {
		return page, err
	}
	page.Edges, page.TopologyPartial = edges, topologyPartial
	return page, nil
}
