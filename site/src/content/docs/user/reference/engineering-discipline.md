---
title: "Engineering Discipline"
description: "Use this when the work involves non-trivial implementation, bug diagnosis, tests, refactors, architecture seams, performance regressions, or a request for TDD. It is a compact synthesis of the external engineering skills reviewed for SKL-T-0002; do not vendor those skills wholesale into Tusker."
tusker:
  audience: "user"
  publish_path: "user/reference/engineering-discipline"
  publish_section_title: "Reference"
  route: "/user/reference/engineering-discipline/"
  source_kind: "repo_doc"
  source_path: "skill/references/ENGINEERING_DISCIPLINE.md"
  summary: "Use this when the work involves non-trivial implementation, bug diagnosis, tests, refactors, architecture seams, performance regressions, or a request for TDD. It is a compact synthesis of the external engineering skills reviewed for SKL-T-0002; do not vendor those skills wholesale into Tusker."
  tags:
    - "reference"
  updated: "2026-05-11"
---

# Engineering Discipline

Use this when the work involves non-trivial implementation, bug diagnosis,
tests, refactors, architecture seams, performance regressions, or a request for
TDD. It is a compact synthesis of the external engineering skills reviewed for
SKL-T-0002; do not vendor those skills wholesale into Tusker.

## Imported Ideas Worth Keeping

| Idea | Tusker form | Why it earns space |
|---|---|---|
| Behavior-first tests | Test through the public interface and assert observable outcomes. | Survives refactors and proves user/caller value. |
| Vertical TDD | One behavior -> one failing check -> minimal implementation -> repeat. | Avoids bulk tests for imagined behavior. |
| Mock discipline | Mock system boundaries; use real owned code or local stand-ins where practical. | Reduces brittle tests and false confidence. |
| Feedback-loop diagnosis | Reproduce first, sharpen the loop, then test hypotheses. | Debugging without a loop is mostly vibes. |
| Falsifiable hypotheses | Rank likely causes and state what would prove or disprove each. | Prevents anchoring on the first plausible story. |
| Surgical edits | Every changed line should trace to the task. | Keeps diffs reviewable and protects user work. |
| Deep modules | Prefer small interfaces that hide meaningful behavior. | Improves testability, locality, and agent navigation. |
| Throwaway prototypes | Prototype only to answer a named question, then delete or absorb. | Keeps exploration from rotting into production. |

Skip the parts that do not fit Tusker: per-repo issue tracker setup,
copy-pasted global Claude rules, and elaborate multi-agent interface-design
ceremony unless the user explicitly asks for design alternatives.

## Operating Posture

- State assumptions when they affect design, data, privacy, or user-visible
  behavior.
- If the request has multiple plausible meanings, name them and choose only
  when the safe path is obvious. Ask when a wrong guess would be expensive.
- Push back on unnecessary abstractions, broad rewrites, or vague goals.
- Prefer the smallest complete change that satisfies the acceptance contract.
- Do not improve adjacent code, comments, names, or formatting unless the task
  requires it.
- Remove only the dead code or unused imports created by your own change.

## Behavior-First Tests

A good test describes what the system does for a caller, not how internals
coordinate to do it.

Prefer tests that:

- exercise the public interface or task-level workflow;
- use real owned code paths;
- assert observable results, persisted state through the domain interface, or
  emitted protocol/output;
- use names that read like capabilities;
- survive private refactors.

Red flags:

- mocking owned modules or internal collaborators;
- testing private methods;
- asserting internal call counts or call order;
- directly querying storage to prove behavior when a domain read interface
  exists;
- tests that fail after a private refactor with no behavior change.

Mock only system boundaries:

- true third-party APIs;
- remote services outside the repo's control;
- time, randomness, network, and filesystem seams when deterministic tests need
  control;
- local stand-ins for owned infrastructure when available, such as an in-memory
  adapter or test database.

## Vertical Implementation Loop

Do not treat RED as "write every test" and GREEN as "write every line of
implementation." That creates tests for guesses.

Use this loop instead:

1. Pick one tracer behavior that crosses the important seam.
2. Write one failing check for that behavior, or the closest equivalent smoke
   command when tests are not available.
3. Implement only enough to pass that check.
4. Repeat for the next behavior, using what the previous slice taught you.
5. Refactor only when the checks are green.

Good slices are narrow but complete: they pass through every layer needed to
make one behavior real. Weak slices are horizontal: "backend first," "UI later,"
or "all tests now, implementation later" when none of the pieces is independently
verifiable.

## Debugging Discipline

Before diagnosing a bug, build the feedback loop.

A useful loop is:

- fast enough to run repeatedly;
- deterministic, or at least has a high reproduction rate for flakes;
- specific to the user-described symptom;
- runnable by the agent when possible.

Good loop shapes include failing tests, CLI fixtures, HTTP scripts, browser
scripts, trace replays, small harnesses, fuzz/property loops, and bisect scripts.

After the loop exists:

1. Reproduce the exact failure the user reported.
2. Write 3-5 ranked hypotheses.
3. For each hypothesis, state the prediction that would falsify it.
4. Probe one variable at a time.
5. Tag temporary logs with a unique prefix like `[DEBUG-abc123]`.
6. Turn the minimized repro into a regression test at the correct seam.
7. Remove temporary instrumentation before declaring done.

If no correct test seam exists, record that as architecture debt. A shallow test
that cannot reproduce the real bug pattern is not a regression test.

## Architecture Language

Use consistent terms when discussing refactors:

- **Module**: something with an interface and implementation.
- **Interface**: everything a caller must know: types, invariants, ordering,
  error modes, configuration, and performance expectations.
- **Implementation**: the code behind the interface.
- **Seam**: where behavior can vary without editing the caller.
- **Adapter**: concrete code that satisfies an interface at a seam.
- **Depth**: leverage at the interface. A deep module hides meaningful behavior
  behind a small surface.
- **Locality**: change and bugs concentrate in one place.

Useful tests:

- Deletion test: if deleting the module removes complexity, it was likely a
  pass-through. If the complexity reappears across callers, the module earned
  its keep.
- Interface test: if tests need to reach past the interface, the module shape is
  probably wrong.
- Adapter test: one adapter is a hypothetical seam; two adapters usually means
  the seam is real.

## Prototypes

Prototype when the question cannot be answered cleanly by inspection.

Rules:

- Name the question the prototype answers.
- Keep it clearly throwaway and close to the code it informs.
- Provide one command to run it.
- Avoid persistence unless persistence is the question.
- Expose the relevant state after each action or variant switch.
- When done, delete it or absorb the decision into production code.
- Capture the decision in the task evidence, an ADR, or a nearby note before the
  prototype disappears.

## Done Criteria

Before moving implementation work to review:

- The acceptance contract is satisfied through behavior-level checks.
- Focused checks pass, and broader validation ran when shared behavior changed.
- Temporary instrumentation and throwaway code are gone or clearly marked.
- The diff contains no speculative features or unrelated cleanup.
- Docs impact is applied, verified no-op, or waived with a real reason.
- Evidence names what was tested and what remains risky.

## Source Notes

This guidance was synthesized from:

- Matt Pocock's engineering skills, especially TDD, diagnose, architecture, and
  vertical issue-slicing guidance:
  https://github.com/mattpocock/skills/tree/main/skills/engineering
- Matt Pocock's good/bad test examples:
  https://github.com/mattpocock/skills/blob/main/skills/engineering/tdd/tests.md
- Forrest Chang's Karpathy-inspired coding guidelines:
  https://github.com/forrestchang/andrej-karpathy-skills

Keep this file as Tusker-native guidance. Link sources for attribution; do not
copy their full text into the skill payload.
