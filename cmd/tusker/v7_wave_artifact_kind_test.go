package main

import "testing"

func TestWaveArtifactsDoNotBorrowPromisedEvidenceKind(t *testing.T) {
	idx, _ := briefFixture()
	task := idx.Tasks["APP-T-0001"]
	task.Data["artifact_contract"] = map[string]any{"kind": "screenshot_set"}
	evidence := idx.Evidence["APP-T-0001"][0]
	evidence.Data["evidence_kind"] = "automated_test"
	idx.Evidence["APP-T-0001"] = []Note{evidence}
	artifacts := normalizeWaveArtifacts(idx, task)
	if len(artifacts) != 1 || artifacts[0].Kind != "diff_summary" {
		t.Fatalf("test evidence was relabeled as the promised screenshot: %#v", artifacts)
	}
	evidence.Data["evidence_kind"] = "unknown"
	if artifacts := normalizeWaveArtifacts(idx, task); len(artifacts) != 0 {
		t.Fatalf("unknown evidence borrowed the promised kind: %#v", artifacts)
	}
}
