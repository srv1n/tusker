# Human approval continuation

## Implemented flow

`HumanActionCard` now calls the existing authenticated gate mutation directly: click Approve -> `POST /api/gates/<gate>/satisfy` -> material-locked gate transition and audit event -> query invalidation and task/wave reload -> existing authorized-wave frontier reconciliation. There is no approval receipt, signing key, challenge, native bridge, second confirmation, or biometric/password prompt.

The action remains scoped to its gate and project. The server records its configured actor without upgrading `agent:*` to `human:*`. Existing wave authorization may admit newly eligible work; inert waves remain inert, paused waves remain paused, duplicate frontier reconciliation queues nothing twice, and dependencies/verification remain enforced.

## Removed

- `HumanDecisionReceipt.swift` and its Swift tests.
- `human_control_receipt.go`, signed-receipt verification tests, and receipt fixtures.
- Secure Enclave/Keychain approval-key creation and runtime public-key provisioning.
- Human-receipt challenge/submit routes, challenge table creation, payload serialization, expiry/replay handling, and signature verification.
- Both Mac `requestHumanReceipt` handlers and injected JavaScript bridge.
- UI receipt transport/types and native-only approval instructions.

Unrelated signing, credentials, Keychain usage, provider receipts, completion/landing authority, and agent-access approval infrastructure were not changed.

## Verification

| Boundary | Result | Evidence |
|---|---|---|
| Focused Go behavior | PASS | `go test ./cmd/tusker -run '^(TestServeHumanActionProjection|TestHumanApprovalContinuation|TestServeDurableMutationsRequireConfiguredOperator|TestServeDurableMutationsRejectForgedOperator|TestServeOperatorActorUsesOnlyExplicitConfiguration)$' -count=1` - 24 tests. Covers durable gate readback, authenticated/refused mutations, forged actor refusal, authorized continuation, duplicate admission, inert scope, and pause. |
| Focused UI | PASS | `bun test --cwd internal/serve/ui test/human-approval-continuation.test.ts tests/human-action.test.ts` - 5 tests, 38 expectations. Covers one action button, shared mutation usage, scope copy, and no native receipt call. |
| UI typecheck/build | PASS | `bun run --cwd internal/serve/ui build`. Generated assets were refreshed; Vite reported only its existing large-chunk advisory. |
| Native package fixture | PASS | `swift test --package-path apps/mac/TuskerBar` - 19 tests. Confirms the Mac target builds and remaining bridges work after approval bridge removal. |
| Approval-specific source scan | PASS | No `HumanDecisionReceipt`, `HumanReceiptNativeKey`, `/api/human-receipts`, `human_control_challenges`, `requestHumanReceipt`, or approval public-key provisioning remains in the Go, Swift, or UI approval path. |
| Installed Mac app | NOT RUN | Installation, daemon launch, and real/disposable approval were not authorized. Remaining proof: install an explicitly identified candidate, open a disposable human gate, click once, verify inline success and no modal/biometric/password prompt, reload, confirm durable gate state and at-most-one continuation. |

The fixture suite does not establish installed-app behavior.

The canonical direct-wave spec passes `tusker docs check`. `HANDOFF.md` and `RUN.md` are shipped skill references outside managed docs roots, so the docs checker correctly reports them as unmanaged. Repository-wide `tusker validate --json` remains failed on 172 unrelated legacy/stale records; FLW-T-0055 itself has a current contract revision and its amended acceptance renders correctly.
