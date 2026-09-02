# Agent Feedback

- context: Local source install and agent capability discovery on macOS.
- friction: The top-level capabilities read_only field was mistaken for global CLI authority, while make install refreshed CLI and skills but left the installed app and running daemon on older revisions.
- product-idea: Keep query mutability out of the capability manifest and make one local install path converge every supported runtime surface.
- impact: Agents report false tracker blockers and the machine can run mixed Tusker revisions after an apparently successful install.
- related: cmd/tusker/capabilities_cmd.go, Makefile, apps/mac/TuskerBar/scripts/install-app.sh
- dedupe-key: capabilities-install-surface-drift
