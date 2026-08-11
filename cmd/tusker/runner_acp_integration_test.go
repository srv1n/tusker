package main

import (
	"strings"
	"testing"
)

func TestCodexACPFactoryFailsClosedUntilProviderPlanIsWired(t *testing.T) {
	wf := Workflow{Runners: map[string]RunnerDefinition{
		"codex-acp": {
			Kind:                string(RunnerCodexACP),
			BundleRoot:          "/opt/tusker/acp/codex",
			ManifestPath:        "manifest.json",
			ManifestSHA256:      "sha256:" + strings.Repeat("a", 64),
			AdapterVersion:      "1.1.14",
			AuthSource:          string(CodexACPAuthChatGPTSession),
			AuthPrincipalSHA256: "sha256:" + strings.Repeat("b", 64),
		},
	}}
	_, _, err := runnerForName("codex-acp", wf)
	if err == nil || !strings.Contains(err.Error(), "not live-ready") {
		t.Fatalf("codex ACP factory must fail closed before verified provider plan wiring, got %v", err)
	}
}

func TestACPRunnerConcreteIdentityIsDistinct(t *testing.T) {
	if (&ACPRunner{runner: RunnerCodexACP}).Name() != RunnerCodexACP {
		t.Fatal("concrete Codex ACP transport lost its persisted runner identity")
	}
	if (&ACPRunner{}).Name() != RunnerACP {
		t.Fatal("generic ACP transport identity changed")
	}
}

func TestCodexACPWorkflowAdmissionRejectsSecretsAndMalformedIdentity(t *testing.T) {
	valid := RunnerDefinition{
		Kind:                string(RunnerCodexACP),
		BundleRoot:          "/opt/tusker/acp/codex",
		ManifestPath:        "manifest.json",
		ManifestSHA256:      "sha256:" + strings.Repeat("a", 64),
		AdapterVersion:      "1.1.14",
		AuthSource:          string(CodexACPAuthChatGPTSession),
		AuthPrincipalSHA256: "sha256:" + strings.Repeat("b", 64),
	}
	if err := validateRunnerDefinitions(Workflow{Runners: map[string]RunnerDefinition{"codex-acp": valid}}, "workflow.yaml"); err != nil {
		t.Fatalf("valid codex ACP admission was rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*RunnerDefinition)
		want   string
	}{
		{name: "raw secret", mutate: func(d *RunnerDefinition) { d.AuthPrincipalSHA256 = "sk-secret-must-not-persist" }, want: "non-secret canonical sha256"},
		{name: "unknown auth", mutate: func(d *RunnerDefinition) { d.AuthSource = "browser-magic" }, want: "auth_source is unsupported"},
		{name: "absolute manifest", mutate: func(d *RunnerDefinition) { d.ManifestPath = "/tmp/manifest.json" }, want: "bundle-relative"},
		{name: "relative root", mutate: func(d *RunnerDefinition) { d.BundleRoot = "bundles/codex" }, want: "canonical absolute"},
		{name: "bad manifest digest", mutate: func(d *RunnerDefinition) { d.ManifestSHA256 = "sha256:abc" }, want: "canonical sha256"},
		{name: "shell command", mutate: func(d *RunnerDefinition) { d.Command = "npx codex-acp" }, want: "does not accept command"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := valid
			test.mutate(&definition)
			err := validateRunnerDefinitions(Workflow{Runners: map[string]RunnerDefinition{"codex-acp": definition}}, "workflow.yaml")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v, want %q", err, test.want)
			}
		})
	}

	direct := RunnerDefinition{Kind: string(RunnerCodexExec), BundleRoot: valid.BundleRoot}
	if err := validateRunnerDefinitions(Workflow{Runners: map[string]RunnerDefinition{"codex-exec": direct}}, "workflow.yaml"); err == nil || !strings.Contains(err.Error(), "codex_acp-only") {
		t.Fatalf("direct runner accepted ACP-only admission fields: %v", err)
	}
}
