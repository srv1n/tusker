# Agent Feedback

- context: Authoring SRV-T-0009/0010/0012 (Bun-only serve UI tasks); tusker validate flagged VERIFICATION_PROOF_MISSING despite exact 'bun --cwd internal/serve/ui test ...' checks.
- friction: v7VerificationCheckLooksExact (cmd/tusker/v7_validation.go:595-614) allowlists go/npm/pnpm/yarn/npx/node/python/cargo/etc as exact verification commands but omits 'bun'. Pure-frontend tasks always warn unless they carry a go-test row or a 'command:' marker.
- product-idea: Add 'bun ' to the exact-command prefix allowlist so UI-only tasks validate clean without a workaround.
- impact: Every Bun-only serve UI task trips a spurious proof warning; authors add noise ('command:' prefix or an unrelated go-test row) to silence it.
- related: SRV-T-0009, SRV-T-0010, SRV-T-0012, cmd/tusker/v7_validation.go
- dedupe-key: validator-bun-exact-proof
