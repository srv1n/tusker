package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestACPAdapterNPMPlatformMappingMatchesHost(t *testing.T) {
	candidates, err := acpAdapterNPMPlatformCandidates()
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate, "@openai/codex-") || !strings.Contains(candidate, runtime.GOOS) {
			t.Fatalf("platform candidate %q does not match host %s/%s", candidate, runtime.GOOS, runtime.GOARCH)
		}
	}
}
