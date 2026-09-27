package main

import (
	"errors"
	"fmt"
	"testing"
)

func TestDaemonPollErrorIsFatalOnlyForUntypedErrors(t *testing.T) {
	projectErr := fmt.Errorf("poll: %w", tuskerError(errorNotFound, "armed-wave integration task is missing"))
	if daemonPollErrorIsFatal(nil) || daemonPollErrorIsFatal(projectErr) {
		t.Fatal("a typed project error must not stop the daemon")
	}
	if !daemonPollErrorIsFatal(errors.New("database is locked")) {
		t.Fatal("an untyped storage error must stop the daemon")
	}
}
