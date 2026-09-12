# Automatic profile defaults

Implemented AAC-T-0008 with the existing `tusker.agent-access/v1` contract.

| Journey | Result |
| --- | --- |
| New profile | Project work, internet on, destructive actions blocked, no references, inherited protected folders shown |
| Existing profile | Preset/access remains unchanged until an explicit edit is saved; Cancel writes nothing |
| Protected folders | Shared paths are labeled Inherited; profile additions are staged and labeled This profile |
| Compatibility | Provider-free setup stops before `Prepare`, is keyed to the draft, shared revision and route version, and ignores stale setup/live-test responses |
| Advanced | Reasoning, internet, destructive Ask, tests, native values, display name and diagnostics are disclosed on demand |

Commands and results:

- `go test ./cmd/tusker -run 'TestServeRunnerConformanceSetupResolvesDraftAccessWithoutModel|TestAgentAccessEditor|TestAccessProfilePublishesFixedCommandPolicy' -count=1 -timeout=120s` — PASS, 4 cases.
- `go test -race ./cmd/tusker -run 'TestServeRunnerConformanceSetupResolvesDraftAccessWithoutModel|TestAgentAccessEditor' -count=1 -timeout=120s` — PASS, 3 cases.
- `bun test --cwd internal/serve/ui test/agent-access-profiles.test.ts test/agent-access-command-policy.test.ts tests/agent-access-journey.test.ts` — PASS, 6 tests / 66 assertions.
- `bun test --cwd internal/serve/ui tests/profiles-interaction.test.ts` — PASS, 1 test / 33 assertions, including delayed stale setup/live responses and a revision-conflict draft-preservation retry.
- `bun run --cwd internal/serve/ui typecheck` and `bun run --cwd internal/serve/ui build` — PASS.

Rendered fixture evidence:

- `automatic-defaults/1280-add-profile.png`
- `automatic-defaults/390-add-profile.png`
- `automatic-defaults/390-add-profile-200-percent.png`
- `automatic-defaults/390-add-profile-200-percent-actions.png`

The browser journeys use fixture providers. Installed Codex, Claude, Muse and ACP enforcement and callback liveness were **NOT RUN**. Saving a profile and compiling native arguments do not qualify an installed route; unsupported required folder protection remains a launch blocker.
