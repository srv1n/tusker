package main

import (
	"net/http"
	"sort"
)

// ServeCapability describes whether a UI surface is authoritative and whether
// it is safe to expose controls for it. Keep this registry deliberately static:
// it is a compatibility contract between the bundled UI and daemon.
type serveCapability struct {
	ID          string `json:"id"`
	Class       string `json:"class"`
	Mutable     bool   `json:"mutable"`
	Description string `json:"description"`
}

var serveCapabilityRegistry = []serveCapability{
	{ID: "projects", Class: "authoritative_mutable", Mutable: true, Description: "Registered project and automation controls."},
	{ID: "config", Class: "authoritative_read_only", Description: "Effective configuration values with source provenance."},
	{ID: "setup", Class: "authoritative_mutable", Mutable: true, Description: "Setup doctor reports and guarded repair."},
	{ID: "tasks", Class: "authoritative_mutable", Mutable: true, Description: "Task lifecycle and dispatch actions."},
	{ID: "epics", Class: "authoritative_read_only", Description: "Vault epic projection."},
	{ID: "waves", Class: "authoritative_mutable", Mutable: true, Description: "Wave review and landing actions."},
	{ID: "runs", Class: "authoritative_mutable", Mutable: true, Description: "Run controls and canonical runtime readback."},
	{ID: "attempts", Class: "authoritative_read_only", Description: "Runtime attempt history."},
	{ID: "gates", Class: "authoritative_mutable", Mutable: true, Description: "Gate decisions and evidence actions."},
	{ID: "evidence", Class: "authoritative_mutable", Mutable: true, Description: "Durable evidence records."},
	{ID: "feedback", Class: "authoritative_mutable", Mutable: true, Description: "Durable operator feedback."},
	{ID: "daemon", Class: "authoritative_mutable", Mutable: true, Description: "Daemon lifecycle and limits."},
	{ID: "factory-operations", Class: "authoritative_read_only", Description: "Read-only control-plane projection."},
	{ID: "docs", Class: "authoritative_read_only", Description: "Vault document reads; legacy editor writes are unavailable."},
	{ID: "docgraph", Class: "authoritative_mutable", Mutable: true, Description: "CAS-protected knowledge document editing."},
	{ID: "decisions", Class: "authoritative_read_only", Description: "Decision records."},
	{ID: "roster", Class: "authoritative_read_only", Description: "Runner and handoff projection."},
	{ID: "delivery", Class: "authoritative_mutable", Mutable: true, Description: "Guarded delivery review and start."},
	{ID: "executions", Class: "authoritative_mutable", Mutable: true, Description: "Execution lineage and guarded binding."},
	{ID: "stream", Class: "cached_projection", Description: "Live invalidation hints; reconnect/read APIs remain authoritative."},
	{ID: "app-preferences", Class: "local_preference", Description: "Browser/native preferences, not daemon state."},
	{ID: "profiles", Class: "unavailable", Description: "Runner profile persistence is not exposed by Serve."},
}

func serveCapabilities() []serveCapability {
	result := append([]serveCapability(nil), serveCapabilityRegistry...)
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *serveServer) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		serveJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "capabilities are read-only"})
		return
	}
	serveJSON(w, http.StatusOK, map[string]any{"schema": "tusker.serve-capabilities/v1", "capabilities": serveCapabilities()})
}
