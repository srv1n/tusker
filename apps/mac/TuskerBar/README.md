# TuskerBar

TuskerBar is the macOS shell for Tusker. It has a normal window and a menu-bar
panel. It does not own task records.

## Runtime behavior

The app uses `http://127.0.0.1:7420` by default. At startup it checks that
endpoint. It uses a healthy service when one exists. Otherwise, it starts the
bundled `tusker daemon run` process and waits up to 15 seconds for health.

The app manages only the default local endpoint. It does not start a process
for a custom base URL. Runtime logs go to:

`~/Library/Application Support/tusker/logs/app-daemon.log`

The source authority is `Sources/TuskerBar/RuntimeSupervisor.swift`.

## Build and install

Run these commands from the repository root:

```sh
make mac-app
make mac-install
make mac-open
```

`make mac-app` builds the Serve UI, the Go binary, and the Swift app. The build
script puts the Go binary in the app bundle. It uses a Developer ID identity
when one is available. Otherwise, it uses an ad-hoc signature.

To refresh every local Tusker surface from this checkout, run either command:

```sh
make install
make mac-preview
```

Both install the CLI and user skills, replace the app in `~/Applications`, stop
an older launchd daemon, atomically refresh its dormant executable, and open the
app with the just-built bundled daemon. Run `make mac-uninstall` to remove the
app. Set `MAC_APP_DIR` to use another install directory.

## Surfaces

- The main window shows the full Serve app.
- The menu-bar panel shows a small local view.
- Closing the main window leaves the menu-bar app open.
- Quitting TuskerBar closes the app. The daemon process can continue.

## Deep links

The bundle registers the `tusker://` scheme.

- `tusker://task/MAC-T-0001` opens the task document route.
- A project-qualified Spotlight item opens the matching project or task.
- `tusker://open?path=<encoded-path>` opens a same-origin Serve path.

`DeepLinks.swift` parses the links. `Spotlight.swift` builds the searchable
items. Both route task links to `/p/<project>/docs?path=<task-id>`.

## Web bridge

The local page can call a small native bridge:

| Call | Result |
| --- | --- |
| `openFull(path)` | Open a same-origin path in the main window. |
| `closePanel()` | Hide the panel. |
| `notify(...)` | Ask macOS to post a local notification. |
| `setBadge(count)` | Set an advisory badge. |
| `pickFolder()` | Open one directory picker. |
| `version` | Read the bundle version. |

The app removes the bridge after navigation to another origin.

See `docs/system/platform-support.md` and `docs/system/serve-ui.md` for the
system contract.
