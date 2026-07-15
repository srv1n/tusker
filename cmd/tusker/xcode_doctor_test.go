package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestXcodeDoctorDetectsInfrastructureFailureSignatures(t *testing.T) {
	cases := []struct {
		fixture string
		finding string
	}{
		{"stale_build_db_lock.log", "stale_build_db_lock"},
		{"build_database_io_inconsistency.log", "build_database_io_inconsistency"},
		{"supplementary_outputs_corruption.log", "supplementary_outputs_corruption"},
	}
	for _, tc := range cases {
		t.Run(tc.finding, func(t *testing.T) {
			report, err := buildXcodeDoctorReport(Args{"log": filepath.Join("testdata", "xcode", tc.fixture)})
			if err != nil {
				t.Fatal(err)
			}
			assertEqual(t, xcodeClassificationInfrastructure, report.Classification, "classification")
			if !xcodeReportHasFinding(report, tc.finding) {
				t.Fatalf("expected finding %s, got %#v", tc.finding, report.Findings)
			}
		})
	}
}

func TestXcodeDoctorDetectsResultBundleInfrastructureSignature(t *testing.T) {
	report, err := buildXcodeDoctorReport(Args{"result-bundle": filepath.Join("testdata", "xcode", "ResultBundle.xcresult")})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, xcodeClassificationInfrastructure, report.Classification, "classification")
	if !xcodeReportHasFinding(report, "supplementary_outputs_corruption") {
		t.Fatalf("expected supplementary outputs finding, got %#v", report.Findings)
	}
	if !containsPathSuffix(report.InspectPaths, filepath.Join("ResultBundle.xcresult", "logs", "build.log")) {
		t.Fatalf("result bundle file was not listed as inspected: %#v", report.InspectPaths)
	}
}

func TestXcodeDoctorClassifiesLikelyCodeAndUnknown(t *testing.T) {
	codeReport, err := buildXcodeDoctorReport(Args{"log": filepath.Join("testdata", "xcode", "code_failure.log")})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, xcodeClassificationCode, codeReport.Classification, "code classification")

	unknownReport, err := buildXcodeDoctorReport(Args{"log": filepath.Join("testdata", "xcode", "unknown.log")})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, xcodeClassificationUnknown, unknownReport.Classification, "unknown classification")
}

func TestXcodeDoctorDryRunListsInspectAndCleanupTargets(t *testing.T) {
	root, project := makeXcodeFixtureTree(t, "Foo")
	target := filepath.Join(root, "Foo-a1b2c3", "Build", "Intermediates.noindex", "XCBuildData")
	output := captureStdout(t, func() {
		err := xcodeDoctorCmd(Args{
			"project":           project,
			"derived-data-root": root,
			"log":               filepath.Join("testdata", "xcode", "stale_build_db_lock.log"),
			"dry-run":           "true",
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{
		"classification: likely_infrastructure",
		"dry-run: true",
		filepath.Join("testdata", "xcode", "stale_build_db_lock.log"),
		target,
		"would_remove",
		"proof recipe:",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("dry-run output missing %q:\n%s", expected, output)
		}
	}
}

func TestXcodeCleanupRemovesOnlyScopedXCBuildData(t *testing.T) {
	root, project := makeXcodeFixtureTree(t, "Foo")
	scopedTarget := filepath.Join(root, "Foo-a1b2c3", "Build", "Intermediates.noindex", "XCBuildData")
	unrelatedTarget := filepath.Join(root, "Bar-a1b2c3", "Build", "Intermediates.noindex", "XCBuildData")
	scopedObject := filepath.Join(root, "Foo-a1b2c3", "Build", "Products", "Debug", "App.app")
	assertExists(t, filepath.Join(scopedTarget, "build.db"))
	assertExists(t, filepath.Join(unrelatedTarget, "build.db"))
	assertExists(t, scopedObject)

	if err := xcodeDoctorCmd(Args{"project": project, "derived-data-root": root, "cleanup": "true"}); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, scopedTarget)
	assertExists(t, filepath.Join(unrelatedTarget, "build.db"))
	assertExists(t, scopedObject)
}

func TestXcodeCleanupDryRunParityLeavesScopedXCBuildData(t *testing.T) {
	root, project := makeXcodeFixtureTree(t, "Foo")
	scopedTarget := filepath.Join(root, "Foo-a1b2c3", "Build", "Intermediates.noindex", "XCBuildData")
	if err := xcodeDoctorCmd(Args{"project": project, "derived-data-root": root, "cleanup": "true", "dry-run": "true"}); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(scopedTarget, "build.db"))
}

func TestXcodeCleanupRefusesBroadOrAmbiguousPaths(t *testing.T) {
	root, project := makeXcodeFixtureTree(t, "Foo")
	for name, args := range map[string]Args{
		"no scope": {
			"derived-data-root": root,
			"cleanup":           "true",
		},
		"global root as exact derived data": {
			"project":      project,
			"derived-data": root,
			"cleanup":      "true",
		},
		"wrong project entry": {
			"project":      project,
			"derived-data": filepath.Join(root, "Bar-a1b2c3"),
			"cleanup":      "true",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := buildXcodeDoctorReport(args)
			if err == nil {
				t.Fatal("expected refusal")
			}
			if issue := errorToIssue(err); issue.Code != errorInvalidArg {
				t.Fatalf("expected INVALID_ARG, got %#v", issue)
			}
		})
	}
}

func TestXcodeDoctorHelpDocumentsProofGuardrail(t *testing.T) {
	output := captureStdout(t, printXcodeHelp)
	for _, expected := range []string{
		"tusker xcode doctor",
		"likely_infrastructure",
		"supplementaryOutputs",
		"Do not claim code validation",
		"rerun the original xcodebuild command",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("xcode help missing %q:\n%s", expected, output)
		}
	}
	guidance, err := readText(filepath.Join("..", "..", "skills", "tusker", "references", "XCODE_BUILD_STATE.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"tusker xcode doctor",
		"likely_infrastructure",
		"Do not record this as build-green proof",
	} {
		if !strings.Contains(guidance, expected) {
			t.Fatalf("operator guidance missing %q:\n%s", expected, guidance)
		}
	}
}

func makeXcodeFixtureTree(t *testing.T, projectName string) (string, string) {
	t.Helper()
	tempDir := t.TempDir()
	project := filepath.Join(tempDir, projectName+".xcodeproj")
	if err := ensureDir(project); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tempDir, "DerivedData")
	for _, app := range []string{"Foo-a1b2c3", "Bar-a1b2c3"} {
		if err := writeText(filepath.Join(root, app, "Build", "Intermediates.noindex", "XCBuildData", "build.db"), "db"); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeText(filepath.Join(root, "Foo-a1b2c3", "Build", "Products", "Debug", "App.app"), "generated product"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(root, "Foo-a1b2c3", "Logs", "Build", "recent.log"), "build database internal inconsistency"); err != nil {
		t.Fatal(err)
	}
	return root, project
}

func xcodeReportHasFinding(report xcodeDoctorReport, id string) bool {
	for _, finding := range report.Findings {
		if finding.ID == id {
			return true
		}
	}
	return false
}

func containsPathSuffix(paths []string, suffix string) bool {
	suffix = filepath.ToSlash(suffix)
	for _, path := range paths {
		if strings.HasSuffix(filepath.ToSlash(path), suffix) {
			return true
		}
	}
	return false
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if fileExists(path) || dirExists(path) {
		t.Fatalf("expected %s to be removed", path)
	}
}
