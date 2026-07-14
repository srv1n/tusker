# TuskerBar

TuskerBar is the native AppKit shell for Tusker: a normal resizable app window
plus a fast floating menu-bar panel. It owns no task state, but it bundles and
supervises the Tusker runtime that provides the local daemon and Serve UI at
`http://127.0.0.1:7420`.

## Build and run

```sh
make mac-app
open apps/mac/TuskerBar/.build/TuskerBar.app
```

Run those commands from the repository root. `make mac-app` builds the Serve
UI, Go runtime, and Swift release binary; embeds the Go runtime in the `.app`;
then signs both executables with a Developer ID Application identity when one
is available or an ad-hoc signature otherwise.

For the normal local install, run this from the repository root:

```sh
make install
```

That installs the CLI/skills and copies + opens `TuskerBar.app` at
`~/Applications/TuskerBar.app`. To install just the Mac app, use
`make mac-install`; use `MAC_APP_DIR=/somewhere make mac-install` for another
destination. `make mac-open` reopens it and `make mac-uninstall` removes it.

Opening Tusker starts its signed, bundled `tusker daemon run` runtime when the
default local endpoint is not already healthy. The daemon owns Serve, survives
UI window/app restarts so active task runs are not killed, and is reused on the
next launch. A custom non-local base URL is never managed by the app.

From a source checkout, `make mac-preview` builds, installs, and opens this
self-starting app. No second terminal is required. `tusker serve` remains a
developer diagnostic, not the normal Mac-app launch path. Reinstalling the app
gracefully restarts an older app-bundled daemon so UI and API versions match.

## Using the two surfaces

- Launching Tusker opens a normal resizable window. Use the green traffic-light
  button or **Window → Enter Full Screen** for macOS full screen.
- Closing that window leaves the lightweight menu-bar app running. Click the
  Tusker menu-bar icon for the compact triage panel; press Escape or click away
  to dismiss it.
- Right-click the menu-bar icon for window, full-screen, browser, settings, and
  quit actions. **Quit TuskerBar** exits both surfaces.
- During startup, both windows show an opaque native progress/retry screen
  instead of an empty WebKit view. Failures point to
  `~/Library/Application Support/tusker/logs/app-daemon.log`.

Notifications and other user-notification APIs require the signed bundle. A
`swift run` process is useful for iteration but is not a supported notification
runtime, so the bundle path is intentional from the first shell task.

## Deep links and web bridge

The bundle registers `tusker://`:

- `tusker://task/MAC-T-0001` opens that task in the panel.
- `tusker://spotlight/task%3Atusker%3AMAC-T-0001` opens the project-qualified
  Spotlight task route in the main window.
- `tusker://open?path=%2Fp%2Ftusker%2Fwork` opens an arbitrary same-origin
  Serve UI path.

TuskerBar also indexes registered projects, tasks, and gates in macOS
Spotlight. Search a project name, task title, or exact ID such as `MAC-T-0001`
or `MAC-G-0007` and
select the result to open the native Tusker window at that project or task.
The index refreshes on launch and as the Serve event stream changes. Indexed
items use a project-qualified identity so duplicate task IDs remain distinct.

When the panel is on its configured base URL, it exposes a deliberately small
`window.tuskerShell` API:

| API | Effect |
|---|---|
| `openFull(path)` | Opens `path` in Tusker's normal native window; that window supports macOS full screen. |
| `closePanel()` | Hides the floating panel. |
| `notify({title, body, path})` | Posts a local notification when permission is available. |
| `setBadge(count)` | Sets an advisory native badge; `/api/summary` wins on the next refresh. |
| `pickFolder()` | Opens a single-selection directory picker and resolves with the selected absolute path, or `undefined` after cancellation. |
| `version` | Read-only bundle version string. |
| `onNavigate(path)` | Optional web-provided hook used by native deep links to avoid reloading the SPA. Return `true` when handled. |

The bridge is removed after a navigation to any non-configured origin; native
message handling also rejects that origin.
