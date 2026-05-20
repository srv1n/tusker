# Quality gates

Use these checks before publishing or returning a final documentation artifact.

## Universal checks

```text
[ ] The primary reader is named.
[ ] The primary user need is named.
[ ] The document has one primary Diátaxis mode.
[ ] Adjacent modes are linked instead of stuffed into the document.
[ ] Source-of-truth is known or the assumption is explicit.
[ ] The title matches the user's need, not an internal feature label.
[ ] The content is accurate enough for the risk level.
[ ] The document states or implies when it becomes stale.
[ ] The page has a useful next step or related link.
```

## Tutorial checks

```text
[ ] The learner can start safely.
[ ] The path is controlled and does not branch unnecessarily.
[ ] The learner sees meaningful results early.
[ ] Expected output is shown where anxiety would otherwise appear.
[ ] Explanation is minimal and linked out.
[ ] Choices and alternatives are removed unless essential.
[ ] The tutorial has been tested end-to-end or marked untested.
```

## How-to guide checks

```text
[ ] The guide solves a real user goal.
[ ] It assumes a competent user, not a beginner.
[ ] Steps are ordered for the user's work, not the system's internals.
[ ] Branches cover likely real-world variants.
[ ] The guide includes verification of the result.
[ ] Background theory is linked out.
[ ] Reference detail is linked out.
```

## Reference checks

```text
[ ] The scope is explicit.
[ ] Structure mirrors the machinery being described.
[ ] Names, defaults, limits, and errors are precise.
[ ] Tables and examples are consistent.
[ ] Warnings are included for dangerous or irreversible use.
[ ] It does not drift into teaching or persuasion.
[ ] It can be updated from a clear source of truth.
```

## Explanation checks

```text
[ ] The topic is bounded.
[ ] It answers a real why/how-does-this-fit question.
[ ] It makes useful connections.
[ ] It includes trade-offs or alternatives where relevant.
[ ] It does not become a procedure.
[ ] It does not become a reference table dump.
[ ] It leaves the reader with better judgment.
```

## Agent-facing checks

```text
[ ] Agent reader and scope are explicit.
[ ] Inputs and outputs are defined.
[ ] Preconditions are defined.
[ ] Allowed tools, commands, or APIs are named.
[ ] Defaults are explicit.
[ ] Validation steps are observable.
[ ] Failure modes are listed.
[ ] Escalation/confirmation rules are listed.
[ ] Source-of-truth and stale triggers are listed.
[ ] Human maintainers can understand why the contract exists.
```

## Functional quality

Functional quality is the minimum bar:

- accurate
- complete enough for scope
- consistent
- precise
- useful
- current
- findable

If functional quality fails, no amount of elegant structure saves the page.

## Deep quality

Deep quality is what makes documentation feel like it fits the reader:

- flow
- anticipation
- humane pacing
- good boundaries
- right amount of context
- clean relationships between pages

You cannot fully measure this. You judge it by using the page or watching someone use it.

## Red flags

| Symptom | Likely problem | Fix |
|---|---|---|
| “This tutorial has options for every environment.” | Tutorial/how-to mix | Pick one safe tutorial path; move variants to how-to guides. |
| “This how-to begins with basic concepts.” | Teaching creep | Link to tutorial or explanation. |
| “This reference page has a long rationale section.” | Explanation creep | Move rationale to explanation. |
| “This explanation has step-by-step commands.” | Procedure creep | Move commands to how-to guide. |
| “The agent keeps making up missing details.” | Reference gap | Add explicit contracts, defaults, and failure modes. |
| “No one knows whether this page is still true.” | Governance gap | Add owner, source-of-truth, and stale triggers. |
