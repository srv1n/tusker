package reviewpacket

import (
	"strings"
	"testing"
)

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
