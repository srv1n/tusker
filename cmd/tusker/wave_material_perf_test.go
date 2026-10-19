package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const waveMaterialPerfTaskBody = `# Task

## Outcome

Ship the thing.

## Acceptance

| ID | Criterion | Result | Notes |
|---|---|---|---|
| A1 | it works | pass | ok |
| A2 | it is fast | | |

## Verification

| Covers | Command | Result |
|---|---|---|
| A1 | go test ./... | pass |

` + "```" + `
## not a heading inside a fence
` + "```" + `

## Evidence

- ledger entry that must not change the contract

## Work log

- did stuff
`

// waveMaterialPerfFixture builds a repo with a managed spec, a plain spec
// file, and a wave of n tasks carrying every field the material canon reads.
func waveMaterialPerfFixture(t testing.TB, n int) (string, v7Index, Note) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/system/00-overview.md", "---\nsubject: overview\n---\n\n# Overview\n")
	write(".tusker/specs/perf-spec.md", "---\nsubject: perf-spec\ntitle: Perf\npart_of: overview\n---\n\n# Perf\n\n## Scope\n\nBody.\n")
	write("docs/specs/plain.md", "# Plain\n")
	vault := filepath.Join(root, ".tusker")
	idx := v7Index{Tasks: map[string]Note{}, Gates: map[string]Note{}, Waves: map[string]Note{}}
	var members []string
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("TSK-T-%04d", i+1)
		members = append(members, id)
		data := map[string]any{
			"id": id, "title": "task " + id, "epic": "EP-1", "risk": "medium",
			"dependencies":         []any{"TSK-T-9999"},
			"spec_refs":            []any{"perf-spec", "docs/specs/plain.md"},
			"artifact_contract":    map[string]any{"kind": "code", "paths": []any{"cmd/a.go", "cmd/b.go"}},
			"proof_mode":           "tests",
			"proof_required":       []any{"unit", "lint"},
			"proof_required_owner": map[string]any{"unit": "agent"},
			"evidence_budget":      3,
			"evidence_required":    []any{"log"},
			"gates":                []any{"G-0001"},
			"work_level":           "standard", "review_level": "deep", "review_reason": "risky",
			"owned_paths": []any{"cmd/a.go"}, "generated_outputs": []any{"gen/out.txt"},
			"runner_profile": "default", "complexity": "m", "execute_profile": "x", "review_profile": "y",
			"dependency_contracts":  map[string]any{"TSK-T-9999": "sha256:abc"},
			"close_policy_snapshot": map[string]any{"mode": "review"},
		}
		if i%2 == 1 {
			data["contract_fingerprint"] = "pinned"
		}
		idx.Tasks[id] = Note{Data: data, Body: waveMaterialPerfTaskBody}
	}
	idx.Gates["G-0001"] = Note{Data: map[string]any{"id": "G-0001", "gate_kind": "human", "owner": "sarav", "blocking": true, "blocks": []any{members[0]}, "action": "approve"}}
	wave := Note{Data: map[string]any{"id": "W-0001", "outcome": "fast", "integration_base_sha": "deadbeef", "members": members, "spec_refs": []any{"perf-spec"}}}
	idx.Waves["W-0001"] = wave
	return vault, idx, wave
}

// TestWaveMaterialFingerprintFormulaPinned pins fingerprint bytes. The wave
// value was re-pinned once when spec text left the material (M5). Armed waves store authorization_fingerprint, so a
// formula drift would stale every armed wave.
func TestWaveMaterialFingerprintFormulaPinned(t *testing.T) {
	task := Note{Data: map[string]any{"title": "x", "proof_required": []any{"b", "a"}, "evidence_budget": 2, "artifact_contract": map[string]any{"k": "v"}}, Body: waveMaterialPerfTaskBody}
	const wantContract = "sha256:944d3aa1bea3e67a595d188b1e17d1741fcdc9b742f4503ea1fd87b53fa3911b"
	if got := directWaveTaskContractFingerprint(task.Data, task.Body); got != wantContract {
		t.Fatalf("contract fingerprint drifted: %s", got)
	}
	// Memoized path must agree on a second call.
	if got := directWaveTaskContractFingerprint(task.Data, task.Body); got != wantContract {
		t.Fatalf("memoized contract fingerprint drifted: %s", got)
	}
	vault, idx, wave := waveMaterialPerfFixture(t, 4)
	const wantWave = "sha256:02d4ddc5a6b531d6805335960226f89cac5bb59af7c6602f012073bb70e3ad7a"
	for i := 0; i < 2; i++ {
		got, issues := waveMaterialFingerprint(vault, idx, wave)
		if got != wantWave || len(issues) != 0 {
			t.Fatalf("wave fingerprint drifted (pass %d): %s %#v", i, got, issues)
		}
	}
}

func BenchmarkWaveMaterialFingerprint(b *testing.B) {
	vault, idx, wave := waveMaterialPerfFixture(b, 20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A fresh read context per iteration mirrors serve/daemon requests.
		if _, issues := waveMaterialFingerprintWithContext(vault, idx, wave, newWaveMaterialReadContext()); len(issues) != 0 {
			b.Fatal(issues)
		}
	}
}
