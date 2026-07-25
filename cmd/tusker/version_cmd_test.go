package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionCommandReportsBuildAndBinaryProvenance(t *testing.T) {
	executable := writeTempExecutable(t, "tusker-test-binary")
	original := buildVersion
	buildVersion = "v1.2.3-test"
	t.Cleanup(func() { buildVersion = original })

	projection := buildVersionProjection(&debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "1234567890abcdef"},
			{Key: "vcs.modified", Value: "true"},
			{Key: "vcs.time", Value: "2026-07-25T00:00:00Z"},
		},
	}, executable)

	assertEqual(t, versionSchema, projection.Schema, "version schema")
	assertEqual(t, "v1.2.3-test", projection.Version, "injected version")
	assertEqual(t, "1234567890abcdef", projection.Revision, "vcs revision")
	assertEqual(t, true, projection.Modified, "dirty provenance")
	assertEqual(t, "2026-07-25T00:00:00Z", projection.VCSTime, "vcs time")
	if len(projection.BinarySHA256) != 64 {
		t.Fatalf("binary digest = %q", projection.BinarySHA256)
	}
}

func TestVersionCommandRoutesFlagAndJSON(t *testing.T) {
	for _, argv := range [][]string{
		{"tusker", "version", "--json"},
		{"tusker", "--version", "--json"},
	} {
		command, args := parseCLI(argv)
		if command != argv[1] || !args.Bool("json") {
			t.Fatalf("parseCLI(%v) = command %q args %#v", argv, command, args)
		}
		output := captureStdout(t, func() {
			if _, err := runInner(command, args); err != nil {
				t.Fatal(err)
			}
		})
		for _, expected := range []string{`"ok":true`, `"schema":"` + versionSchema + `"`, `"binary_sha256":"`} {
			if !strings.Contains(output, expected) {
				t.Fatalf("version output missing %q:\n%s", expected, output)
			}
		}
	}
}

func writeTempExecutable(t *testing.T, contents string) string {
	t.Helper()
	path := t.TempDir() + "/tusker"
	if err := writeText(path, contents); err != nil {
		t.Fatal(err)
	}
	return path
}
