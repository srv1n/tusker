---
title: "External skill routing checks"
subject: skills-docs-methods
status: current
read_when: "Reviewing the external design and writing route."
skip_when: "You only need task execution or runtime behavior."
---

# External skill routing checks

Tusker keeps task capture and conversion. It uses an external design method
only when intent is unresolved, and an external writing method only when that
optional dependency is available. This report records the source and caller
inspection for FLW-T-0031.

## Sources and host state

| Method | Source and version | Host state | Return to Tusker |
| --- | --- | --- | --- |
| Design | Installed `grilling` skill | Present | Settled decisions or open questions. |
| Writing | [pstack technical-writing](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/technical-writing/SKILL.md) and [unslop](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/unslop/SKILL.md) at `7366ac128bdf95f45e6734f412b49a4031800169` | Missing from the host catalog | Clear prose that preserves local task constraints. |

The pstack repository is MIT-licensed. Codex's installed `skill-installer`
supports explicit GitHub-path installation, but this task did not install it.
The installed `grilling` instructions were inspected; Tusker references it but
does not copy its interview protocol.

Remaining installation drift: pstack `technical-writing` and `unslop` are not
installed, and the repository's existing `.agents/skills/spec` and
`.claude/skills/spec` copies remain user-owned and unrefreshed.

## Routing examples

| Situation | Tusker route | Preserved meaning |
| --- | --- | --- |
| Product choice is unsettled. | Use the available design method, then link its decisions or open questions in the canonical spec. | Tusker still owns task conversion and gates. |
| Optional writing method is unavailable. | Report the missing dependency and use the local prose rule. | No installation, permission change, or false claim of use. |
| A supplied spec already settles the work. | Preserve spec and decision links, then create task contracts. | No new interview occurs. |

## Source and caller findings

`skills/spec/SKILL.md` duplicated a Tusker-owned interview and
`cmd/tusker/init_docs.go` materialized it in both fresh-project skill roots.
The scaffold now installs only the Tusker operating package. Existing
user-created `.agents/skills/spec` and `.claude/skills/spec` copies are left
untouched; a managed-copy refresh remains an operator action through
`tusker skill sync --repo . --source <canonical-tusker-checkout>`.

## Verification

Executed on 2026-09-11:

```sh
go test ./cmd/tusker -run 'TestExternalDesignSkillRouting|TestScaffoldDocumentationSystem|TestScaffoldSkills|TestAgentSkillPackage' -count=1 -v
```

PASS: 8 tests in `./cmd/tusker`. It covers the present external route, the
missing optional route, the settled-spec route, fresh scaffolding, and
existing-copy preservation. Independent review still checks that these examples
retain their source links and local meaning.

`go run ./cmd/tusker skill doctor --strict --json` also ran. It failed on
39 existing task-proof/spec errors and 63 warnings outside this ticket's owned
paths; it was not recorded as a package pass.
