# Agent Feedback

- context: Postmortem of rzn/backend 2026-07-13 CIR wave: resident daemon auto-dispatched Codex workers while user believed automation was disabled via serve UI
- friction: Disable is not trustworthy: daemon gates only on DB projects.enabled; config automation.enabled is display-only; serve UI disable writes config first and skips the DB flip on error; agent redrive/budget_redrive paths bypass the dispatch guardrail class; no master kill-switch; no agent-caller detection; automation plan prints 'dispatch' with no caveat
- product-idea: Fail-closed disable: daemon honors AND of both flags; UI disable flips DB first and verifies readback; add global automation off switch; gate redrives and daemon run on actor identity (refuse agent:* without operator override); rename plan decision output away from bare 'dispatch'
- impact: Runaway worker waves, burned tokens, broken user trust in the disable toggle
- related: daemon.go dispatchRun; serve_actions.go handleProjectAutomationAction; automation_commands.go oneShotDispatchRefusal; runner_profiles.go setProjectLocalConfigWithReadback
