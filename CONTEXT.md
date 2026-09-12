# Tusker

Tusker organizes a project's intended work, its execution and the evidence used to accept its results.

## Language

**Project**:
A persistent body of work with one user-facing identity, shared planning documents and related tasks.
_Avoid_: Using a branch or worktree name as a synonym for a project.

**Project home**:
The designated place for a project's current planning documents and task authoring.
_Avoid_: Treating every execution workspace as another project home.

**Execution workspace**:
The working copy assigned to an implementation attempt, belonging to a project rather than defining another project.
_Avoid_: Calling an execution workspace a separate project merely because its location differs.


**Agent review**:
An independent agent's assessment of implemented work against its agreed contract and evidence.
_Avoid_: Treating a worker's own success claim as independent review.

**Outcome review**:
An assessment of a delivered result, such as its visual quality or measured performance. A person participates when the result requires an explicitly declared human judgment.
_Avoid_: Using outcome review as a synonym for mandatory human code review.

**Architect**:
The agent responsible for interpreting an agreed objective, answering design questions and proposing subsequent work from the results.
_Avoid_: Using architect as a synonym for the scheduler or the owner of every worker process.

**Agent contact**:
A durable reference to the conversation or task owner that another participant should address, including after an execution ends or resumes.
_Avoid_: Using a display name, model name or temporary process as the recipient identity.

**Clarification**:
A question tied to particular work whose answer is needed to proceed or settle an ambiguity. It retains its question, recipient and reply together.
_Avoid_: Treating every clarification as a human gate or a new implementation task.
