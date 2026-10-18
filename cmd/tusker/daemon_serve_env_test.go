package main

import (
	"os"
	"testing"
)

func TestLatchDaemonServeRequiredRemovesEnvFromChildren(t *testing.T) {
	old := daemonServeRequired
	defer func() { daemonServeRequired = old }()
	daemonServeRequired = false
	t.Setenv(daemonServeRequiredEnv, "1")
	latchDaemonServeRequired()
	if !daemonServeRequired {
		t.Fatal("serve requirement was not latched")
	}
	if _, set := os.LookupEnv(daemonServeRequiredEnv); set {
		t.Fatal("child processes would still inherit TUSKER_SERVE_REQUIRED")
	}
}
