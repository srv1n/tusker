package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestACPAdapterBundleValidationFailures(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest)
		leaveModes bool
		want       string
	}{
		{name: "tampered asset", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, _ *ACPAdapterBundleManifest) {
			writeACPAdapterBundleFile(t, filepath.Join(request.BundleRoot, "lib", "runtime.js"), []byte("tampered"), 0o600)
		}, want: "fingerprint drift"},
		{name: "asset traversal", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			manifest.Assets[0].Path = "../escape"
			writeACPAdapterBundleManifest(t, request, *manifest)
		}, want: "escapes bundle"},
		{name: "volume path", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			manifest.Assets[0].Path = "C:/adapter.js"
			writeACPAdapterBundleManifest(t, request, *manifest)
		}, want: "volume-free"},
		{name: "asset symlink", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			path := filepath.Join(request.BundleRoot, filepath.FromSlash(manifest.Assets[0].Path))
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside.js")
			writeACPAdapterBundleFile(t, outside, []byte("outside"), 0o600)
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
		}, want: "must not be a symlink"},
		{name: "hardlink", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			path := filepath.Join(request.BundleRoot, filepath.FromSlash(manifest.Assets[0].Path))
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside.js")
			writeACPAdapterBundleFile(t, outside, []byte("outside"), 0o600)
			if err := os.Link(outside, path); err != nil {
				t.Fatal(err)
			}
			manifest.Assets[0].SHA256 = testACPAdapterBundleFileDigest(t, path)
			writeACPAdapterBundleManifest(t, request, *manifest)
		}, want: "hard links"},
		{name: "owner writable asset", leaveModes: true, mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			sealACPAdapterBundle(t, request.BundleRoot)
			if err := os.Chmod(filepath.Join(request.BundleRoot, filepath.FromSlash(manifest.Assets[0].Path)), 0o600); err != nil {
				t.Fatal(err)
			}
		}, want: "must not be owner"},
		{name: "missing asset", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			if err := os.Remove(filepath.Join(request.BundleRoot, filepath.FromSlash(manifest.Assets[0].Path))); err != nil {
				t.Fatal(err)
			}
		}, want: "missing"},
		{name: "extra file", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, _ *ACPAdapterBundleManifest) {
			writeACPAdapterBundleFile(t, filepath.Join(request.BundleRoot, "extra.bin"), []byte("extra"), 0o600)
		}, want: "undeclared extra"},
		{name: "omitted asset", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			manifest.Assets = manifest.Assets[:2]
			writeACPAdapterBundleManifest(t, request, *manifest)
		}, want: "undeclared extra"},
		{name: "duplicate declaration", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			manifest.Assets = append(manifest.Assets, manifest.Assets[len(manifest.Assets)-1])
			writeACPAdapterBundleManifest(t, request, *manifest)
		}, want: "duplicate"},
		{name: "manifest overlap", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			manifest.Assets = append(manifest.Assets, ACPAdapterBundleAsset{Path: request.ManifestPath, SHA256: request.ExpectedManifestSHA256, Role: "asset"})
			manifest.Assets = SortACPAdapterBundleAssets(manifest.Assets)
			writeACPAdapterBundleManifest(t, request, *manifest)
		}, want: "overlaps"},
		{name: "bare PATH argv", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			manifest.Argv[0] = "node"
			writeACPAdapterBundleManifest(t, request, *manifest)
		}, want: "absolute path"},
		{name: "package launcher", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			manifest.Argv[0] = filepath.Join(request.BundleRoot, "bin", "npx")
			writeACPAdapterBundleManifest(t, request, *manifest)
		}, want: "package launcher"},
		{name: "free flag", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			manifest.Argv = append(manifest.Argv, "-c")
			writeACPAdapterBundleManifest(t, request, *manifest)
		}, want: "plus entrypoint"},
		{name: "wrong platform", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest *ACPAdapterBundleManifest) {
			manifest.GOARCH = "not-" + runtime.GOARCH
			writeACPAdapterBundleManifest(t, request, *manifest)
		}, want: "does not match runtime"},
		{name: "root writable", leaveModes: true, mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, _ *ACPAdapterBundleManifest) {
			sealACPAdapterBundle(t, request.BundleRoot)
			if err := os.Chmod(request.BundleRoot, 0o700); err != nil {
				t.Fatal(err)
			}
		}, want: "must not be owner"},
		{name: "nested directory writable", leaveModes: true, mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, _ *ACPAdapterBundleManifest) {
			sealACPAdapterBundle(t, request.BundleRoot)
			if err := os.Chmod(filepath.Join(request.BundleRoot, "lib"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, want: "must not be owner"},
		{name: "empty directory", mutate: func(t *testing.T, request *ACPAdapterBundleValidationRequest, _ *ACPAdapterBundleManifest) {
			if err := os.Mkdir(filepath.Join(request.BundleRoot, "empty"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, want: "empty directory"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, manifest := newACPAdapterBundleFixture(t)
			unsealACPAdapterBundle(t, request.BundleRoot)
			test.mutate(t, &request, &manifest)
			if !test.leaveModes {
				sealACPAdapterBundle(t, request.BundleRoot)
			}
			_, err := ValidateACPAdapterBundle(request)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("err=%v, want substring %q", err, test.want)
			}
		})
	}
}

func TestACPAdapterBundleManifestBindingAndSecretRejection(t *testing.T) {
	t.Run("semantic drift keeps stale trusted digest", func(t *testing.T) {
		request, manifest := newACPAdapterBundleFixture(t)
		unsealACPAdapterBundle(t, request.BundleRoot)
		trusted := request.ExpectedManifestSHA256
		manifest.Version = "0.2.0"
		writeACPAdapterBundleManifest(t, &request, manifest)
		request.ExpectedManifestSHA256 = trusted
		sealACPAdapterBundle(t, request.BundleRoot)
		if _, err := ValidateACPAdapterBundle(request); err == nil || !strings.Contains(err.Error(), "fingerprint drift") {
			t.Fatalf("semantic drift was accepted: %v", err)
		}
	})

	t.Run("noncanonical exact bytes", func(t *testing.T) {
		request, manifest := newACPAdapterBundleFixture(t)
		unsealACPAdapterBundle(t, request.BundleRoot)
		raw, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		writeACPAdapterBundleFile(t, filepath.Join(request.BundleRoot, request.ManifestPath), raw, 0o600)
		request.ExpectedManifestSHA256 = testACPAdapterBundleDigest(raw)
		sealACPAdapterBundle(t, request.BundleRoot)
		if _, err := ValidateACPAdapterBundle(request); err == nil || !strings.Contains(err.Error(), "canonical JSON") {
			t.Fatalf("noncanonical manifest was accepted: %v", err)
		}
	})

	t.Run("unknown secret canary", func(t *testing.T) {
		request, manifest := newACPAdapterBundleFixture(t)
		unsealACPAdapterBundle(t, request.BundleRoot)
		canonical, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		const canary = "super-secret-canary-value"
		raw := append(append([]byte(nil), canonical[:len(canonical)-1]...), []byte(`,"OPENAI_API_KEY":"`+canary+`"}`)...)
		writeACPAdapterBundleFile(t, filepath.Join(request.BundleRoot, request.ManifestPath), raw, 0o600)
		request.ExpectedManifestSHA256 = testACPAdapterBundleDigest(raw)
		sealACPAdapterBundle(t, request.BundleRoot)
		_, err = ValidateACPAdapterBundle(request)
		if err == nil || strings.Contains(err.Error(), canary) {
			t.Fatalf("secret-bearing manifest err=%v", err)
		}
	})

	t.Run("trusted digest required separately", func(t *testing.T) {
		request, _ := newACPAdapterBundleFixture(t)
		request.ExpectedManifestSHA256 = ""
		if _, err := ValidateACPAdapterBundle(request); err == nil || !strings.Contains(err.Error(), "separately trusted") {
			t.Fatalf("missing trusted digest was accepted: %v", err)
		}
	})
}

func TestACPAdapterBundleArgvVariantsAndStableReceipt(t *testing.T) {
	interpreterRequest, _ := newACPAdapterBundleFixture(t)
	first, err := ValidateACPAdapterBundle(interpreterRequest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ValidateACPAdapterBundle(interpreterRequest)
	if err != nil {
		t.Fatal(err)
	}
	if first.VerifiedContentDigest == "" || first.VerifiedContentDigest != second.VerifiedContentDigest || first.GOOS != runtime.GOOS || first.GOARCH != runtime.GOARCH {
		t.Fatalf("receipt is not stable and platform-bound: first=%#v second=%#v", first, second)
	}

	nativeRequest, nativeManifest := newACPAdapterBundleFixture(t)
	unsealACPAdapterBundle(t, nativeRequest.BundleRoot)
	if err := os.Remove(filepath.Join(nativeRequest.BundleRoot, "adapter.js")); err != nil {
		t.Fatal(err)
	}
	nativeManifest.Argv = nativeManifest.Argv[:1]
	nativeManifest.Assets = []ACPAdapterBundleAsset{nativeManifest.Assets[1], nativeManifest.Assets[2]}
	nativeManifest.Provider, nativeManifest.Adapter = "codex", "codex-acp"
	nativeRequest.ExpectedDescriptor = ACPAdapterBundleDescriptorPolicy{Provider: "codex", Adapter: "codex-acp", Version: nativeManifest.Version, LaunchKind: ACPAdapterBundleLaunchNative}
	writeACPAdapterBundleManifest(t, &nativeRequest, nativeManifest)
	sealACPAdapterBundle(t, nativeRequest.BundleRoot)
	if _, err := ValidateACPAdapterBundle(nativeRequest); err != nil {
		t.Fatalf("native one-part argv was rejected: %v", err)
	}
}

func TestACPAdapterBundlePhysicalAliasAndRootSymlink(t *testing.T) {
	t.Run("physical duplicate", func(t *testing.T) {
		request, manifest := newACPAdapterBundleFixture(t)
		unsealACPAdapterBundle(t, request.BundleRoot)
		alias := filepath.Join(request.BundleRoot, "lib", "alias.js")
		if err := os.Link(filepath.Join(request.BundleRoot, "lib", "runtime.js"), alias); err != nil {
			t.Fatal(err)
		}
		manifest.Assets = append(manifest.Assets, ACPAdapterBundleAsset{Path: "lib/alias.js", SHA256: testACPAdapterBundleFileDigest(t, alias), Role: "asset"})
		manifest.Assets = SortACPAdapterBundleAssets(manifest.Assets)
		writeACPAdapterBundleManifest(t, &request, manifest)
		sealACPAdapterBundle(t, request.BundleRoot)
		if _, err := ValidateACPAdapterBundle(request); err == nil || !strings.Contains(err.Error(), "physical duplicate") {
			t.Fatalf("physical alias was accepted: %v", err)
		}
	})

	t.Run("root symlink", func(t *testing.T) {
		request, _ := newACPAdapterBundleFixture(t)
		link := filepath.Join(t.TempDir(), "bundle-link")
		if err := os.Symlink(request.BundleRoot, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		request.BundleRoot = link
		if _, err := ValidateACPAdapterBundle(request); err == nil || !strings.Contains(err.Error(), "non-symlink directory") {
			t.Fatalf("symlink root was accepted: %v", err)
		}
	})
}

func TestACPAdapterBundleDescriptorCrossShapeAndPreSpawnRevalidation(t *testing.T) {
	t.Run("codex rejects interpreter shape", func(t *testing.T) {
		request, manifest := newACPAdapterBundleFixture(t)
		unsealACPAdapterBundle(t, request.BundleRoot)
		manifest.Provider, manifest.Adapter = "codex", "codex-acp"
		request.ExpectedDescriptor = ACPAdapterBundleDescriptorPolicy{Provider: "codex", Adapter: "codex-acp", Version: manifest.Version, LaunchKind: ACPAdapterBundleLaunchInterpreter}
		writeACPAdapterBundleManifest(t, &request, manifest)
		sealACPAdapterBundle(t, request.BundleRoot)
		if _, err := ValidateACPAdapterBundle(request); err == nil || !strings.Contains(err.Error(), "native launch kind") {
			t.Fatalf("codex interpreter shape was accepted: %v", err)
		}
	})

	t.Run("claude rejects native shape", func(t *testing.T) {
		request, manifest := newACPAdapterBundleFixture(t)
		unsealACPAdapterBundle(t, request.BundleRoot)
		if err := os.Remove(filepath.Join(request.BundleRoot, "adapter.js")); err != nil {
			t.Fatal(err)
		}
		manifest.Argv = manifest.Argv[:1]
		manifest.Assets = []ACPAdapterBundleAsset{manifest.Assets[1], manifest.Assets[2]}
		request.ExpectedDescriptor.LaunchKind = ACPAdapterBundleLaunchNative
		writeACPAdapterBundleManifest(t, &request, manifest)
		sealACPAdapterBundle(t, request.BundleRoot)
		if _, err := ValidateACPAdapterBundle(request); err == nil || !strings.Contains(err.Error(), "interpreter entrypoint") {
			t.Fatalf("claude native shape was accepted: %v", err)
		}
	})

	t.Run("mutation fails immediate revalidation", func(t *testing.T) {
		request, _ := newACPAdapterBundleFixture(t)
		receipt, err := ValidateACPAdapterBundle(request)
		if err != nil {
			t.Fatal(err)
		}
		unsealACPAdapterBundle(t, request.BundleRoot)
		writeACPAdapterBundleFile(t, filepath.Join(request.BundleRoot, "lib", "runtime.js"), []byte("changed-before-spawn"), 0o600)
		sealACPAdapterBundle(t, request.BundleRoot)
		if err := RevalidateACPAdapterBundleReceipt(request, receipt); err == nil || !strings.Contains(err.Error(), "fingerprint drift") {
			t.Fatalf("pre-spawn mutation survived revalidation: %v", err)
		}
	})

	t.Run("content-addressed root digest", func(t *testing.T) {
		request, _ := newACPAdapterBundleFixture(t)
		digest, err := ACPAdapterBundleFinalRootDigest(request.ExpectedFinalRoot, request.ExpectedManifestSHA256)
		if err != nil {
			t.Fatal(err)
		}
		request.ExpectedFinalRoot = ""
		request.ExpectedFinalRootDigest = digest
		if _, err := ValidateACPAdapterBundle(request); err != nil {
			t.Fatalf("trusted final root digest was rejected: %v", err)
		}
	})
}

func TestACPAdapterBundleRejectsExcessiveTreeDepth(t *testing.T) {
	request, _ := newACPAdapterBundleFixture(t)
	unsealACPAdapterBundle(t, request.BundleRoot)
	path := request.BundleRoot
	for index := 0; index <= acpAdapterBundleMaxTreeDepth; index++ {
		path = filepath.Join(path, "d")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sealACPAdapterBundle(t, request.BundleRoot)
	if _, err := ValidateACPAdapterBundle(request); err == nil || !strings.Contains(err.Error(), "tree depth") {
		t.Fatalf("excessive tree depth was accepted: %v", err)
	}
}

func newACPAdapterBundleFixture(t *testing.T) (ACPAdapterBundleValidationRequest, ACPAdapterBundleManifest) {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() { _ = makeACPAdapterBundleWritable(root) })
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "lib"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeACPAdapterBundleFile(t, filepath.Join(root, "bin", "node"), []byte("native-node-binary"), 0o700)
	writeACPAdapterBundleFile(t, filepath.Join(root, "adapter.js"), []byte("adapter-entrypoint"), 0o600)
	writeACPAdapterBundleFile(t, filepath.Join(root, "lib", "runtime.js"), []byte("runtime-asset"), 0o600)
	assets := []ACPAdapterBundleAsset{
		{Path: "adapter.js", Role: "entrypoint"},
		{Path: "bin/node", Role: "executable"},
		{Path: "lib/runtime.js", Role: "asset"},
	}
	for index := range assets {
		assets[index].SHA256 = testACPAdapterBundleFileDigest(t, filepath.Join(root, filepath.FromSlash(assets[index].Path)))
	}
	manifest := ACPAdapterBundleManifest{
		Schema: ACPAdapterBundleSchema, Provider: "claude", Adapter: "claude-agent-acp", Version: "0.1.0", Protocol: ACPAdapterBundleProtocolV1,
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Argv: []string{filepath.Join(physicalRoot, "bin", "node"), filepath.Join(physicalRoot, "adapter.js")}, Assets: assets,
	}
	request := ACPAdapterBundleValidationRequest{
		BundleRoot: root, ManifestPath: "manifest.json",
		ExpectedDescriptor: ACPAdapterBundleDescriptorPolicy{Provider: "claude", Adapter: "claude-agent-acp", Version: "0.1.0", LaunchKind: ACPAdapterBundleLaunchInterpreter},
		ExpectedFinalRoot:  physicalRoot, TrustCurrentUserBoundary: true,
		ProviderAllowed: func(provider string) bool { return provider == "codex" || provider == "claude" },
	}
	writeACPAdapterBundleManifest(t, &request, manifest)
	sealACPAdapterBundle(t, root)
	return request, manifest
}

func writeACPAdapterBundleManifest(t *testing.T, request *ACPAdapterBundleValidationRequest, manifest ACPAdapterBundleManifest) {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeACPAdapterBundleFile(t, filepath.Join(request.BundleRoot, filepath.FromSlash(request.ManifestPath)), raw, 0o600)
	request.ExpectedManifestSHA256 = testACPAdapterBundleDigest(raw)
}

func testACPAdapterBundleFileDigest(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return testACPAdapterBundleDigest(raw)
}

func testACPAdapterBundleDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return acpAdapterBundleDigestPrefix + hex.EncodeToString(digest[:])
}

func writeACPAdapterBundleFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func sealACPAdapterBundle(t *testing.T, root string) {
	t.Helper()
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o400)
		if info.IsDir() {
			mode = 0o500
		} else if info.Mode()&0o111 != 0 {
			mode = 0o500
		}
		return os.Chmod(path, mode)
	}); err != nil {
		t.Fatal(err)
	}
}

func unsealACPAdapterBundle(t *testing.T, root string) {
	t.Helper()
	if err := makeACPAdapterBundleWritable(root); err != nil {
		t.Fatal(err)
	}
}

func makeACPAdapterBundleWritable(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if info.IsDir() {
			mode = 0o700
		} else if info.Mode()&0o111 != 0 {
			mode = 0o700
		}
		return os.Chmod(path, mode)
	})
}
