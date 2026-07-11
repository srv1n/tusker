package main

import "testing"

func TestSentinelProcessIdentitySharesOneProbePerRunPerTick(t *testing.T) {
	probes := 0
	d := &Daemon{processIdentityProbe: func(RunStatus) bool {
		probes++
		return true
	}}
	run := RunStatus{ProcessPID: 42, ProcessPGID: 42, ProcessStartedAt: "2026-07-11T12:00:00Z"}
	restore := d.beginPollProcessIdentityCache()
	if !d.processIdentityMatchesForPoll(run) {
		t.Fatal("reconcile probe unexpectedly failed")
	}
	if !d.processIdentityMatchesForPoll(run) {
		t.Fatal("sentinel reuse unexpectedly failed")
	}
	if probes != 1 {
		t.Fatalf("reconcile plus sentinel used %d identity probes, want 1", probes)
	}
	secondIdentity := run
	secondIdentity.ProcessStartedAt = "2026-07-11T12:00:01Z"
	if !d.processIdentityMatchesForPoll(secondIdentity) || probes != 2 {
		t.Fatalf("distinct process identity did not get its own probe: probes=%d", probes)
	}
	restore()
	if !d.processIdentityMatchesForPoll(run) || probes != 3 {
		t.Fatalf("next tick reused stale identity result: probes=%d", probes)
	}
}
