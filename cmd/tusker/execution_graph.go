package main

// The execution graph is a read model.  It deliberately builds from the
// immutable ledger plus existing runtime/provider facts; no graph read ever
// calls a lifecycle or provider adapter method.

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
)

const executionGraphSchema = "tusker.execution-graph/v1"

type ExecutionGraphFilter struct {
	ExecutionID, RootID, ParentID, TaskID, WaveID, Source, Provider, ProviderID, AgentType, Binding, Lifecycle, Name, Attention string
	Limit                                                                                                                       int
	Cursor                                                                                                                      string
}

type ExecutionGraphNode struct {
	ExecutionView
	ProviderStatus       string                         `json:"provider_status"`
	ProviderCapabilities []ProviderCapabilityFact       `json:"provider_capabilities"`
	ProviderOwned        bool                           `json:"provider_owned"`
	Lifecycle            ExecutionLifecycleFacts        `json:"lifecycle"`
	ActiveChildren       int                            `json:"active_children"`
	FailedChildren       int                            `json:"failed_children"`
	AttentionChildren    int                            `json:"attention_children"`
	PartialVisibility    bool                           `json:"partial_visibility"`
	Diagnostics          []string                       `json:"diagnostics"`
	Controls             []ExecutionControlAvailability `json:"controls"`
}

// ExecutionLifecycleFacts intentionally keeps the dimensions separate.  A
// provider status is an observation, not a delivery outcome or a lease state.
type ExecutionLifecycleFacts struct {
	LeaseState          string `json:"lease_state"`
	AttemptOutcome      string `json:"attempt_outcome"`
	SessionRef          string `json:"session_ref"`
	ProviderStatus      string `json:"provider_status"`
	DeliveryState       string `json:"delivery_state"`
	ProcessObserved     bool   `json:"process_observed"`
	AdmissionState      string `json:"admission_state"`
	ProcessState        string `json:"process_state"`
	OutcomeState        string `json:"outcome_state"`
	SessionState        string `json:"session_state"`
	ChildAttentionState string `json:"child_attention_state"`
	DerivedPhase        string `json:"derived_phase"`
}

type ExecutionGraphPage struct {
	Schema          string               `json:"schema"`
	Nodes           []ExecutionGraphNode `json:"nodes"`
	Edges           []ExecutionEdge      `json:"edges"`
	NextCursor      string               `json:"next_cursor,omitempty"`
	Partial         bool                 `json:"partial_visibility"`
	TopologyPartial bool                 `json:"topology_partial"`
}

type executionObservation struct {
	status   string
	caps     []ProviderCapabilityFact
	degraded bool
	reason   string
}

func (s *RuntimeStore) ExecutionGraph(projectID string, filter ExecutionGraphFilter) (ExecutionGraphPage, error) {
	page := ExecutionGraphPage{Schema: executionGraphSchema, Nodes: []ExecutionGraphNode{}, Edges: []ExecutionEdge{}}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return page, tuskerError(errorInvalidArg, "execution graph requires project")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 250 {
		limit = 250
	}
	rows, err := s.query(`SELECT execution_id FROM execution_records WHERE project_id = ? ORDER BY created_at DESC, execution_id DESC`, projectID)
	if err != nil {
		return page, err
	}
	executionIDs := make([]string, 0)
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
	// RuntimeStore has one SQLite connection. The projection below performs
	// additional reads for every node, so retaining this cursor would make the
	// graph read wait on its own connection.
	if err := rows.Close(); err != nil {
		return page, err
	}
	all := make([]ExecutionGraphNode, 0)
	memo := make(map[string]ExecutionGraphNode, len(executionIDs))
	for _, id := range executionIDs {
		node, err := s.executionGraphNodeMemo(id, memo)
		if err != nil {
			return page, err
		}
		if executionGraphMatches(node, filter) {
			all = append(all, node)
		}
	}
	start := 0
	if cursor := strings.TrimSpace(filter.Cursor); cursor != "" {
		for start < len(all) && all[start].ExecutionID != cursor {
			start++
		}
		if start < len(all) {
			start++
		} else {
			return page, tuskerError(errorInvalidArg, "execution graph cursor is unknown")
		}
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	page.Nodes = append(page.Nodes, all[start:end]...)
	if end < len(all) {
		page.NextCursor = all[end-1].ExecutionID
	}
	ids := make(map[string]bool, len(page.Nodes))
	for _, node := range page.Nodes {
		ids[node.ExecutionID] = true
		page.Partial = page.Partial || node.PartialVisibility
	}
	edges, topologyPartial, err := s.executionGraphEdges(projectID, ids)
	if err != nil {
		return page, err
	}
	page.Edges = edges
	page.TopologyPartial = topologyPartial
	return page, nil
}

func executionGraphMatches(n ExecutionGraphNode, f ExecutionGraphFilter) bool {
	contains := func(value, want string) bool {
		return want == "" || strings.Contains(strings.ToLower(value), strings.ToLower(want))
	}
	// A task/wave lookup covers either immutable provenance or the current
	// audited binding. Direct work commonly has only the latter.
	if !contains(n.ExecutionID, strings.TrimSpace(f.ExecutionID)) || !contains(n.RootExecutionID, strings.TrimSpace(f.RootID)) || !contains(n.ParentExecutionID, strings.TrimSpace(f.ParentID)) || !matchesExecutionGraphAssociation(n.TaskID, n.BoundTaskID, f.TaskID) || !matchesExecutionGraphAssociation(n.WaveID, n.BoundWaveID, f.WaveID) || !contains(n.Source, strings.TrimSpace(f.Source)) || !contains(n.Provider, strings.TrimSpace(f.Provider)) || !contains(n.EffectiveProviderID(), strings.TrimSpace(f.ProviderID)) || !contains(n.AgentType, strings.TrimSpace(f.AgentType)) || !contains(n.EffectiveDisplayName, strings.TrimSpace(f.Name)) {
		return false
	}
	if lifecycle := strings.TrimSpace(f.Lifecycle); lifecycle != "" && !executionGraphLifecycleMatches(n, lifecycle) {
		return false
	}
	if binding := strings.TrimSpace(f.Binding); binding != "" && binding != n.BoundTaskID && binding != n.BoundWaveID {
		return false
	}
	if attention := strings.TrimSpace(f.Attention); attention != "" && attention != "all" {
		if attention == "true" && n.AttentionChildren == 0 {
			return false
		}
		if attention == "false" && n.AttentionChildren != 0 {
			return false
		}
	}
	return true
}

func executionGraphLifecycleMatches(n ExecutionGraphNode, filter string) bool {
	filter = strings.TrimSpace(filter)
	for _, value := range []string{
		n.ProviderStatus,
		n.Lifecycle.LeaseState,
		n.Lifecycle.AttemptOutcome,
		n.Lifecycle.ProviderStatus,
		n.Lifecycle.DeliveryState,
		n.Lifecycle.AdmissionState,
		n.Lifecycle.ProcessState,
		n.Lifecycle.OutcomeState,
		n.Lifecycle.SessionState,
		n.Lifecycle.ChildAttentionState,
		n.Lifecycle.DerivedPhase,
	} {
		if strings.EqualFold(strings.TrimSpace(value), filter) {
			return true
		}
	}
	return false
}

func matchesExecutionGraphAssociation(immutable, bound, filter string) bool {
	filter = strings.TrimSpace(filter)
	return filter == "" || strings.Contains(strings.ToLower(immutable), strings.ToLower(filter)) || strings.Contains(strings.ToLower(bound), strings.ToLower(filter))
}

func (n ExecutionGraphNode) EffectiveProviderID() string {
	if n.ProviderChildHandle != "" {
		return n.ProviderChildHandle
	}
	return n.ProviderSessionID
}

func (s *RuntimeStore) executionGraphNodeMemo(id string, memo map[string]ExecutionGraphNode) (ExecutionGraphNode, error) {
	if node, ok := memo[id]; ok {
		return node, nil
	}
	view, err := s.ExecutionView(id)
	if err != nil || view == nil {
		return ExecutionGraphNode{}, err
	}
	node := ExecutionGraphNode{ExecutionView: *view, ProviderOwned: view.NodeKind == ExecutionNodeProviderChild, ProviderCapabilities: []ProviderCapabilityFact{}, Diagnostics: []string{}, Controls: []ExecutionControlAvailability{}}
	obs, err := s.executionLatestObservation(view.ExecutionID)
	if err != nil {
		return node, err
	}
	if obs != nil {
		node.ProviderStatus, node.ProviderCapabilities, node.PartialVisibility = obs.status, obs.caps, obs.degraded
		if obs.reason != "" {
			node.Diagnostics = append(node.Diagnostics, obs.reason)
		}
	}
	if run, err := s.executionGraphRun(*view); err != nil {
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
	// Mark this node before walking children so a corrupt legacy edge cycle
	// returns the in-progress projection instead of recursing forever.
	memo[id] = node
	if err := s.executionGraphChildCounts(&node, memo); err != nil {
		delete(memo, id)
		return node, err
	}
	if facts, err := s.ExecutionLifecycle(id); err != nil {
		return node, err
	} else {
		node.Lifecycle.DeliveryState, node.Lifecycle.AdmissionState, node.Lifecycle.ProcessState = facts.DeliveryState, facts.AdmissionState, facts.ProcessState
		node.Lifecycle.ProviderStatus, node.Lifecycle.OutcomeState, node.Lifecycle.SessionState = facts.ProviderState, facts.OutcomeState, facts.SessionState
		node.Lifecycle.ChildAttentionState, node.Lifecycle.DerivedPhase = facts.ChildAttentionState, facts.DerivedPhase
	}
	if control, err := s.ExecutionControlAvailability(id); err == nil {
		node.Controls = append(node.Controls, control)
	} else {
		return node, err
	}
	memo[id] = node
	return node, nil
}

func (s *RuntimeStore) executionLatestObservation(executionID string) (*executionObservation, error) {
	var capsJSON, status, reason string
	var degraded int
	err := s.queryRowScan(`SELECT status, capabilities_json, degraded, degraded_reason FROM provider_execution_observations WHERE child_execution_id = ? OR (child_execution_id = '' AND parent_execution_id = ?) ORDER BY occurred_at DESC, source_sequence DESC, observation_id DESC LIMIT 1`, []any{executionID, executionID}, &status, &capsJSON, &degraded, &reason)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	obs := &executionObservation{status: status, degraded: degraded != 0, reason: reason}
	if err := json.Unmarshal([]byte(capsJSON), &obs.caps); err != nil {
		return nil, err
	}
	return obs, nil
}

func (s *RuntimeStore) executionGraphRun(view ExecutionView) (*RunStatus, error) {
	// A run row is mutable across retries. It may only be used to control an
	// execution when the immutable execution identity still names its active
	// attempt and exact lease generation. Task IDs are useful for grouping, but
	// are never an authority correlation key.
	if strings.TrimSpace(view.AttemptID) == "" || view.LeaseGeneration <= 0 {
		return nil, nil
	}
	return s.LookupRunForAttempt(view.ProjectID, view.AttemptID, view.LeaseGeneration)
}

func (s *RuntimeStore) executionGraphChildCounts(node *ExecutionGraphNode, memo map[string]ExecutionGraphNode) error {
	rows, err := s.query(`SELECT child_execution_id FROM execution_edges WHERE parent_execution_id = ? ORDER BY child_execution_id`, node.ExecutionID)
	if err != nil {
		return err
	}
	childIDs := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		childIDs = append(childIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range childIDs {
		child, err := s.executionGraphNodeMemo(id, memo)
		if err != nil {
			return err
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
	return nil
}

func (s *RuntimeStore) executionGraphEdges(projectID string, ids map[string]bool) ([]ExecutionEdge, bool, error) {
	rows, err := s.query(`SELECT e.parent_execution_id, e.child_execution_id, e.kind, e.created_at FROM execution_edges e JOIN execution_records p ON p.execution_id = e.parent_execution_id WHERE p.project_id = ? ORDER BY e.created_at, e.parent_execution_id, e.child_execution_id`, projectID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []ExecutionEdge{}
	topologyPartial := false
	for rows.Next() {
		var edge ExecutionEdge
		if err := rows.Scan(&edge.ParentExecutionID, &edge.ChildExecutionID, &edge.Kind, &edge.CreatedAt); err != nil {
			return nil, false, err
		}
		// Page edges are closed: an edge is emitted only when both endpoint
		// nodes are returned. Callers never have to guess at a missing node.
		if ids[edge.ParentExecutionID] && ids[edge.ChildExecutionID] {
			out = append(out, edge)
		} else if ids[edge.ParentExecutionID] || ids[edge.ChildExecutionID] {
			// Filtering and pagination may omit an ancestor or descendant. Keep
			// the page edge-closed, but make that loss explicit so consumers do
			// not promote an orphaned child to a root.
			topologyPartial = true
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		if out[i].ParentExecutionID != out[j].ParentExecutionID {
			return out[i].ParentExecutionID < out[j].ParentExecutionID
		}
		return out[i].ChildExecutionID < out[j].ChildExecutionID
	})
	return out, topologyPartial, rows.Err()
}
