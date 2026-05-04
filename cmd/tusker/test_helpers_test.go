package main

import (
	"os"
	"reflect"
	"testing"
)

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertEqual(t *testing.T, expected, actual any, label string) {
	t.Helper()
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("%s: expected %#v, got %#v", label, expected, actual)
	}
}
