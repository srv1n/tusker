package main

import (
	"context"
	"strings"
	"testing"
)

func TestTrustAdapterContract(t *testing.T) {
	t.Run("generic ACP publishes only implemented capabilities", func(t *testing.T) {
		runner := &ACPRunner{}
		caps := runner.Capabilities()
		if !caps.StructuredEvents || !caps.ExplicitApprovals || !caps.Heartbeats || !caps.MachineFinalStatus {
			t.Fatalf("implemented ACP transport capabilities missing: %#v", caps)
		}
		if caps.ResumeSession || caps.UsageMetrics || caps.ArtifactEnumeration {
			t.Fatalf("ACP advertised unavailable behavior: %#v", caps)
		}
		if _, err := runner.Resume(context.Background(), ResumeRequest{SessionRef: "acp:v1:fake:session"}); err == nil || !strings.Contains(err.Error(), "unavailable") {
			t.Fatalf("ACP unsupported resume was not explicit: %v", err)
		}
		reconciled, err := runner.Reconcile(context.Background(), ReconcileRequest{SessionRef: "acp:v1:fake:session"})
		if err != nil || reconciled.LeaseState != LeaseStateReleased || reconciled.Outcome != AttemptOutcomeAbandoned {
			t.Fatalf("lost ACP process invented a resume: %#v err=%v", reconciled, err)
		}
	})

	t.Run("available runner declarations distinguish local, cloud, and unsupported usage", func(t *testing.T) {
		if !(&CodexExecRunner{}).Capabilities().ResumeSession {
			t.Fatal("codex exec omitted its implemented session continuation")
		}
		cloud := (&CodexCloudRunner{}).Capabilities()
		if cloud.ResumeSession || cloud.UsageMetrics || cloud.Heartbeats {
			t.Fatalf("cloud runner advertised reachability it does not implement: %#v", cloud)
		}
		if (&ACPRunner{}).Capabilities().UsageMetrics {
			t.Fatal("unknown ACP usage was represented as measured usage")
		}
	})
}
