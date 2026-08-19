package reviewpacket

import (
	"reflect"
	"strings"
	"testing"
)

func TestAnalyzeEvents(t *testing.T) {
	events := ParseEvents(strings.Join([]string{
		`{"kind":"file_change","payload":{"path":"cmd/tusker/main.go","insertions":3,"deletions":1}}`,
		`{"kind":"verification","at":"2026-08-12T00:00:00Z","payload":{"command":"go test ./...","result":"pass","turn_id":"turn-1","session_ref":"session-1"}}`,
		`{"kind":"validation_failed","payload":{"validation_command":"tusker validate","result":"failed","error":"token=secret-value"}}`,
	}, "\n"))
	facts := AnalyzeEvents(events)
	assertStrings(t, facts.ChangedFiles, []string{"`cmd/tusker/main.go` (event:file_change)"})
	assertStrings(t, facts.DiffSummary, []string{"`cmd/tusker/main.go` +3 -1 (event:file_change)"})
	assertStrings(t, facts.VerificationCommands, []string{"`go test ./...` result=pass turn=turn-1 at=2026-08-12T00:00:00Z"})
	assertStrings(t, facts.SessionRefs, []string{"session-1"})
	assertStrings(t, facts.TurnIDs, []string{"turn-1"})
	assertStrings(t, facts.ValidationSummaries, []string{"`tusker validate` result=failed"})
	assertStrings(t, facts.OpenRisks, []string{"validation_failed: token=[redacted]"})
}

func TestRenderStablePacket(t *testing.T) {
	doc := Document{
		ItemID: "APP-T-0001", ItemTitle: "Review extraction",
		Run:   Run{RecordID: "APP-T-0001", AttemptID: "attempt-1", Runner: "codex", Lane: "execute", WorkRevision: 2, SessionRef: "session-1"},
		Turns: []Turn{{Index: 1, ID: "turn-1", SessionRef: "session-1", Status: "complete"}},
		Facts: Facts{ChangedFiles: []string{"`main.go` (M)"}, VerificationCommands: []string{"`go test ./...` result=pass"}},
	}
	got := Render(doc)
	for _, exact := range []string{
		"# Review packet\n",
		"- Item: APP-T-0001 - Review extraction\n",
		"- Attempt: attempt-1\n",
		"- Turns: 1\n",
		"- #1 `turn-1` session=session-1 status=complete last_event= error=none\n",
		"- Session refs: `session-1`\n",
		"- Turn ids: `turn-1`\n",
		"- `main.go` (M)\n",
		"- `go test ./...` result=pass\n",
		"- Reviewer must still check claims against the current tree before approval.\n",
	} {
		if !strings.Contains(got, exact) {
			t.Fatalf("packet missing %q:\n%s", exact, got)
		}
	}
	if !strings.HasSuffix(got, "- This packet summarizes daemon-observed runtime facts. It does not embed raw logs or full transcripts.\n") {
		t.Fatalf("unexpected packet suffix:\n%s", got)
	}
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
