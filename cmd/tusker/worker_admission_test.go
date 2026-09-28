package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWeightDerivation(t *testing.T) {
	root := t.TempDir()
	for _, marker := range []string{"rust/Cargo.toml", "swift/Package.swift", "ios/App.xcodeproj"} {
		path := filepath.Join(root, marker)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(marker, ".xcodeproj") {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	dirs := heavyProjectDirs(root)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"rust/src/lib.go", 4}, {"rust", 4}, {".", 4}, {"ios/Sources/View.go", 4},
		{"swift/lib.go", 4}, {"other/file.rs", 4}, {"other/file.swift", 4}, {"docs/README.md", 1},
	} {
		note := Note{Data: map[string]any{"owned_paths": []string{tc.path}}}
		if got := taskAdmissionWeight(note, dirs); got != tc.want {
			t.Errorf("%s: weight %d, want %d", tc.path, got, tc.want)
		}
	}
	if got := taskAdmissionWeight(Note{Data: map[string]any{"weight": 2, "owned_paths": []string{"rust"}}}, dirs); got != 2 {
		t.Fatalf("explicit override: %d", got)
	}
}

func TestWeightAuthoring(t *testing.T) {
	for _, raw := range []string{"0", "9", "1.5", "heavy", ""} {
		if _, _, err := taskWeightArg(Args{"weight": raw}); err == nil {
			t.Errorf("accepted invalid weight %q", raw)
		}
	}
	if weight, set, err := taskWeightArg(Args{"weight": "8"}); err != nil || !set || weight != 8 {
		t.Fatalf("weight = %d, set=%t, err=%v", weight, set, err)
	}
	if _, set, err := taskWeightArg(Args{}); err != nil || set {
		t.Fatalf("optional weight set=%t, err=%v", set, err)
	}
}

func TestWeightAdmission(t *testing.T) {
	for _, tc := range []struct {
		used, active, next int
		held               bool
	}{
		{0, 0, 4, false}, {4, 1, 4, false}, {8, 2, 4, true},
		{4, 1, 1, false}, {7, 4, 1, false}, {8, 5, 1, true}, {8, 0, 8, false},
	} {
		if held := weightAdmissionReason(tc.used, tc.active, 8, tc.next) != ""; held != tc.held {
			t.Errorf("used=%d active=%d next=%d: held=%t", tc.used, tc.active, tc.next, held)
		}
		if got := weightAdmissionReason(tc.used, tc.active, 0, tc.next); got != "" {
			t.Fatalf("feature off: %s", got)
		}
	}
}

func TestLoadGate(t *testing.T) {
	load := func() (float64, error) { return 1.4, nil }
	pressure := func() (int, error) { return 0, nil }
	if got := loadGateReason(1.0, load, pressure); !strings.Contains(got, "load 1.4 per CPU") {
		t.Fatal(got)
	}
	if got := loadGateReason(2.0, load, func() (int, error) { return 2, nil }); !strings.Contains(got, "memory pressure") {
		t.Fatal(got)
	}
	if got := loadGateReason(2.0, load, pressure); got != "" {
		t.Fatal(got)
	}
	if got := loadGateReason(0, func() (float64, error) { t.Fatal("feature off read load"); return 0, nil }, pressure); got != "" {
		t.Fatal(got)
	}
}
