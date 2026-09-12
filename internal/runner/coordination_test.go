package runner

import "testing"

func TestAgentTransportDoesNotInferActiveDeliveryFromResume(t *testing.T) {
	c := CoordinationCapabilities{Endpoint: "codex-cli", Version: "installed", Evidence: "probed", ResumeIdle: true, CaptureReply: true}
	if !c.Can("resume") || c.Can("active-delivery") {
		t.Fatalf("capability gate failed: %#v", c)
	}
}
