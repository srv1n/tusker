---
title: "Platform support"
subject: platform-support
part_of: overview
keywords: [platforms, macos, linux, windows, release, support]
status: canonical
read_when: "You need to know which Tusker surfaces are production-supported on a host or included in a release."
skip_when: "You are changing platform-independent task contracts only."
---

# Platform support

Tusker has an explicit support boundary. A package compiling somewhere is not
the same thing as the product being supported there.

| Host | Production-supported surfaces | Not supported |
| --- | --- | --- |
| macOS 14+ on arm64 or amd64 | CLI, resident daemon, Serve UI, launchd integration, shell installer, TuskerBar | Older macOS releases |
| Linux on arm64 or amd64 | CLI, resident daemon, Serve UI, shell installer | TuskerBar and launchd management; operators must use their host service manager |
| Windows | Portable `internal/...` Go packages are compile/test signals only | CLI, daemon, Serve server, shell installer, and release artifacts |

Release archives are built only for `darwin/{arm64,amd64}` and
`linux/{arm64,amd64}`. CI exercises the full Go suite on Linux and macOS, the
portable internal packages on Windows, Linux end-to-end tests, and a Linux race
lane. A green portable Windows lane must never be described as Windows product
support.

## Maintainer entrypoints

- `make install` installs the cross-platform CLI and Codex/Claude user skills.
- `make install-cli` installs only the CLI and refreshes existing user skills.
- `make mac-preview` is the macOS composite path: it installs the CLI/skills,
  builds and signs TuskerBar, installs it transactionally, and opens it.
- Every `mac-*` Make target refuses non-macOS hosts before invoking Swift,
  codesign, launchd, or `open`.

The shell installer requires `curl`, Python 3, `minisign`, and either
`shasum -a 256` or `sha256sum`. It verifies the signed release envelope before
extracting anything and fails closed when the production public key has not
been provisioned.
