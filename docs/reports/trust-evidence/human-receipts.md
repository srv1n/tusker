# Human receipt evidence

Status: focused contract validation passed; one real AppKit click remains.

Human-owned gates no longer accept an agent actor or an arbitrary `human:`
string. Serve issues a five-minute, single-use challenge for an open
human-owned gate only. The challenge binds the project, gate, configured
operator, current material revision, action digest, nonce, and native key ID.
TuskerBar signs the exact LF-delimited v1 payload using a P-256 Keychain/Secure
Enclave key; Serve receives only its SPKI public key through
`TUSKER_HUMAN_RECEIPT_PUBLIC_KEY`. Private key material never enters JavaScript,
the CLI, or a readable file.

The native sheet receives server-authored `gate_title`, `action_text`, and
`verification_text`. Submit verifies a DER ECDSA signature encoded as unpadded
base64, current gate material/action, expiry, and atomic single use. A material
change revokes the unused challenge. The receipt JSON and signature are then
recorded as gate evidence. This is limited to gates owned by `human:`; it does
not add approvals to task start, review, or close.

The native transport fixture executed:

```sh
swift test --package-path apps/mac/TuskerBar --filter HumanDecisionReceiptTests
```

Result: PASS (4 tests): one capability fetch, capability headers on both POSTs,
direct challenge fields, `ok:true` submit, and no caller-supplied submit action.
That is native transport evidence, not evidence of the Serve verifier.

`TestTrustHumanReceipts` covers the agreed native canonical vector, a valid
native-compatible signing flow, forged, cross-gate, replayed, expired, changed
material (including a body-only Markdown change with a stale state revision),
and free-text-human negative cases. It passed on local macOS arm64:

The lifecycle steward should run:

```sh
scripts/with-validation-lock.sh -- go test ./cmd/tusker -run '^TestTrustHumanReceipts$' -count=1 -v
```

Result: PASS (1.595s). The remaining A3 gate is one real human click in the native
AppKit sheet with a redacted receipt and visible gate result. No CLI command or
agent text can substitute for that acceptance.
