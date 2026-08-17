---
title: "Platform support"
subject: platform-support
part_of: overview
keywords: [platforms, macos, linux, windows, release, support, launchd, installer]
status: canonical
read_when: "You need to know which Tusker surfaces are production-supported on a host, which behaviors are gated by OS, or what a release/CI lane actually covers."
skip_when: "You are changing platform-independent task contracts, proof, or gate policy — read [[tasks-and-proof]], [[proof-and-closeout]], or [[gates]]."
sources:
  - Makefile
  - scripts/release-build.sh
  - scripts/install.sh
  - .github/workflows/ci.yml
  - cmd/tusker/v7_full_gate_provider.go
  - cmd/tusker/v7_document_write.go
---

# Platform support

Tusker has an explicit support boundary. A package compiling somewhere is not
the same thing as the product being supported there.

| Host | Production-supported surfaces | Not supported |
| --- | --- | --- |
| macOS 14+ on arm64 or amd64 | CLI, resident daemon, Serve UI, launchd integration, shell installer, TuskerBar | Older macOS releases |
| Linux on arm64 or amd64 | CLI, resident daemon, Serve UI, shell installer | TuskerBar and launchd management; operators must use their host service manager |
| Windows | Portable `internal/...` Go packages are compile/test signals only | CLI, daemon, Serve server, shell installer, and release artifacts |

Windows is excluded structurally, not by policy alone: `cmd/tusker` compiles
Unix-only syscall surfaces with no build tags — `syscall.Flock` and `O_NOFOLLOW`
for the document write lock (`v7_document_write.go:61`) and `syscall.Stat_t`
ownership checks — so the CLI binary cannot be built for `windows/*` at all.

The default release matrix is `darwin/{arm64,amd64}` and `linux/{arm64,amd64}`
(`Makefile:11`, `scripts/release-build.sh`). The build script also *accepts*
`windows/amd64` as an override target, but that target fails at compile for the
reason above; treat the four-way matrix as the real one. CI exercises the full Go
suite on Linux and macOS, the portable `./internal/...` packages on Windows
(`ci.yml` `go-matrix`), Linux end-to-end tests, a Linux race lane, a pinned
govulncheck lane, and the Serve UI gate on Linux. A green portable Windows lane
must never be described as Windows product support.

## Behavior gated by host OS

| Surface | Rule |
| --- | --- |
| Certified full-gate provider | The trusted-provider transport is **macOS-only**: `verifyV7TrustedProviderExecutable` and `verifyV7ImmutableProviderAuthority` refuse when `runtime.GOOS != "darwin"` rather than fall back to a pathname check. Promotion gates needing a certified provider receipt therefore only complete on macOS — see [[gates]]. |
| Daemon service management | launchd install/start/status is macOS-only; other hosts run `tusker daemon run` under their own service manager — see [[orchestration]]. |
| ACP adapter install | Refused on any host that is not darwin or linux, along with symlinked or group/world-writable trees — see [[runners-and-acp]]. |
| TuskerBar | macOS only; every `mac-*` Make target depends on `require-macos` (`Makefile:45`) before invoking Swift, codesign, launchd, or `open`. |

## Maintainer entrypoints

- `make install` installs the cross-platform CLI and Codex/Claude user skills.
- `make install-cli` installs only the CLI and refreshes existing user skills.
- `make mac-preview` is the macOS composite path: it installs the CLI/skills,
  builds and signs TuskerBar, installs it through an atomic swap helper, and
  `open`s it (`apps/mac/TuskerBar/scripts/install-app.sh`).

The shell installer requires `curl`, Python 3, `minisign`, and either
`shasum -a 256` or `sha256sum`. It verifies the signed release envelope before
extracting anything and fails closed when the production public key has not
been provisioned.
