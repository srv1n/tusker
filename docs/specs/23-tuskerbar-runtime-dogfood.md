---
capsule:
  what: "A one-task, app-owned runtime smoke test for the normal TuskerBar workflow."
  use_when:
    - "Proving that a PM can start a reviewed wave from TuskerBar without terminal daemon commands."
  skip_when:
    - "Changing runner policy, scheduled promotion, or the full desktop settings experience."
---

# TuskerBar runtime dogfood

## Product outcome

A PM can use TuskerBar as the normal local control surface: the app starts its
bundled daemon, a reviewed low-risk wave can be started from the Delivery
screen, and work stays off `main` unless a separately configured departure
authorizes promotion.

## Requirements

| ID | Outcome |
|---|---|
| R1 | The Mac-app documentation clearly says `make mac-preview` is the normal local workflow and that `tusker daemon service start` is not part of it. |
| R2 | A fresh one-task delivery plan can be reviewed and started from TuskerBar while project automation remains opt-in, concurrent work remains capped, and no scheduled promotion is enabled. |
| R3 | The task proves the normal daemon owns a fresh local poll and does not move `main` as part of the dogfood run. |

## Constraints

- One low-risk documentation task only.
- Use the project’s normal lower-tier execution routing; do not add a model ID
  to the task contract.
- No daemon-service installation, service start, scheduled-promotion change,
  release, credentials, or push.
- `main` must not move as a result of this dogfood wave.

## Acceptance boundary

This is a runtime/control-surface proof, not the final desktop daemon UX. A
future product slice will make runtime health and restart controls visible in
the TuskerBar menu and reconcile the app-managed and headless-service modes.
