# Release integrity and signing gate

> **Scope:** This is an optional, future public-distribution design note. It
> is not a launch gate for Tusker's current source/internal orchestrator. Start
> with [GitHub distribution readiness](github-distribution-readiness.md) for the
> current posture, staged re-entry gates, platform matrix, and the rule that a
> future signing policy chooses one mechanism rather than requiring both GPG and
> Minisign. Do not provision keys or publish artifacts from this document alone.

Tusker releases are immutable, reproducible, and fail closed. A release must
come from a clean checkout whose `vMAJOR.MINOR.PATCH` tag points at `HEAD` and
passes `git verify-tag`. The GitHub `release` environment and repository tag
ruleset are the human/protected authority boundary.

## Human key-provisioning blocker

Production publishing is intentionally blocked until the release owner:

1. creates a dedicated CI Minisign keypair outside this repository with
   `minisign -G -W` (Minisign 0.12 cannot unlock an encrypted secret key
   noninteractively; keep the unencrypted key only in the protected secret
   manager and an appropriately secured offline recovery store);
2. stores the secret-key **content** in the protected
   `TUSKER_MINISIGN_SECRET_KEY` GitHub environment secret;
3. commits only the public-key file as `scripts/release-minisign.pub`;
4. exports the GPG public key used for release tags to
   `scripts/release-tag-signer.asc` (the private key stays outside the repo);
5. replaces `TUSKER_RELEASE_PUBLIC_KEY_NOT_PROVISIONED` in
   `scripts/install.sh` with that file's public-key line; and
6. configures `TUSKER_RELEASE_TAG_SIGNER_FINGERPRINT` in the protected release
   environment; and
7. requires signed tags and restricted tag creation for `v*` in GitHub rules.

Do not generate a key in CI, commit a secret key, or bypass this gate. The
release helper checks that the committed public key and the installer's pinned
key are identical, then verifies its own manifest signature before staging the
immutable release directory.

`make tag-release RELEASE_VERSION=vX.Y.Z` creates a GPG-signed tag with the
maintainer's configured Git signing key and immediately verifies it in a fresh
temporary keyring against `scripts/release-tag-signer.asc` and the configured
fingerprint. An annotated but unsigned tag is never release-authoritative.

Protected Linux jobs install Minisign 0.12 from the upstream release using a
repository-pinned archive SHA-256. They do not assume the mutable hosted-runner
image happens to contain the signer.

## Release envelope

`MANIFEST.sha256` is the canonical signed envelope. It hashes every archive,
`checksums.txt`, `provenance.json`, and the CycloneDX SBOM. The sole detached
signature is `MANIFEST.sha256.minisig`. The installer verifies this signature
with the pinned key, finds exactly one archive entry, verifies its digest, then
applies a strict tar-member policy before extraction.

The build derives `SOURCE_DATE_EPOCH` from the tagged commit; callers cannot
choose another timestamp. It uses deterministic Go build flags, GNU tar with
normalized ownership/mode/mtime and sorted members, and `gzip -n`.

## Rollback

CLI installation copies the existing final binary to a transaction rollback
file while leaving the live path in place, validates the new exact version,
and atomically renames over the final path. Any signal, post-swap health error,
or requested skill-install failure restores the old binary. A successful
upgrade preserves it as `tusker.previous`.

TuskerBar uses macOS `renameatx_np(..., RENAME_SWAP)` so app replacement and
rollback never create a missing-path interval. Code-signature verification is
performed before and after the swap; a failed `open` rolls back.
