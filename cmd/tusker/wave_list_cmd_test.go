package main

import (
	"strings"
	"testing"
)

func TestWaveListCmd(t *testing.T) {
	vault, _, project := autonomousWaveFixture(t, []string{"APP-T-0001"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	output := captureStdout(t, func() {
		if err := waveListCmd(Args{"project": project.ProjectID}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "W-0001") || !strings.Contains(output, "tasks=1 done=0") || !strings.Contains(output, "authorization=disarmed") {
		t.Fatalf("wave list: %q", output)
	}
}
