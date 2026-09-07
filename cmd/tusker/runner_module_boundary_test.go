package main

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunnerModuleBoundary(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "internal", "runner", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("runner module files: %v %v", files, err)
	}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			name, _ := strconv.Unquote(spec.Path.Value)
			if strings.Contains(name, "cmd/tusker") || strings.Contains(name, "internal/v7schema") {
				t.Errorf("%s imports orchestration package %s", path, name)
			}
		}
	}
}
