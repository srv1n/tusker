package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// This is intentionally a provider boundary, not a Darwin process-tree
// implementation. Darwin exposes useful identity facts (audit PID versions,
// start times, SessionCreate), but no public unprivileged API that can
// enumerate and terminate every process in a launch/audit scope after a
// double-fork/reparent. A provider must own a container or VM and make its
// lifecycle scope disappear before it reports a command complete.
const (
	v7FullGateIsolationContract = "tusker.full-gate-isolation/lifecycle-provider/v2"
	v7FullGateProviderSchema    = "tusker.full-gate-provider/v1"
	v7FullGateCleanTimeout      = 2 * time.Second
)

var errV7FullGateProvider = errors.New("full-gate lifecycle provider")

type v7FullGateProviderRequest struct {
	Schema            string `json:"schema"`
	Contract          string `json:"contract"`
	RunID             string `json:"run_id"`
	Workspace         string `json:"workspace"`
	Command           string `json:"command"`
	ResultPath        string `json:"result_path"`
	ProviderKind      string `json:"provider_kind"`
	ProviderID        string `json:"provider_id"`
	ProviderPath      string `json:"provider_path"`
	ExecutableID      string `json:"executable_id"`
	RequestDigest     string `json:"request_digest"`
	CandidateReadOnly bool   `json:"candidate_read_only"`
	NetworkDenied     bool   `json:"network_denied"`
	ControlEnvDenied  bool   `json:"control_env_denied"`
}

// v7FullGateProviderResult is a receipt from the provider, not a hint. The
// provider may return only after every process in its container/VM scope has
// stopped; lifecycle_id ties both normal completion and recovery to that
// immutable provider-side scope.
type v7FullGateProviderResult struct {
	Schema        string `json:"schema"`
	Contract      string `json:"contract"`
	RunID         string `json:"run_id"`
	LifecycleID   string `json:"lifecycle_id"`
	State         string `json:"state"`
	Output        string `json:"output,omitempty"`
	Error         string `json:"error,omitempty"`
	ProviderID    string `json:"provider_id"`
	RequestDigest string `json:"request_digest"`
}

type v7FullGateProvider interface {
	Run(context.Context, string, string) ([]byte, error)
	Close() error
}

type v7TrustedFullGateProvider struct {
	Kind    string `yaml:"kind"`
	Command string `yaml:"command"`
	Version string `yaml:"version"`
}

type v7FullGateProviderRegistry struct {
	Schema    string                               `yaml:"schema"`
	Providers map[string]v7TrustedFullGateProvider `yaml:"providers"`
}

var v7FullGateProviderRegistryPath = func() string {
	return filepath.Join(DefaultStateRoot(), "full-gate-providers.yaml")
}

var newV7FullGateProvider = func(profile, repoRoot, stateRoot string) (v7FullGateProvider, error) {
	trusted, path, identity, executableIdentity, err := resolveV7TrustedFullGateProvider(profile)
	if err != nil {
		return nil, err
	}
	if v7PathWithin(repoRoot, path) {
		return nil, fmt.Errorf("%w: trusted provider executable must not be repository-local", errV7FullGateProvider)
	}
	return &v7ExternalFullGateProvider{path: path, kind: trusted.Kind, identity: identity, executableIdentity: executableIdentity, recoveryRoot: filepath.Join(stateRoot, "full-gate-recovery")}, nil
}

type v7ExternalFullGateProvider struct {
	path               string
	kind               string
	identity           string
	executableIdentity string
	recoveryRoot       string
	mu                 sync.Mutex
	active             *v7FullGateProviderRequest
	closed             bool
}

func resolveV7TrustedFullGateProvider(profile string) (v7TrustedFullGateProvider, string, string, string, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: scheduled full promotion requires a configured lifecycle-safe container/VM isolation_provider profile", errV7FullGateProvider)
	}
	registryPath := v7FullGateProviderRegistryPath()
	info, err := os.Lstat(registryPath)
	if err != nil {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: trusted provider registry %s is unavailable: %v", errV7FullGateProvider, registryPath, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o022 != 0 {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: trusted provider registry must be a non-group/world-writable regular file", errV7FullGateProvider)
	}
	raw, err := os.ReadFile(registryPath)
	if err != nil {
		return v7TrustedFullGateProvider{}, "", "", "", err
	}
	var registry v7FullGateProviderRegistry
	if err := yaml.Unmarshal(raw, &registry); err != nil {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: parse trusted provider registry: %v", errV7FullGateProvider, err)
	}
	trusted, ok := registry.Providers[profile]
	if registry.Schema != v7FullGateProviderSchema || !ok || trusted.Kind != "container" && trusted.Kind != "vm" || strings.TrimSpace(trusted.Version) == "" {
		return v7TrustedFullGateProvider{}, "", "", "", fmt.Errorf("%w: trusted provider profile %q is not a valid container/VM lifecycle provider", errV7FullGateProvider, profile)
	}
	path, identity, err := verifyV7TrustedProviderExecutable(trusted.Command)
	if err != nil {
		return v7TrustedFullGateProvider{}, "", "", "", err
	}
	return trusted, path, departureFingerprint(profile, trusted.Kind, trusted.Version, identity), identity, nil
}

func verifyV7TrustedProviderExecutable(path string) (string, string, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\n\r\t ") || filepath.Base(path) == "sandbox-exec" {
		return "", "", fmt.Errorf("%w: provider executable must be an absolute non-sandbox executable path", errV7FullGateProvider)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", "", fmt.Errorf("%w: trusted provider executable unavailable: %v", errV7FullGateProvider, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o022 != 0 || info.Mode()&0o111 == 0 {
		return "", "", fmt.Errorf("%w: trusted provider executable must be a non-group/world-writable executable regular file", errV7FullGateProvider)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("%w: read trusted provider executable: %v", errV7FullGateProvider, err)
	}
	sum := sha256.Sum256(raw)
	return path, fmt.Sprintf("sha256:%x", sum[:]), nil
}

func (p *v7ExternalFullGateProvider) verifyIdentity() error {
	_, identity, err := verifyV7TrustedProviderExecutable(p.path)
	if err != nil {
		return err
	}
	if p.executableIdentity != identity {
		return fmt.Errorf("%w: trusted provider executable identity changed after profile resolution", errV7FullGateProvider)
	}
	return nil
}

func (p *v7ExternalFullGateProvider) Run(ctx context.Context, workspace, command string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	request, requestPath, err := p.newRequest(workspace, command)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(filepath.Dir(requestPath))
	p.mu.Lock()
	if p.closed || p.active != nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("%w: provider is unavailable for a new scope", errV7FullGateProvider)
	}
	p.active = &request
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.active = nil
		p.mu.Unlock()
	}()

	if err := p.verifyIdentity(); err != nil {
		return nil, err
	}
	cmd := exec.Command(p.path, "--tusker-full-gate-run", requestPath)
	var providerOutput bytes.Buffer
	cmd.Stdout, cmd.Stderr = &providerOutput, &providerOutput
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: start provider: %v", errV7FullGateProvider, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case runErr := <-done:
		result, receiptErr := readV7FullGateProviderResult(request)
		if receiptErr != nil {
			_ = p.cleanup(request, requestPath)
			return providerOutput.Bytes(), receiptErr
		}
		cleanupErr := p.cleanup(request, requestPath)
		if runErr != nil || cleanupErr != nil {
			if runErr != nil {
				return append(providerOutput.Bytes(), []byte(result.Output)...), fmt.Errorf("%w: provider run: %v", errV7FullGateProvider, runErr)
			}
			return append(providerOutput.Bytes(), []byte(result.Output)...), cleanupErr
		}
		return []byte(result.Output), nil
	case <-ctx.Done():
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(v7FullGateCleanTimeout):
			_ = cmd.Process.Kill()
			<-done
		}
		if err := p.cleanup(request, requestPath); err != nil {
			return providerOutput.Bytes(), err
		}
		return providerOutput.Bytes(), ctx.Err()
	}
}

func (p *v7ExternalFullGateProvider) Close() error {
	p.mu.Lock()
	p.closed = true
	active := p.active
	p.mu.Unlock()
	if active == nil {
		return nil
	}
	return p.cleanup(*active, "")
}

func (p *v7ExternalFullGateProvider) newRequest(workspace, command string) (v7FullGateProviderRequest, string, error) {
	recoveryRoot := p.recoveryRoot
	if strings.TrimSpace(recoveryRoot) == "" {
		recoveryRoot = filepath.Join(DefaultStateRoot(), "full-gate-recovery")
	}
	if err := os.MkdirAll(recoveryRoot, 0o700); err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	control, err := os.MkdirTemp(recoveryRoot, "scope-")
	if err != nil {
		return v7FullGateProviderRequest{}, "", err
	}
	workspace, err = sandboxCanonicalPath(workspace)
	if err != nil {
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", err
	}
	request := v7FullGateProviderRequest{
		Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract,
		RunID: strings.ToLower(newRecordID()), Workspace: workspace, Command: command,
		ResultPath: filepath.Join(control, "result.json"), ProviderKind: p.kind, ProviderID: p.identity, ProviderPath: p.path, ExecutableID: p.executableIdentity,
		CandidateReadOnly: true, NetworkDenied: true, ControlEnvDenied: true,
	}
	request.RequestDigest = v7FullGateRequestDigest(request)
	requestPath := filepath.Join(control, "request.json")
	raw, err := json.Marshal(request)
	if err != nil {
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", err
	}
	if err := os.WriteFile(requestPath, append(raw, '\n'), 0o600); err != nil {
		_ = os.RemoveAll(control)
		return v7FullGateProviderRequest{}, "", err
	}
	return request, requestPath, nil
}

func (p *v7ExternalFullGateProvider) cleanup(request v7FullGateProviderRequest, requestPath string) error {
	if err := p.verifyIdentity(); err != nil {
		return err
	}
	if requestPath == "" {
		return fmt.Errorf("%w: provider recovery cannot locate its trusted request", errV7FullGateProvider)
	}
	cmd := exec.Command(p.path, "--tusker-full-gate-cleanup", requestPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: provider cleanup for run %s failed: %v: %s", errV7FullGateProvider, request.RunID, err, strings.TrimSpace(string(output)))
	}
	result, err := readV7FullGateProviderResult(request)
	if err != nil {
		return err
	}
	if result.State != "cleaned" {
		return fmt.Errorf("%w: provider cleanup for run %s was not certified", errV7FullGateProvider, request.RunID)
	}
	return nil
}

func readV7FullGateProviderResult(request v7FullGateProviderRequest) (v7FullGateProviderResult, error) {
	raw, err := os.ReadFile(request.ResultPath)
	if err != nil {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider did not produce a cleanup receipt: %v", errV7FullGateProvider, err)
	}
	var result v7FullGateProviderResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: invalid provider cleanup receipt: %v", errV7FullGateProvider, err)
	}
	if result.Schema != v7FullGateProviderSchema || result.Contract != v7FullGateIsolationContract || result.RunID != request.RunID || result.ProviderID != request.ProviderID || result.RequestDigest != request.RequestDigest || strings.TrimSpace(result.LifecycleID) == "" {
		return v7FullGateProviderResult{}, fmt.Errorf("%w: provider cleanup receipt does not bind the requested lifecycle scope", errV7FullGateProvider)
	}
	return result, nil
}

func v7FullGateRequestDigest(request v7FullGateProviderRequest) string {
	parts := []string{request.Schema, request.Contract, request.RunID, request.Workspace, request.Command, request.ResultPath, request.ProviderKind, request.ProviderID, request.ProviderPath, request.ExecutableID, fmt.Sprint(request.CandidateReadOnly), fmt.Sprint(request.NetworkDenied), fmt.Sprint(request.ControlEnvDenied)}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("sha256:%x", sum[:])
}

// recoverV7FullGateProviderScopes is run before a daemon accepts work. A
// crash can interrupt the parent between provider start and cleanup; durable
// request records let the next daemon invoke the provider's exact cleanup
// operation rather than inferring safety from a dead root PID.
func recoverV7FullGateProviderScopes(stateRoot string) error {
	root := filepath.Join(stateRoot, "full-gate-recovery")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: read provider recovery records: %v", errV7FullGateProvider, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return fmt.Errorf("%w: invalid provider recovery entry %q", errV7FullGateProvider, entry.Name())
		}
		dir := filepath.Join(root, entry.Name())
		requestPath := filepath.Join(dir, "request.json")
		raw, readErr := os.ReadFile(requestPath)
		if readErr != nil {
			return fmt.Errorf("%w: read provider recovery request: %v", errV7FullGateProvider, readErr)
		}
		var request v7FullGateProviderRequest
		if err := json.Unmarshal(raw, &request); err != nil || request.RequestDigest != v7FullGateRequestDigest(request) || request.Schema != v7FullGateProviderSchema || request.Contract != v7FullGateIsolationContract {
			return fmt.Errorf("%w: invalid provider recovery request %q", errV7FullGateProvider, entry.Name())
		}
		_, executableID, verifyErr := verifyV7TrustedProviderExecutable(request.ProviderPath)
		if verifyErr != nil || executableID != request.ExecutableID {
			return fmt.Errorf("%w: provider recovery executable identity is unavailable or changed for %q", errV7FullGateProvider, entry.Name())
		}
		provider := &v7ExternalFullGateProvider{path: request.ProviderPath, executableIdentity: request.ExecutableID}
		if err := provider.cleanup(request, requestPath); err != nil {
			return err
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("%w: remove recovered provider scope: %v", errV7FullGateProvider, err)
		}
	}
	return nil
}

func v7PathWithin(root, path string) bool {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
