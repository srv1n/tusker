---
title: "Platform support"
subject: platform-support
part_of: overview
status: canonical
---

# Platform support

Tusker can run on desktop and server computers. Support differs by operating
system.

## Build tools

The local build uses Go and Bun 1.3.14. The macOS app also uses Swift. Release
scripts use shell tools, Python 3, SHA-256 tools, GNU tar for deterministic
archives, and Minisign.

## macOS

The Makefile builds the CLI, daemon, Serve web app, and TuskerBar. TuskerBar is
an AppKit app. The daemon service installer uses launchd and the canonical
Application Support state root.

`make mac-app` builds and signs the app. It uses Developer ID when available
and ad-hoc signing otherwise. An ad-hoc build is for local use.

`make install` and `make mac-preview` refresh the CLI, Codex and Claude user
skills, TuskerBar, the bundled daemon, and the dormant launchd daemon executable
in one build. They stop an older launchd daemon before opening the newly
installed app.

## Linux

The release matrix builds the CLI for amd64 and arm64. The Go daemon and Serve
code are in the same binary. TuskerBar and the launchd installer do not apply.

## Windows

The release script accepts `windows/amd64`, and CI compiles portable Go code.
The Makefile release matrix does not include Windows by default. The macOS
installer, daemon service path, shell installer, and TuskerBar do not provide a
Windows product path.

## Release boundary

The release workflow requires a trusted signed tag and signing secrets. The
release script fails when the committed public keys or required signing tools
are missing. A local build is not a public release.

## Code sources

- `Makefile`
- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- `scripts/release-build.sh`
- `scripts/release-validate.sh`
- `cmd/tusker/daemon_service.go`
- `apps/mac/TuskerBar/scripts/build-app.sh`
