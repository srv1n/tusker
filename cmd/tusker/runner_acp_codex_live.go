package main

// This file is the concrete Codex ACP admission seam.  It deliberately keeps
// the installed-bundle policy, the non-secret auth selection, and the exact
// provider configuration together so a generic ACP process cannot be started
// from a bare command string.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"tusker/internal/acp"
)

const codexACPProviderPlanSchema = "tusker.codex-acp-provider-plan/v1"

// CodexACPProviderPlan is copied into the private wrapper request.  It is
// intentionally serializable and contains no credential value, function, or
// inherited environment.  The wrapper resolves exactly its selected auth
// source from its own inherited environment after it has revalidated this
// bundle receipt and the current durable run identity.
type CodexACPProviderPlan struct {
	Schema              string                              `json:"schema"`
	BundleRoot          string                              `json:"bundle_root"`
	ManifestPath        string                              `json:"manifest_path"`
	ManifestSHA256      string                              `json:"manifest_sha256"`
	AdapterVersion      string                              `json:"adapter_version"`
	AuthSource          string                              `json:"auth_source"`
	AuthPrincipalSHA256 string                              `json:"auth_principal_sha256"`
	Model               string                              `json:"model"`
	Effort              string                              `json:"effort,omitempty"`
	Mode                CodexACPMode                        `json:"mode"`
	BundleReceipt       ACPAdapterBundleVerificationReceipt `json:"bundle_receipt"`
}

func newCodexACPProviderAdmission(definition RunnerDefinition) (CodexACPProviderPlan, error) {
	if err := validateCodexACPDefinition(definition); err != nil {
		return CodexACPProviderPlan{}, err
	}
	plan := CodexACPProviderPlan{
		Schema:              codexACPProviderPlanSchema,
		BundleRoot:          definition.BundleRoot,
		ManifestPath:        definition.ManifestPath,
		ManifestSHA256:      definition.ManifestSHA256,
		AdapterVersion:      definition.AdapterVersion,
		AuthSource:          definition.AuthSource,
		AuthPrincipalSHA256: definition.AuthPrincipalSHA256,
		Mode:                CodexACPModeReadOnly,
	}
	receipt, err := ValidateACPAdapterBundle(plan.bundleRequest())
	if err != nil {
		return CodexACPProviderPlan{}, fmt.Errorf("validate codex ACP bundle before lease: %w", err)
	}
	plan.BundleReceipt = receipt
	return plan, nil
}

func validateCodexACPDefinition(definition RunnerDefinition) error {
	if RunnerName(strings.TrimSpace(definition.Kind)) != RunnerCodexACP {
		return errors.New("codex ACP admission requires the codex_acp runner kind")
	}
	if strings.TrimSpace(definition.Command) != "" {
		return errors.New("codex ACP admission refuses command strings")
	}
	if definition.BundleRoot != strings.TrimSpace(definition.BundleRoot) || !filepath.IsAbs(definition.BundleRoot) || filepath.Clean(definition.BundleRoot) != definition.BundleRoot {
		return errors.New("codex ACP admission requires an exact canonical bundle root")
	}
	if manifest, err := normalizeACPAdapterBundleRelative(definition.ManifestPath); err != nil || manifest != definition.ManifestPath {
		return errors.New("codex ACP admission requires a canonical bundle-relative manifest path")
	}
	if !validACPAdapterBundleDigest(definition.ManifestSHA256) || !validCodexACPAdapterVersion(definition.AdapterVersion) || !v7CloseAuthorityDigest(definition.AuthPrincipalSHA256, "sha256:") {
		return errors.New("codex ACP admission has invalid immutable identity fields")
	}
	switch CodexACPAuthSource(definition.AuthSource) {
	case CodexACPAuthChatGPTSession, CodexACPAuthCodexAPIKey, CodexACPAuthOpenAIAPIKey:
	default:
		return errors.New("codex ACP admission has an unsupported auth source")
	}
	return nil
}

func (p CodexACPProviderPlan) bundleRequest() ACPAdapterBundleValidationRequest {
	return ACPAdapterBundleValidationRequest{
		BundleRoot:             p.BundleRoot,
		ManifestPath:           p.ManifestPath,
		ExpectedManifestSHA256: p.ManifestSHA256,
		ExpectedDescriptor: ACPAdapterBundleDescriptorPolicy{
			Provider: codexACPProvider, Adapter: "codex-acp", Version: p.AdapterVersion, LaunchKind: ACPAdapterBundleLaunchNative,
		},
		ExpectedFinalRoot:        p.BundleRoot,
		TrustCurrentUserBoundary: true,
		ProviderAllowed: func(provider string) bool {
			return provider == codexACPProvider
		},
	}
}

func (p CodexACPProviderPlan) withProfile(profile ResolvedRunnerProfile) (CodexACPProviderPlan, error) {
	if strings.TrimSpace(profile.Definition.Command) != "" {
		return CodexACPProviderPlan{}, errors.New("codex ACP profiles cannot replace the pinned adapter command")
	}
	if strings.TrimSpace(profile.Definition.PermissionPreset) != "read-only" || strings.TrimSpace(profile.Definition.Sandbox.Mode) != "read-only" {
		return CodexACPProviderPlan{}, errors.New("codex ACP admits only an explicit read-only profile")
	}
	p.Model = strings.TrimSpace(profile.Definition.Model)
	p.Effort = strings.TrimSpace(profile.Definition.Effort)
	if _, _, err := p.descriptorAndArgv(); err != nil {
		return CodexACPProviderPlan{}, err
	}
	return p, nil
}

func (p CodexACPProviderPlan) descriptorAndArgv() (CodexACPDescriptor, []string, error) {
	if p.Schema != codexACPProviderPlanSchema || p.Mode != CodexACPModeReadOnly {
		return CodexACPDescriptor{}, nil, errors.New("invalid or non-read-only Codex ACP provider plan")
	}
	if err := validateCodexACPDefinition(RunnerDefinition{
		Kind: string(RunnerCodexACP), BundleRoot: p.BundleRoot, ManifestPath: p.ManifestPath, ManifestSHA256: p.ManifestSHA256,
		AdapterVersion: p.AdapterVersion, AuthSource: p.AuthSource, AuthPrincipalSHA256: p.AuthPrincipalSHA256,
	}); err != nil {
		return CodexACPDescriptor{}, nil, err
	}
	if p.BundleReceipt.Schema == "" || len(p.BundleReceipt.Assets) == 0 {
		return CodexACPDescriptor{}, nil, errors.New("Codex ACP provider plan has no verified bundle receipt")
	}
	// Never derive paths from a deserialized receipt before it has been checked
	// against the exact trusted bundle request. Apart from making the immediate
	// pre-spawn revalidation explicit here, this prevents malformed handoff JSON
	// from reaching descriptor's asset accounting.
	if err := RevalidateACPAdapterBundleReceipt(p.bundleRequest(), p.BundleReceipt); err != nil {
		return CodexACPDescriptor{}, nil, err
	}
	descriptor, err := p.descriptor()
	if err != nil {
		return CodexACPDescriptor{}, nil, err
	}
	argv, err := descriptor.LaunchArgv(p.bundleRequest(), p.BundleReceipt)
	if err != nil {
		return CodexACPDescriptor{}, nil, err
	}
	return descriptor, argv, nil
}

func (p CodexACPProviderPlan) descriptor() (CodexACPDescriptor, error) {
	if len(p.BundleReceipt.Assets) == 0 {
		return CodexACPDescriptor{}, errors.New("Codex ACP provider plan has no receipt assets")
	}
	if len(p.BundleReceipt.Argv) != 1 || !filepath.IsAbs(p.BundleReceipt.Argv[0]) {
		return CodexACPDescriptor{}, errors.New("Codex ACP provider plan does not contain a one-part native adapter receipt")
	}
	adapterPath := p.BundleReceipt.Argv[0]
	var adapter CodexACPImmutableAsset
	assets := make([]CodexACPImmutableAsset, 0, len(p.BundleReceipt.Assets)-1)
	for _, asset := range p.BundleReceipt.Assets {
		path := filepath.Join(p.BundleReceipt.BundleRoot, filepath.FromSlash(asset.Path))
		if path == adapterPath {
			if asset.Role != "executable" {
				return CodexACPDescriptor{}, errors.New("Codex ACP adapter receipt executable role drift")
			}
			adapter = CodexACPImmutableAsset{Path: path, Fingerprint: asset.SHA256}
			continue
		}
		assets = append(assets, CodexACPImmutableAsset{Path: path, Fingerprint: asset.SHA256})
	}
	if adapter.Path == "" {
		return CodexACPDescriptor{}, errors.New("Codex ACP adapter is missing from the verified receipt")
	}
	descriptor := CodexACPDescriptor{
		AdapterVersion: p.AdapterVersion, Adapter: adapter, Assets: assets,
		Model: p.Model, Effort: p.Effort, Mode: p.Mode,
	}
	fingerprint, err := descriptor.ManifestFingerprintForDescriptor()
	if err != nil {
		return CodexACPDescriptor{}, err
	}
	descriptor.ManifestFingerprint = fingerprint
	return descriptor, nil
}

func (p CodexACPProviderPlan) authContract() (CodexACPAuthContract, error) {
	auth := CodexACPAuthContract{Source: CodexACPAuthSource(p.AuthSource), PrincipalDigest: p.AuthPrincipalSHA256}
	if _, err := auth.environmentKey(); err != nil || !v7CloseAuthorityDigest(auth.PrincipalDigest, "sha256:") {
		return CodexACPAuthContract{}, errors.New("invalid Codex ACP non-secret auth contract")
	}
	return auth, nil
}

func (p CodexACPProviderPlan) wrapperEnvironment() (CodexACPDescriptor, []string, error) {
	descriptor, _, err := p.descriptorAndArgv()
	if err != nil {
		return CodexACPDescriptor{}, nil, err
	}
	auth, err := p.authContract()
	if err != nil {
		return CodexACPDescriptor{}, nil, err
	}
	// This is intentionally inside the wrapper process.  Only the selected
	// environment value crosses this process boundary, and it is never written
	// into the wrapper request, event log, raw log, or StartRequest.
	environment, err := descriptor.CodexACPEnvironment(os.Environ(), auth)
	if err != nil {
		return CodexACPDescriptor{}, nil, err
	}
	return descriptor, environment.Variables, nil
}

// CodexACPRunner remains a local-only provider identity.  It shares the
// generic ACP containment mechanics but never delegates admission to a bare
// ACPRunner: the plan is mandatory at every Start boundary.
type CodexACPRunner struct{ admission CodexACPProviderPlan }

func (r *CodexACPRunner) Name() RunnerName { return RunnerCodexACP }

func (r *CodexACPRunner) Capabilities() RunnerCapabilities {
	return (&ACPRunner{runner: RunnerCodexACP}).Capabilities()
}

func (r *CodexACPRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	if req.CodexACP == nil {
		return nil, tuskerError(errorConfigInvalid, "codex_acp start requires a verified provider plan")
	}
	expectedAdmission, err := r.expectedStartAdmission(req)
	if err != nil {
		return nil, err
	}
	if err := req.CodexACP.matchesAdmission(expectedAdmission); err != nil {
		return nil, err
	}
	if _, _, err := req.CodexACP.descriptorAndArgv(); err != nil {
		return nil, tuskerError(errorConfigInvalid, "codex_acp pre-spawn bundle verification failed: "+err.Error())
	}
	if err := validateACPLaunchRequestForRunner(RunnerCodexACP, req); err != nil {
		return nil, err
	}
	return startDetachedRunnerWrapper(ctx, RunnerCodexACP, req, nil, r.Capabilities())
}

// expectedStartAdmission binds the factory's immutable bundle/auth receipt to
// the exact profile projection that the durable dispatch request carries. The
// factory intentionally has no secret or workflow lookup; the detached child
// independently re-resolves this named profile from registered project config.
func (r *CodexACPRunner) expectedStartAdmission(req StartRequest) (CodexACPProviderPlan, error) {
	profile := strings.TrimSpace(req.RunnerProfile)
	model := strings.TrimSpace(req.RunnerModel)
	effort := strings.TrimSpace(req.RunnerEffort)
	if profile == "" || profile != req.RunnerProfile ||
		strings.TrimSpace(req.RunnerHarness) != string(RunnerCodexACP) || req.RunnerHarness != string(RunnerCodexACP) ||
		model == "" || model != req.RunnerModel || !validRunnerModelName(model) ||
		effort == "" || effort != req.RunnerEffort || !validRunnerEffort(effort) {
		return CodexACPProviderPlan{}, tuskerError(errorConfigInvalid, "codex_acp start requires exact non-empty read-only profile, harness, model, and effort fields")
	}
	expected := r.admission
	expected.Model = model
	expected.Effort = effort
	return expected, nil
}

func (r *CodexACPRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	return (&ACPRunner{runner: RunnerCodexACP}).Resume(ctx, req)
}

func (r *CodexACPRunner) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error) {
	return (&ACPRunner{runner: RunnerCodexACP}).Reconcile(ctx, req)
}

func (r *CodexACPRunner) Interrupt(ctx context.Context, req InterruptRequest) error {
	return (&ACPRunner{runner: RunnerCodexACP}).Interrupt(ctx, req)
}

func (r *CodexACPRunner) Collect(ctx context.Context, req CollectRequest) (*CollectResult, error) {
	return (&ACPRunner{runner: RunnerCodexACP}).Collect(ctx, req)
}

func (p CodexACPProviderPlan) matchesAdmission(admission CodexACPProviderPlan) error {
	// The whole receipt, not merely its aggregate digest, is a durable part of
	// admission. This is what prevents a wrapper JSON payload from selecting a
	// separately valid same-provider bundle after the factory made its choice.
	if !reflect.DeepEqual(p, admission) {
		return tuskerError(errorInvalidTransition, "codex_acp start plan no longer matches the complete factory-admitted bundle/auth/config identity")
	}
	return nil
}

func validateCodexACPWrapperRequest(req runnerWrapperRequest) error {
	if req.Resume != nil {
		return tuskerError(errorInvalidTransition, "codex_acp wrapper refuses resume until a provider-specific negotiated restore lane is implemented")
	}
	if req.Start.CodexACP == nil {
		return tuskerError(errorConfigInvalid, "codex_acp wrapper is missing its provider plan")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return fmt.Errorf("open runtime store for codex_acp wrapper identity: %w", err)
	}
	defer store.Close()
	run, err := findRunScopedOrAmbiguous(store, req.Start.ProjectID, req.Start.RecordID)
	if err != nil {
		return err
	}
	if run == nil || req.Runner != string(RunnerCodexACP) || run.Runner != string(RunnerCodexACP) || !codexACPWrapperOwnsRun(*run, req.Start) {
		return tuskerError(errorInvalidTransition, "codex_acp wrapper refuses a missing, replaced, or non-codex_acp leased run")
	}
	canonicalAdmission, err := canonicalCodexACPWrapperAdmission(store, *run, req.Start)
	if err != nil {
		return err
	}
	if err := req.Start.CodexACP.matchesAdmission(canonicalAdmission); err != nil {
		return err
	}
	if _, _, err := req.Start.CodexACP.descriptorAndArgv(); err != nil {
		return tuskerError(errorConfigInvalid, "codex_acp wrapper bundle revalidation failed: "+err.Error())
	}
	return nil
}

// codexACPWrapperOwnsRun intentionally strengthens the generic wrapper lease
// predicate. A Codex ACP handoff is valid only for the exact concrete run and
// its immutable execution selection; permissive empty legacy fields are not a
// safe compatibility mode for this opt-in provider.
func codexACPWrapperOwnsRun(run RunStatus, req StartRequest) bool {
	if strings.TrimSpace(req.ProjectID) == "" || req.ProjectID != run.ProjectID ||
		strings.TrimSpace(req.RecordID) == "" || req.RecordID != run.RecordID ||
		strings.TrimSpace(req.AttemptID) == "" || strings.TrimSpace(run.ActiveAttemptID) == "" || req.AttemptID != run.ActiveAttemptID ||
		strings.TrimSpace(run.LeaseOwner) == "" || req.AttemptID != run.LeaseOwner ||
		req.LeaseGeneration <= 0 || run.LeaseGeneration <= 0 || req.LeaseGeneration != run.LeaseGeneration ||
		req.WorkRevision <= 0 || run.WorkRevision <= 0 || req.WorkRevision != run.WorkRevision ||
		strings.TrimSpace(req.RunnerProfile) == "" || req.RunnerProfile != run.RunnerProfile ||
		strings.TrimSpace(req.RunnerHarness) != string(RunnerCodexACP) || req.RunnerHarness != run.RunnerHarness ||
		strings.TrimSpace(req.RunnerModel) == "" || req.RunnerModel != run.RunnerModel ||
		strings.TrimSpace(req.RunnerEffort) == "" || req.RunnerEffort != run.RunnerEffort ||
		strings.TrimSpace(req.WorkspacePath) == "" || strings.TrimSpace(run.WorkspacePath) == "" || !sameCanonicalProjectPath(req.WorkspacePath, run.WorkspacePath) {
		return false
	}
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateClaimed, LeaseStateRunning:
		return true
	default:
		return false
	}
}

// canonicalCodexACPWrapperAdmission rebuilds the expected non-secret plan
// from the registered project's effective workflow/config. The wrapper never
// treats its own serialized plan as authority for a bundle path or manifest.
func canonicalCodexACPWrapperAdmission(store *RuntimeStore, run RunStatus, req StartRequest) (CodexACPProviderPlan, error) {
	projects, err := store.ListProjects()
	if err != nil {
		return CodexACPProviderPlan{}, fmt.Errorf("list registered projects for codex_acp wrapper: %w", err)
	}
	var project *RegisteredProject
	for index := range projects {
		if projects[index].ProjectID == run.ProjectID {
			project = &projects[index]
			break
		}
	}
	if project == nil || !project.Enabled ||
		!sameCanonicalProjectPath(req.RepoRoot, project.RepoRoot) ||
		!sameCanonicalProjectPath(req.VaultPath, project.VaultRoot) ||
		!sameCanonicalProjectPath(project.WorkflowPath, workflowPath(project.VaultRoot)) {
		return CodexACPProviderPlan{}, tuskerError(errorInvalidTransition, "codex_acp wrapper refuses an unregistered or path-rebound project admission")
	}
	workflow, err := loadWorkflow(project.VaultRoot)
	if err != nil {
		return CodexACPProviderPlan{}, fmt.Errorf("load canonical codex_acp workflow admission: %w", err)
	}
	definition, err := uniqueCodexACPDefinition(workflow.Data.Runners)
	if err != nil {
		return CodexACPProviderPlan{}, err
	}
	profile, ok := workflow.Data.RunnerProfiles[run.RunnerProfile]
	if !ok || strings.TrimSpace(profile.Harness) != string(RunnerCodexACP) {
		return CodexACPProviderPlan{}, tuskerError(errorInvalidTransition, "codex_acp wrapper profile is absent or no longer bound to codex_acp")
	}
	admission, err := newCodexACPProviderAdmission(definition)
	if err != nil {
		return CodexACPProviderPlan{}, err
	}
	admission, err = admission.withProfile(ResolvedRunnerProfile{Name: run.RunnerProfile, Definition: profile})
	if err != nil {
		return CodexACPProviderPlan{}, err
	}
	if admission.Model != run.RunnerModel || admission.Effort != run.RunnerEffort {
		return CodexACPProviderPlan{}, tuskerError(errorInvalidTransition, "codex_acp wrapper run model or effort no longer matches canonical profile admission")
	}
	return admission, nil
}

func uniqueCodexACPDefinition(definitions map[string]RunnerDefinition) (RunnerDefinition, error) {
	var matches []RunnerDefinition
	for _, definition := range definitions {
		if RunnerName(strings.TrimSpace(definition.Kind)) == RunnerCodexACP {
			matches = append(matches, definition)
		}
	}
	if len(matches) != 1 {
		return RunnerDefinition{}, tuskerError(errorInvalidTransition, "codex_acp wrapper requires exactly one canonical codex_acp runner definition")
	}
	return matches[0], nil
}

func codexACPConfigOptions(options []acp.ConfigOption) []CodexACPAdvertisedConfigOption {
	out := make([]CodexACPAdvertisedConfigOption, 0, len(options))
	for _, option := range options {
		values := make([]string, 0, len(option.Options))
		for _, value := range option.Options {
			values = append(values, value.Value)
		}
		out = append(out, CodexACPAdvertisedConfigOption{ID: option.ID, Value: option.CurrentValue, AllowedValues: values})
	}
	return out
}

func applyCodexACPConfig(ctx context.Context, client *acp.Client, descriptor CodexACPDescriptor, session acp.Session) (CodexACPConfigPlan, error) {
	plan, err := descriptor.ConfigPlan(codexACPConfigOptions(session.ConfigOptions))
	if err != nil {
		return CodexACPConfigPlan{}, err
	}
	current := session
	for _, step := range plan.Steps {
		current, err = client.SetConfigOption(ctx, step.OptionID, step.Value)
		if err != nil {
			return CodexACPConfigPlan{}, fmt.Errorf("set Codex ACP %s configuration: %w", step.Semantic, err)
		}
	}
	if err := plan.VerifyApplied(codexACPConfigOptions(current.ConfigOptions)); err != nil {
		return CodexACPConfigPlan{}, err
	}
	return plan, nil
}

func codexACPConfigReceipt(plan CodexACPConfigPlan) string {
	parts := make([]string, 0, len(plan.Steps)+1)
	parts = append(parts, codexACPProviderPlanSchema)
	for _, step := range plan.Steps {
		parts = append(parts, step.Semantic+"\x00"+step.OptionID+"\x00"+step.Value)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
