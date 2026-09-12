# Slash prose lint

| Case | Expected | Result |
| --- | --- | --- |
| `small/medium/large` | Plain prose | PASS |
| `10/100/1000-document` | Plain prose | PASS |
| `cmd/tusker/foo` | Code path | PASS |
| `guide.md` | Filename | PASS |
| `` `renderTask` `` | Code span | PASS |

The shared detector serves both task top layers and managed-document openings. Existing appendix, table, fenced-code, mixed-case, priority, and risk behavior remains unchanged.

## Verification

PASS: `go test ./cmd/tusker -run 'TestTopLayerLint|Test.*DocOpening' -count=1 -v`

- 10 top-level tests ran, including the new slash-prose case and eight mixed-case subtests.
- Package result: `ok tusker/cmd/tusker 0.733s`.
- An earlier `rtk go test` attempt reported `No tests found`; it is not counted as proof. The passing result above came from `rtk proxy` with the exact command.
