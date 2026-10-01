package main

import "testing"

func TestRunnerMigrationConformance(t *testing.T) {
	manifest, err := buildCapabilitiesManifest(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"acp", "acp install", "acp setup"} {
		if acp, ok := capabilityCommandNamed(manifest.Commands, name); ok {
			t.Fatalf("bundled ACP lifecycle remains advertised: %#v", acp)
		}
	}
	for _, command := range []string{"acp install", "acp setup"} {
		if code, err := runInner(command, Args{}); err != nil || code == 0 {
			t.Fatalf("%s: code=%d err=%v", command, code, err)
		}
	}
}
