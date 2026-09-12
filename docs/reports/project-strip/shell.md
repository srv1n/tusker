# Compact project navigation: browser proof

## Result

PASS against an isolated Playwright fixture using the local Vite server at
`http://127.0.0.1:5193`. The matrix contains 0, 1, 13 and 50 projects, including
a long name and a project with an attention badge. It performs no mutating request.

## Command and runtime

```text
node internal/serve/ui/test/project-strip.browser.mjs
```

Headless Chrome via the already-installed Playwright module; Node reported
`v24.19.0`. The proof used 1440×1000, 1440×900, 1024×900 and 390×844 viewports.

## Scenarios

- Desktop: one strip, all 13 projects once, no permanent sidebar, no page-level horizontal overflow, icon-only Search and App Settings.
- Project counts: 0 and 1 stay arrow-free; 1 supports pinning; 50 overflows and focus-reveals the last chip.
- Narrow: native strip overflow, end arrow, no body overflow, keyboard focus reveals the last chip.
- Persistence: project switching keeps mounted order; pinning moves and persists first; project Knowledge route restores after switching away and back.
- Scope and regression: global and project Settings fully render without browser/error-boundary failures; global Settings clears project scope; project Settings stays scoped; back/forward restores route ownership; Work/Documents and Plan/Board/Trains/Diagnostics remain reachable; Add project and task Search remain reachable; embedded `/panel?shell=1` keeps the triage shell without the strip.

## Screenshots

- [Before desktop](./before-desktop.png)
- [After desktop](./after-desktop.png)
- [After narrow](./after-narrow.png)
- [After overflow](./after-overflow.png)
- [After global Settings](./after-global-settings.png)
- [After project Settings](./after-project-settings.png)

The screenshot set is fixture-backed rendered evidence. It does not claim live
daemon interaction, installed Mac relaunch behavior or human visual approval.

## Live smoke

The strengthened live mode now exercises project switching, Documents, both
settings scopes and back/forward with GET-only network traffic. The resident
Serve instance did not yet contain the generated strip build:

```text
TUSKER_PROJECT_STRIP_LIVE_URL=http://127.0.0.1:7420 node internal/serve/ui/test/project-strip.browser.mjs --live
```

Observed result: **OPEN** — timed out waiting for `[data-project-strip]`. No
resident process or installed bundle was restarted, and no mutating request was
sent. Fixture interaction assertions remain the authoritative behavioral proof.
