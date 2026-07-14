# Agent Feedback

- context: Upgrading the launchd daemon from the TuskerBar bundled runtime on a five-project registry.
- friction: daemon service install returned INVALID_TRANSITION after its fixed 5s readiness window, although the new daemon completed a fresh poll and was healthy less than a second later.
- product-idea: Make service-install readiness use the daemon heartbeat/startup phase or a realistic configurable deadline, and report still-starting separately from failed.
- impact: False failure sends operators toward unnecessary repair after a successful binary upgrade.
- related: apps/mac/TuskerBar startup; cmd/tusker/daemon_service.go
- dedupe-key: daemon-service-readiness-timeout
