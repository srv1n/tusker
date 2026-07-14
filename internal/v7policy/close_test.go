package v7policy

import "testing"

func TestDefaultClosePolicy(t *testing.T) {
	low := DefaultClosePolicy("low")
	if low.RequiredAcceptor != "reviewer_agent" || len(low.RequiredEvidence) != 0 {
		t.Fatalf("unexpected low-risk policy: %#v", low)
	}
	high := DefaultClosePolicy("high")
	if high.RequiredAcceptor != "reviewer_agent" || len(high.RequiredEvidence) != 0 {
		t.Fatalf("unexpected high-risk policy: %#v", high)
	}
	medium := DefaultClosePolicy("medium")
	if medium.RequiredAcceptor != "reviewer_agent" || len(medium.RequiredEvidence) != 0 {
		t.Fatalf("unexpected medium-risk policy: %#v", medium)
	}
	critical := DefaultClosePolicy("critical")
	if critical.RequiredAcceptor != "reviewer_agent" || len(critical.RequiredGates) != 0 {
		t.Fatalf("unexpected critical-risk policy: %#v", critical)
	}
}

func TestAcceptorAllowed(t *testing.T) {
	if !AcceptorAllowed("human:sarav", "human") {
		t.Fatal("expected human actor to satisfy human acceptor")
	}
	if AcceptorAllowed("reviewer:bot", "human") {
		t.Fatal("reviewer actor should not satisfy human acceptor")
	}
	if !AcceptorAllowed("reviewer:bot", "reviewer_agent") {
		t.Fatal("reviewer actor should satisfy reviewer_agent acceptor")
	}
}
