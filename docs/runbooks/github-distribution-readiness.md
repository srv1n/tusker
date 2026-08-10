---
title: "GitHub distribution readiness"
subject: github-distribution-readiness
keywords: [distribution, github, release, linux, macos, windows, checksums, rollback]
status: canonical
read_when: "You are deciding whether Tusker should move from source/internal installs to public GitHub artifacts."
skip_when: "You are doing normal local task, documentation, or agent-orchestration work."
---

# GitHub distribution readiness

This is a deliberately deferred operator plan for a possible future public
distribution of Tusker. It is not a product launch gate and it does not grant
release, publish, signing, or infrastructure authority.

Tusker's product is the local, higher-level orchestrator for documentation,
task tracking, proof, and Codex/Claude/cloud-agent coordination. Public binary
distribution is an outer delivery concern. Keep it outside the task model,
daemon authority, runner contracts, and project-vault data model.

## Current posture

The supported day-to-day path is a source or local build:

```sh
make install       # CLI plus Codex/Claude user skills
make install-cli   # CLI only
make install-repo REPO=/absolute/path/to/repo
```

The existing release installer and tagged-release workflow are **not** the
current product path. The installer intentionally fails closed until a future
release owner chooses and provisions a distribution trust policy. A `v*` tag or
a green local build is not evidence that a public release is authorized.

The current support boundary is recorded in
[`docs/system/platform-support.md`](../system/platform-support.md):

| Host | Supported product surface today | Distribution decision |
| --- | --- | --- |
| macOS 14+ arm64/amd64 | CLI, resident daemon, Serve UI, launchd integration, TuskerBar | Candidate future public targets |
| Linux arm64/amd64 | CLI, resident daemon, Serve UI; use the host service manager | Candidate future public targets |
| Windows | Portable `internal/...` Go packages are compile/test signals only | No CLI, daemon, Serve server, installer, or release artifact claim |

The release builder contains an experimental Windows/amd64 branch, but it is
not in the default matrix and must not be described as Windows support without
a separate product decision and native evidence.

## Staged re-entry

Revisit this runbook only when there is a concrete reason to distribute
artifacts (for example, external users, a public repository, or a maintainer
request for one-command installation). Record the evidence for each stage; do
not jump directly from a local build to a public release.

| Stage | Entry condition | Required work | Exit receipt |
| --- | --- | --- | --- |
| 0. Internal/source | Current state; no external distribution promise | Keep `make check`, focused platform checks, `validate`, and strict skill checks green; use source/local installs | Commit SHA, host/arch, tool versions, commands and PASS/FAIL output |
| 1. GitHub readiness | Owner explicitly chooses target hosts and a distribution owner | Protect `main` and `v*`; review workflow permissions and action SHAs; decide archive/app scope; implement the matrix below; write install/support docs | Reviewed readiness checklist, protected-rule screenshots/export, workflow lint result, and target decision |
| 2. Candidate release | Stage 1 is approved and a clean candidate commit exists | Build, test, package, checksum, install-smoke, and rollback-test every selected target; retain artifacts without publishing | Candidate tag/commit, workflow run IDs, artifact names/digests, test summaries, and rollback receipt |
| 3. Public release | Stage 2 receipt reviewed by the release owner | Publish one immutable GitHub release, update install instructions, canary with a bounded audience, and monitor rollback/upgrade | Release URL, immutable asset listing, checksum receipt, canary result, and support/rollback owner |
| 4. Optional trust hardening | Public distribution creates a real verification threat model or user requirement | Select **one** artifact-signing/attestation mechanism and document its trust root and recovery | A separate decision record and verifier receipt; never make both GPG and Minisign mandatory by default |

Stage 4 is intentionally optional. Checksums, protected GitHub permissions,
immutable assets, and rollback are the baseline. GPG, Minisign, Sigstore, or
another mechanism may be evaluated later, but choosing one is a policy decision
for public distribution—not a requirement for Tusker's core orchestrator.

## Future build/test/package matrix

The matrix below is the minimum public-release proposal. A target only becomes
supported after its row has a native (or explicitly justified equivalent)
build, test, package, and install/upgrade receipt.

| Target | Build | Test evidence | Package and smoke |
| --- | --- | --- | --- |
| Linux amd64 | Native Linux build of the CLI and embedded Serve UI | Full Go suite, vet/validation, Linux E2E, race lane where applicable | `tusker_<version>_linux_amd64.tar.gz`; checksum verification, confined extraction, version/health check, interrupted-upgrade rollback |
| Linux arm64 | Native arm64 runner preferred; cross-build alone is insufficient | Portable/full Go tests on arm64 plus the shared Linux correctness gate | `tusker_<version>_linux_arm64.tar.gz`; same checksum, extraction, install, and rollback smoke |
| macOS amd64 | Native Intel macOS build; CLI and, if chosen, TuskerBar | Full Go tests and macOS-specific process/filesystem checks; TuskerBar build/install smoke if the app is distributed | `tusker_<version>_darwin_amd64.tar.gz`; optional signed/notarized app package only after a separate app-distribution decision; install and rollback smoke |
| macOS arm64 | Native Apple Silicon build; CLI and, if chosen, TuskerBar | Full Go tests and macOS-specific process/filesystem checks; TuskerBar build/install smoke if the app is distributed | `tusker_<version>_darwin_arm64.tar.gz`; optional app package under the same separate decision; install and rollback smoke |
| Windows | No product build/package commitment | Continue `go test ./internal/...` as a portability signal only | Do not publish a Windows CLI, daemon, installer, or artifact until a separate support project covers services, process control, filesystem semantics, installer UX, and native E2E |

The current default archive matrix is the four Linux/macOS CLI rows. TuskerBar
is a macOS local application today; shipping it through GitHub is a separate
decision from shipping the CLI archive. The older
[`release-integrity.md`](release-integrity.md) runbook retains detailed release
mechanics as an optional design note; it does not override this product
boundary.

## GitHub controls and release invariants

When Stage 1 is reopened, require all of the following before Stage 2:

- `main` requires reviewed pull requests and the relevant CI checks; workflow
  and release-script changes receive the same review as code.
- `v*` tags are protected. Only the release owner or a narrowly scoped release
  environment can create them; force-retagging is disabled.
- Build jobs have read-only contents permissions. Publication is a separate,
  explicitly approved job with the smallest write permission it needs.
- Actions are pinned to reviewed commit SHAs. The workflow records the source
  commit, toolchain, runner OS/architecture, and artifact digests.
- A release name is immutable: an existing tag/release/asset is never replaced
  in place. A correction gets a new version.
- Every archive has a SHA-256 checksum, and the installer verifies the exact
  archive selected for its host/architecture before extraction.
- Extraction is confined to a temporary directory and rejects traversal,
  absolute paths, duplicate entries, and unexpected link/device types.
- Upgrades stage on the destination filesystem, health-check the exact version,
  atomically replace the live binary/app, and retain one known-good rollback.
  Interruption, failed health, or a failed post-swap check must restore it.

These controls protect ordinary users from accidental corruption and mutable
release mistakes without coupling them to the core orchestrator's authority
model.

## Evidence receipt

Keep one small receipt per candidate or public release. It should name:

```text
source_commit=<full commit SHA>
tag=<version, if any>
workflow_run=<URL or ID>
targets=<exact OS/architecture list>
toolchains=<Go/Bun/Swift versions as applicable>
artifacts=<filename sha256 ...>
checks=<test, vet, validation, package, install, rollback summaries>
owner=<release owner>
approval=<stage and date>
```

Do not use a receipt as task proof, lease authority, provider evidence, or
permission to publish. It is an operator record for a distribution decision.

## Optional SBOM, provenance, licensing, and signing

For internal/source installs, keep the existing license and dependency checks
proportionate to development. If public distribution is approved, decide the
following in a separate short policy record:

1. Which license and third-party notices ship with each artifact.
2. Whether an SBOM and build provenance are needed for the intended consumers,
   jurisdictions, or incident-response obligations.
3. Whether artifact signatures or attestations add value beyond protected
   GitHub releases and checksums.

If signing is chosen, select one mechanism, one trust-root publication path,
one key/identity rotation and recovery plan, and one verification UX. Do not
reintroduce a combined GPG-plus-Minisign ceremony merely because an old
release draft contains both. The decision belongs in this outer distribution
layer and must not block local source installation or core Tusker operation.

## Keeping the core clean

Revisit this plan without polluting the product architecture:

- keep packaging, workflow, installer, checksum, and optional trust code in
  `.github/workflows/`, `scripts/`, and release/runbook documentation;
- keep target-specific app packaging under `apps/mac/TuskerBar/`;
- do not add release keys, GitHub credentials, publication state, or artifact
  policy fields to task frontmatter, runtime state, runner profiles, or the
  project-vault schema;
- do not let release status alter task leases, proof, review, dispatch, or
  provider authorization;
- update `docs/system/platform-support.md` and this runbook together whenever
  a support target changes; retain the Windows boundary until its separate
  acceptance project is complete.

The safe default is to remain at Stage 0. Public distribution is a product
choice with an owner, not an accidental consequence of a tag push.
