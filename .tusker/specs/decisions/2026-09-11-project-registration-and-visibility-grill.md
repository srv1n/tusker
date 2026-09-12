---
title: "Project registration and visibility decisions"
subject: project-registration-and-visibility-decisions
part_of: project-registration-and-visibility
status: canonical
read_when: "You need the operator rationale behind automatic registration and selectable main-screen projects."
skip_when: "You only need the current contract; read [[project-registration-and-visibility]]."
---

# Project registration and visibility decisions

## Automatic registration

- Question: Should registration happen only through Add Project, or should `tusker init` register its folder?
- Options: explicit Add Project only; init registration; broad filesystem scanning.
- Recommendation: init registration, because init is already an explicit operator action and broad scanning creates surprise inventory.
- Operator: `tusker init` should be automatically discovered by Tusker.
- Locked: init registers by default; no ambient filesystem scan.

## Main-screen membership

- Question: Should every registration appear in primary navigation?
- Options: show everything; select visibility in Settings; remove unwanted registrations.
- Recommendation: separate registration from visibility so hidden projects stay recoverable and retain history.
- Operator: list everything under Settings → All Projects and use check/uncheck controls for the main screen.
- Locked: persisted visibility is independent from registration and automation.

## New-project default

- Question: Should newly initialized projects start checked or hidden?
- Recommendation: checked, because the operator just initialized that folder.
- Operator: agreed to the recommended pattern.
- Locked: new and migrated registrations are visible by default; the operator can uncheck them.

## Failure containment

- Incident: one stale `/private/tmp` registration repeatedly terminated the daemon and caused the desktop shell to reload, destroying navigation state.
- Locked: project-local read failures are quarantined; runtime recovery does not reload committed Web content.
