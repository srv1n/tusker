# V7 Package Map

V7 is being moved out of `cmd/tusker` in low-risk slices. During the transition,
`cmd/tusker` may keep compatibility aliases or wrappers so public CLI behavior
and existing tests can move independently from package ownership.

| Package | Owns | Should not own |
|---|---|---|
| `cmd/tusker` | CLI routing, flag adaptation, terminal output, error formatting. | Core V7 schema constants, pure policy decisions, store internals, validation rules, workflow projections. |
| `internal/v7schema` | V7 ID patterns, enum sets, frontmatter ordering, state revision hashing, note-format pure helpers, `tusker.yaml` schema structs. | Filesystem writes, command behavior, user-facing errors. |
| `internal/v7policy` | Close-policy defaults and pure acceptor checks. | Vault scanning, evidence lookup, command-specific error construction. |

Next safe extraction targets:

1. Move markdown object/store CAS code from `cmd/tusker` into an internal store package after current V7 command splits compile cleanly.
2. Move git/project discovery helpers into a discovery package once `cli.go` syntax is stable.
3. Move validation policy tables from `cmd/tusker/v7_validation.go` only after public validator tests cover the package boundary.
