package main

import "testing"

func TestRunnerMigrationConformance(t *testing.T) {
	manifest, err := buildCapabilitiesManifest(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	acp, ok := capabilityCommandNamed(manifest.Commands, "acp")
	if !ok || containsString(acp.Subcommands, "install") || containsString(acp.Subcommands, "setup") {
		t.Fatalf("bundled ACP lifecycle remains advertised: %#v", acp)
	}
	for _, command := range []string{"acp install", "acp setup"} {
		if code, err := runInner(command, Args{}); err != nil || code == 0 {
			t.Fatalf("%s: code=%d err=%v", command, code, err)
		}
	}
}
