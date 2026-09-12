---
subject: agent-access-journey
status: fixture-pass
---

# Agent access journey

This artifact records provider-free proof for the existing-profile upgrade and
the native callback boundary. It does not claim installed-provider or paid/live
qualification.

## Fixture journey

`TestAgentAccessProviderFreeJourney` proves the following sequence in one
disposable fixture:

1. A legacy `danger-full-access` profile keeps its stable ID, model, reasoning
   level, display name and tier memberships when explicitly saved with the
   `tusker.agent-access/v1` project-access object.
2. Routine `git status` and `git add` requests are accepted by the project-write
   callback without creating an approval row.
3. `rm -rf build` creates one immutable `allow_once` request and resumes only
   after the human-scoped decision settles it.
4. `rm -rf private` is denied before approval persistence when the path is a
   configured private-folder exclusion.
5. A Codex route with no affirmative private-folder controls resolves as
   `unsupported`, so the provider is unavailable rather than widened.

The rendered `ProfilesSection` fixture in
`tests/agent-access-journey.test.ts` separately proves that Cancel leaves the
legacy profile untouched, `Use project access` is explicit, the model/reasoning
and tier references remain in the draft, and Save sends the complete access
object without the legacy preset. It also renders an unavailable provider card.

## Checks

```text
go test ./cmd/tusker -run '^TestAgentAccessProviderFreeJourney$' -count=1 -timeout=120s
PASS

bun test --cwd internal/serve/ui tests/agent-access-journey.test.ts
PASS (1 test, 11 assertions)
```

## Qualification boundary

Installed CLIs, resident-daemon callbacks, provider authentication, and paid
model turns were not invoked by these checks. Run those only as separately
authorized installed/live qualification, and record their route/version and
provider result independently from this fixture evidence.
