# Tusker production-hardening implementation report — 2026-08-09

This report records the implementation disposition of all 34 findings in
`production-readiness-code-review-2026-08-09.md`. It is evidence for the current
working tree, not a release receipt. The checkout was already materially dirty
and contains concurrent tracked and untracked work, so no clean-commit, tag,
artifact, publish, migration-restore, or production-soak claim is made here.

> **Scope note:** The signed-artifact, clean-tag, hosted-matrix, canary, and
> soak items below are deferred gates for a possible future GitHub distribution;
> they are not blockers for normal source/internal Tusker use. Keep the core
> hardening and local proof separate from that optional delivery track. See the
> [GitHub distribution readiness runbook](../runbooks/github-distribution-readiness.md)
> for the staged re-entry plan and the single-mechanism signing policy.

## Release decision

**Source hardening: substantially complete. Distribution: blocked.**

All P0 implementation defects and the user-visible fake-success paths are
closed. Most P1 defects now have fail-closed code and focused regression proof.
The tree is still not an authorized production candidate for three concrete
reasons:

1. release signing identity is intentionally unprovisioned, so production
   installers and artifact construction fail closed;
2. the current checkout is not a clean, signed tag and therefore cannot exercise
   the immutable release transaction or two-builder reproducibility gate; and
3. schema migration restore/downgrade proof plus the exact clean-candidate CI,
   race, E2E, install-canary, and soak receipts still have to be retained.

Disposition meanings:

- **Closed** — implementation and focused deterministic proof are present.
- **Mitigated** — the dangerous default is fenced, but an acceptance item still
  requires clean-candidate or operational proof.
- **Deferred P2** — bounded debt that is not allowed to disappear from the exit
  gate.
- **Blocked** — no honest source-only action can provide the missing authority or
  exact-candidate proof.

| Disposition | Count | Release consequence |
| --- | ---: | --- |
| Closed | 29 | No known P0 remains in reviewed scope |
| Mitigated | 2 | Must finish before public production |
| Deferred P2 | 2 | Needs a named follow-up before broad rollout |
| Blocked | 1 | Release cannot proceed |

# 1. Correctness, data integrity, security, and operations

| Finding | Disposition | Implemented behavior and proof |
| --- | --- | --- |
| COR-01 cross-project run identity | **Closed** | Authority-changing run lookup is project-scoped; bare duplicate identities return `RUN_IDENTITY_AMBIGUOUS`. Daemon control carries `ProjectID`. Collision, ownership, lifecycle, retry, wrapper, and work-session paths were migrated and focused collision tests pass. |
| COR-02 private runtime state | **Closed** | Runtime roots are owner-controlled `0700`; DB/WAL/SHM, sockets, status, request, raw/event logs are regular, current-user, single-link, owner-only artifacts. Read-only opens validate without mutating. Descriptor-relative `openat`/`fstatat`/`linkat`/`renameat` operations refuse symlinked parents and path swaps. |
| COR-03 privileged loopback control plane | **Closed with stated threat limit** | Serve mutations require loopback/same-origin checks, JSON content type, and a per-process capability. The UI bootstraps and refreshes a stale capability once. This blocks drive-by browser requests; it is not OS-user authentication, and a same-UID local process remains inside the documented trust boundary. |
| COR-04 unbounded/secret hook output | **Closed** | Hook output is drained through a 64 KiB cap, command text is omitted, and token/secret/password/API-key/authorization patterns are redacted. A 10 MiB adversarial corpus test passes. |
| COR-05 scratch deletion/writer race | **Closed** | Scratch GC, retirement, dispatch, delivery, attachments, plans, and wrapper log writes coordinate through a private lock. Traversal/deletion/writes/moves are descriptor-relative and no-follow; live or unverifiable runners stop retirement. Symlink, live-runner, and writer/GC barrier tests pass. |
| COR-06 schema upgrade boundary | **Mitigated** | Runtime schema versioning now has a cheap current-schema path guarded by a complete required table/column/index inventory; a marker cannot hide a damaged schema. State preflight happens before SQLite open. Historical-version failure injection, backup/restore, integrity, downgrade policy, and contention latency still require an exact clean-candidate drill. |
| COR-07 malformed end state | **Closed** | Decode failure is exposed as `end_state_invalid` plus an error; redrive authority in CLI and Serve refuses corrupt provenance instead of treating it as empty. Inspection remains available to diagnose/quarantine the row. |
| COR-08 directive busy retry | **Closed** | `RunDirective` uses the store retry wrapper rather than a raw `QueryRow`. |
| COR-09 Python-dependent terminal status | **Closed** | The Go parent owns wait/cancellation/status publication; the ordinary runner no longer depends on `python3` in the worker `PATH`. Terminal status is atomic, owner-only, exact-once, and publishes a durable infrastructure breadcrumb on failure. |
| COR-10 HTTP hardening | **Closed** | Responses carry CSP/frame denial/nosniff/referrer policy; headers and request concurrency are bounded; stream and short-request admission are separate; panic recovery does not append a second response after commit. |
| COR-11 operational history/retention | **Closed** | Resource release writes an attributable event transactionally. Daemon/native logs are owner-only, redacted, size/age bounded, rotated, and symlink/hardlink checked; notification history and status evidence are bounded/durable. |

Security caveats retained by design:

- same-UID processes are in the local Serve/control-plane trust boundary;
- a post-`linkat` cleanup/fsync error can report infrastructure failure even when
  the exact-once status inode is already durable; this is fail-closed;
- Windows is not a supported CLI/daemon runtime because these authority paths use
  Unix filesystem/process primitives.

# 2. Performance, scalability, and efficiency

| Finding | Disposition | Implemented behavior and proof |
| --- | --- | --- |
| PERF-01 unbounded runtime reads | **Closed at external boundaries** | Serve snapshots use project-scoped SQL with a hard 1,000-row snapshot refusal; `/api/runs` defaults to 100 and caps at 500 with stable cursor headers. Attempts/turns/events are SQL-bounded and expose truncation. Primary HTTP endpoints propagate DB errors instead of returning false-empty success. Compatibility/internal exact-identity scans remain where bounded truncation would break correctness. |
| PERF-02 stream drop without replay | **Closed** | SSE events have strict monotonic IDs and a bounded replay ring. Web and native clients send `Last-Event-ID`, deduplicate, generation-fence reconnects, and force broad refresh on replay miss. Slow clients are disconnected without consuming the short-request admission pool. |
| PERF-03 settings write amplification | **Closed** | Project settings use a validated draft and explicit Save with refusal/error feedback; local-only app settings are labeled read-only/unavailable instead of firing fake or per-keystroke mutations. |
| PERF-04 package/test feedback bottleneck | **Deferred P2** | CI is split into UI, release, portable matrix, E2E, race, and vulnerability lanes, and test fixtures now ignore developer-machine Git hooks. The `cmd/tusker` package is still a large Unix-oriented monolith; package extraction needs profiling and a separate compatibility-preserving program. |

The external API caps are product contracts, not merely UI limits. When a
snapshot cannot be complete within its hard bound, Serve refuses it instead of
allowing an operator to act on a partial world view.

# 3. Product usability, accessibility, and truthfulness

| Finding | Disposition | Implemented behavior and proof |
| --- | --- | --- |
| UX-01 fake document Save | **Closed** | The unused mock editor was deleted. DocReader and TaskContract are explicitly read-only until a real CAS write contract is available; no Save success can be shown for a non-write. |
| UX-02 fake Approve | **Closed** | The local `approved` state and false “gate cleared” claim were removed. Only real lifecycle/gate actions remain. |
| UX-03 fake frontmatter write | **Closed** | Property controls are disabled on read-only document surfaces; the unconditional `{ok:true}` path is no longer exposed as persistence. |
| UX-04 ephemeral global settings | **Closed** | Theme remains a genuine local preference; unimplemented profiles, permissions, notification delivery, density, and defaults are explicitly unavailable/read-only. Project settings have one validated save transaction. |
| UX-05 refusals treated as success | **Closed** | All action mutations pass through `requireAccepted`; `{refused:true}` and `{ok:false}` become typed `ActionRefusalError` values. Capability 403 refresh is bounded to one retry; delivery and non-JSON failures normalize to `ApiError`. |
| UX-06 incomplete focus management | **Closed** | Sidebar drawer, search, and confirmation dialogs trap focus, support Escape, and restore the invoker. Keyboard/viewport tests cover the contract. |
| UX-07 notification over-deduplication | **Closed** | Native dedupe keys use project event identity rather than task/kind forever; an ordered 256-entry persisted history supports replay dedupe, rollback, and repeated state transitions. |
| UX-08 development trust shortcuts | **Closed** | Web Inspector is off outside debug or an explicit developer setting; Markdown rejects unsafe schemes, network paths, backslashes, and control characters; external links use `noopener noreferrer`; server security headers are explicit. |
| UX-09 capability/docs drift | **Closed** | `/api/capabilities` exposes a sorted machine-readable classification (`authoritative_mutable`, `authoritative_read_only`, `cached_projection`, `local_preference`, `unavailable`). UI gaps and Serve/platform documentation state persistence, freshness, and threat limits directly. |

Client proof on this tree:

- 128 Bun tests, 743 assertions, 0 failures across 26 files;
- UI typecheck and production build pass;
- two consecutive UI builds produce the same embedded-dist hash manifest;
- responsive Chromium checks pass at 390 px and 1,440 px outside the socket-
  restricted sandbox; and
- 18 SwiftPM tests pass, covering SSE replay/reconnect, notifications, log
  redaction/rotation, runtime shell behavior, and Spotlight routing.

# 4. Maintainability, testing, documentation, and release engineering

| Finding | Disposition | Implemented behavior and proof |
| --- | --- | --- |
| REL-01 unsafe release version/delete | **Closed** | Strict SemVer validation, canonical containment, symlink refusal, a locked staged builder, and no direct Make interpolation protect cleanup/build paths. Traversal, absolute, whitespace, leading-zero, metacharacter, and symlink fixtures pass. |
| REL-02 unsigned/confined/mutable release | **Closed in source** | One signed canonical manifest covers archives, checksums, SBOM, and provenance. Installer verification is key-pinned and signature-required; archive extraction rejects absolute/traversal/duplicate/symlink/hardlink/device entries. Existing releases/assets cannot be clobbered. |
| REL-03 destructive upgrades | **Closed** | CLI install stages and health-checks an exact-version binary, swaps without a missing-final gap, retains the previous binary, and rolls back on signal/post-swap failure. macOS build/install use signed staged bundles plus `renameatx_np(RENAME_SWAP)` rollback. |
| REL-04 non-reproducible artifacts | **Closed in implementation; clean-runner proof pending** | Tag commit time drives `SOURCE_DATE_EPOCH`; Go uses trimpath/no VCS/no build ID; GNU tar and gzip metadata are normalized; canonical provenance JSON and deterministic CycloneDX output are validated. Local macOS lacks GNU tar, so the two-clean-builder equality receipt belongs to CI. |
| REL-05 platform proof | **Closed in policy/CI definition** | CI includes Linux release, Linux/macOS Go, Windows portable-package, Linux E2E, Linux race, UI/browser, and vulnerability lanes. Platform docs explicitly limit production runtime support to macOS/Linux; Windows is a portable compile signal, not a product claim. |
| REL-06 workflow trust/privilege | **Closed in source** | Actions are pinned to reviewed commit SHAs; jobs use least privilege; release build and publish are split behind a protected environment; exact signed tag/commit and signer fingerprint are verified in a fresh keyring; publish refuses an existing release. |
| REL-07 dependency/license/SBOM policy | **Mitigated** | Weekly Dependabot covers Go, Bun, and Actions. Pinned `govulncheck` is release-blocking. A real deterministic CycloneDX inventory, canonical provenance, and signed digest envelope ship with artifacts. A formal forbidden-license fixture, SAST policy, external attestation format, and expiring exception ledger remain to be added before broad public rollout. |
| REL-08 broken maintainer entrypoints | **Closed** | `make install` is cross-platform CLI/skill install; `install-cli` and `mac-preview` are explicit; mac-only targets guard `uname`; removed docs-publication targets no longer advertise dead commands; platform support is canonical. |
| REL-09 monolith/duplicate surfaces | **Deferred P2** | Dead fake editor/banner code and duplicate release paths were removed or centralized. The large command package and compatibility wrappers remain bounded architecture debt; split them only with measured dependency/cycle/test-time evidence. |
| REL-10 no green production proof | **Blocked** | This report contains broad local source proof, but no clean signed tag, hosted matrix receipt, signed artifacts, canary transaction, migration restore/downgrade drill, or 24-hour soak. Those are required evidence, not code comments. |

## Verification ledger

| Check | Result on current working tree |
| --- | --- |
| `git diff --check` and `make fmt-check` | PASS |
| Go 1.26.5 compile; Linux/amd64 cross-compile | PASS |
| `go vet ./...` | PASS |
| Focused authority/runtime/Serve/installer/scratch/event-log tests | PASS (89 in final patched lane; earlier focused lanes also green) |
| `go test ./internal/...` | PASS (21) |
| Terminal Markdown dependency compatibility | PASS (4) |
| `govulncheck@v1.6.0 ./...` | PASS: 0 reachable vulnerabilities after dependency/toolchain upgrades; advisories remain in imported but statically unreachable code |
| Full UI tests/typecheck/build/repeat-build hash | PASS |
| Full SwiftPM suite | PASS (18) |
| `make release-test` | PASS after making the target fail-fast; GNU-tar reproducibility fixture SKIPPED locally and required in Linux CI |
| `tusker validate --branch-policy --json` | PASS: 0 errors, 803 events; 110 non-blocking warnings remain in existing task/doc hygiene |
| `tusker skill doctor --strict --json` | PASS: 0 errors; warnings only |
| E2E suites outside socket sandbox | PASS: contract convergence 1, execution observability 1, crash recovery 13; 15 total |
| Full backend suite | **PASS: 2,060 tests, 0 failures** under `go test -timeout=20m -p=1 -parallel=1 ./cmd/tusker -count=1`; the serialized rerun used isolated validation state and completed after all late regression repairs |

### Defects found by the final gates

The final validation pass was allowed to change the implementation; it was not
treated as ceremonial proof. It found and repaired the following additional
defects before the 2,060-test green rerun:

1. `make release-test` continued after a failed fixture and could return the
   status of a later passing test. The target now runs under `set -eu` and the
   same SBOM failure correctly makes the gate fail.
2. the local Go 1.26.0 toolchain plus locked dependencies produced 15 reachable
   vulnerability findings. The release floor is now Go 1.26.5, Goldmark is
   1.7.17, and `x/text` is 0.39.0; the rerun reports zero reachable findings.
3. fresh bootstrap on a machine with no usable local runner catalog wrote
   `profiles: {}`, which correctly cleared inherited built-ins but left the
   inherited `default` reference dangling. Empty discovered catalogs are now
   omitted, an isolated-HOME regression proves the built-in default remains
   valid, and contract-convergence E2E passes.
4. the crash-recovery fixture wrote a legacy root config after `init`; the new
   managed config correctly had higher precedence and kept automation disabled.
   The durability fixture now writes its explicit authority/runner config to the
   managed path instead of accidentally testing obsolete precedence. The final
   contract-convergence, execution-observability, and crash-recovery suites pass
   all 15 E2E tests outside the socket-restricted sandbox.
5. the package `TestMain` deferred removal of each isolated state root and then
   called `os.Exit`, which means the defer could never run. Repeated subprocess
   tests leaked tens of gigabytes and eventually produced false SQLite/TempDir
   failures. Every exit path now performs explicit cleanup; the final backend
   run gained disk headroom instead of exhausting it.
6. bounded runner completion could leave descendants alive when the leader
   exited while they held output pipes, and wrapper lease cancellation did not
   cancel the child context. `exec.ErrWaitDelay` now fences the owned group,
   wrapper cancellation produces authoritative interrupted status, and the
   Darwin landing sandbox uses a canonical toolchain `PATH` rather than an
   inaccessible host shim.
7. dispatch disk preflight treated an empty workspace as the process working
   directory, duplicated healthy state-root probes, and could consume a
   paused-to-recovered transition before Serve observed it. Blank workspaces
   are omitted, healthy observations are reused, blocked state is freshly
   remeasured, and the recovery marker survives the workspace decision exactly
   once. Disk-pressure and fair-dispatch suites pass.
8. review-result idempotency included retry timestamps in its immutable
   revision, so an otherwise exact retry crossing an RFC3339 second boundary
   conflicted. Replays now compare a timestamp-independent immutable payload
   fingerprint while verdict, finding, and summary changes still fail closed.
9. profile bootstrap tests mixed obsolete root config with authoritative managed
   config, obscuring the actual precedence contract. Bootstrap writes the
   managed layer atomically, legacy-only projects migrate without mutation, and
   an adversarial A-versus-B regression proves a newer managed default always
   wins over a stale legacy default.
10. first-writer event-log processes read sequence metadata before acquiring the
    lock, allowing them to observe different generations of the log/lock pair.
    The transaction now creates/opens and locks the owner-only inode first, then
    reads metadata and verifies the persisted lock identity. Fifty consecutive
    cross-process stress runs pass.
11. the unbounded runner securely opened its raw log in Go but the child shell
    reopened the pathname with `>>`; cancellation watcher goroutines could also
    race `Wait` and signal a reused process-group number. Output now uses only
    the parent-owned descriptor, cancellation is an `exec.CommandContext`
    pre-`Wait` fence, and stale status removal is descriptor-relative,
    owner-only, no-follow, and covered by symlink-parent tests.

## Human and hosted release gate

The source deliberately contains a non-key marker. A human release owner must:

1. generate and protect a dedicated Minisign key; commit only the public key at
   `scripts/release-minisign.pub` and replace the installer marker;
2. commit the trusted release-tag public key at
   `scripts/release-tag-signer.asc`;
3. configure the protected `release` environment secret
   `TUSKER_MINISIGN_SECRET_KEY` and variable
   `TUSKER_RELEASE_TAG_SIGNER_FINGERPRINT`, with required reviewers and protected
   `v*` tag rules;
4. produce a clean signed tag pointing to the approved commit and retain all CI
   lane results, the two-builder hash comparison, signed envelope verification,
   malicious-installer results, and artifact hashes; and
5. run a canary install/upgrade/rollback, historical migration backup/restore and
   downgrade drill, then the 24-hour DB/log/memory/SSE soak.

Until those steps pass, `scripts/install.sh` and `make release-artifacts` are
supposed to refuse production use. That refusal is a working safety feature,
not an unfinished shortcut.
