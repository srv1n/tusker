# Model Settings acceptance

The production `ProfilesSection` is rendered with deterministic API data; the
fixture does not stand in for API proof. Backend request tests are recorded in
the adjacent API report.

| Check | Result |
| --- | --- |
| Focused UI test | PASS: 3 tests, 26 expectations |
| TypeScript | PASS: `tsc --noEmit` |
| Full UI suite | PASS: 179 tests, 1092 expectations |
| Desktop render | PASS: 1440 x 1400, SHA-256 `fb1c2cd810b7185aea1fe805038d53d5bcf99bbd86d8b4ae5099d85a95b65adc` |
| Narrow render | PASS: 390 x 1600, SHA-256 `ad89095b7cc9f059de96466fe023b87b33a302c14059418ed1294dfee5fbda45` |

Artifacts:

- `desktop.png`
- `narrow.png`
- `critique.md`

The UI supports global/project scope, per-lane ordered profile lists,
project-field reset, guarded saves that retain drafts after errors, manual
profile editing, unavailable labeling, local conformance, and explicit live
print/timer canaries. It does not launch a canary during this acceptance run.
The screenshots received a fresh screenshot-only self-critique; independent
visual review remains outstanding.
