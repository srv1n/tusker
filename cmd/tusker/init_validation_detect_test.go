package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitDetectsLandingGateCommand(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"go keeps built-in gate", map[string]string{"go.mod": "module x\n", "test_x.py": ""}, ""},
		{"npm test script", map[string]string{"package.json": `{"scripts":{"test":"vitest"}}`}, "npm test"},
		{"package without test script", map[string]string{"package.json": `{"name":"test"}`}, ""},
		{"npm init placeholder", map[string]string{"package.json": `{"scripts":{"test":"echo \\"Error: no test specified\\" && exit 1"}}`}, ""},
		{"cargo", map[string]string{"Cargo.toml": "[package]\n"}, "cargo test"},
		{"make test target", map[string]string{"Makefile": "build:\n\ttrue\ntest:\n\ttrue\n"}, "make test"},
		{"pytest config", map[string]string{"pyproject.toml": "[tool.pytest.ini_options]\n"}, "python3 -m pytest"},
		{"root unittest", map[string]string{"test_app.py": ""}, "python3 -m unittest"},
		{"tests dir unittest", map[string]string{"tests/test_app.py": ""}, "python3 -m unittest discover -s tests"},
		{"nothing", map[string]string{"README.md": ""}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			for name, body := range tc.files {
				path := filepath.Join(repo, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			vault := filepath.Join(repo, ".tusker")
			if err := os.MkdirAll(vault, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := writeDefaultTuskerConfig(vault); err != nil {
				t.Fatal(err)
			}
			commands := backpressureCommands(vault)
			if tc.want == "" {
				if strings.Join(commands, "|") != "go build ./...|go vet ./...|go test ./... -count=1" {
					t.Fatalf("expected built-in Go gate, got %q", commands)
				}
				return
			}
			if len(commands) != 1 || commands[0] != tc.want {
				t.Fatalf("expected gate %q, got %q", tc.want, commands)
			}
		})
	}
}
