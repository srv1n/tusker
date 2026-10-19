# Review packet

- Item: RTH-T-0006 - Clear the two docs check findings in committed docs
- Record: RTH-T-0006
- Attempt: 01M595BTYRHKEJQPZZVMMGAME3
- Runner: codex_exec
- Runner profile: codex_exec-gpt-6-luna
- Harness: codex_exec
- Model: gpt-6-luna
- Effort: xhigh
- Lane: execute
- Work revision: 0
- Turns: 0
- Usage telemetry: raw diagnostic data only; it is neither billable nor an exact aggregate.
- Workspace: /Users/sarav/Library/Application Support/tusker/workspaces/tusker/RTH-T-0006__task-rth-t-0006
- Session: 01a15255-f619-7e33-950e-69f966b53c93
- Started: 2026-10-19T04:05:27Z
- Last event: 2026-10-19T04:12:29Z

## Runtime summary

- lease=running outcome=none exit_code=0 completed_at=2026-10-19T04:12:29Z runtime=7m2s

## Soft dependency blast radius

- No soft-edge dependents were found for this task.

## Runtime artifacts

- prompt: `/Users/sarav/Library/Application Support/tusker/runs/tusker/RTH-T-0006/rev-00-execute-attempt-0002-01m595btyrhkejqpzzvmmgame3.prompt.md`
- events: `/Users/sarav/Library/Application Support/tusker/runs/tusker/RTH-T-0006/rev-00-execute-attempt-0002-01m595btyrhkejqpzzvmmgame3.events.jsonl`
- raw log pointer: `/Users/sarav/Library/Application Support/tusker/runs/tusker/RTH-T-0006/rev-00-execute-attempt-0002-01m595btyrhkejqpzzvmmgame3.raw.log`
- status: `/Users/sarav/Library/Application Support/tusker/runs/tusker/RTH-T-0006/rev-00-execute-attempt-0002-01m595btyrhkejqpzzvmmgame3.status.json`

## Turns

- No normalized turns were recorded for this attempt.

## Sessions and turns

- Session refs: `01a15255-f619-7e33-950e-69f966b53c93`
- Turn ids: none observed.

## Supervisor decisions

- `continue_attempt` reason=none parent_attempt=none parent_session=none branch=none workspace=/Users/sarav/Library/Application Support/tusker/workspaces/tusker/RTH-T-0006__task-rth-t-0006 signal=none tokens=0 at=2026-10-19T04:05:27Z

## Changed files

- `.tusker/attempts/RTH-T-0006/RTH-T-0006-A-0001.md` (??)
- `.tusker/attempts/RTH-T-0006/RTH-T-0006-A-0002.md` (??)
- `.tusker/work/tasks/RTH-T-0006.md` (??)
- `.tusker/workspace.json` (M)

### Diff summary

- `.tusker/workspace.json | 50 ++++++++------------------------------------------`
- `1 file changed, 8 insertions(+), 42 deletions(-)`

## Commands and tests

- `/bin/zsh -lc "cat /Users/sarav/.codex/RTK.md && cat '/Users/sarav/Library/Application Support/tusker/workspaces/tusker/RTH-T-0006__task-rth-t-0006/.agents/skills/tusker/SKILL.md' && rtk rg --files knowledge .tusker docs/system | rtk head -100 && rtk status --s...` kind=command_completed result=exit status 127 exit_code=127
- `/bin/zsh -lc "rtk git diff -- docs/system/INDEX.md docs/system/graph.json && rtk proxy sh -c '"'! tusker check --only docs --json | grep -q coordinator-batch'"'; rtk git status --short"` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc "rtk git show --stat --oneline HEAD && rtk git show --format=fuller HEAD -- docs/system/coordinator-batches.md docs/system/proposals/coordinator-batch-mode.md docs/system/INDEX.md docs/system/graph.json && rtk ls -ld .tusker && rtk readlink .tuske...` kind=command_completed result=exit status 1 exit_code=1
- `/bin/zsh -lc "rtk proxy env | rtk cut -d= -f1 | rtk rg '"'^TUSKER|''^PWD$|''^GIT_'"' && tusker --help && rtk rg -n 'repo_root|TUSKER_REPO_ROOT|workspace\\.json' cmd internal | rtk head -100"` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc "rtk proxy env | rtk rg '"'^TUSKER_(REPO_ROOT|VAULT|WORKING_DIR|WORKSPACE|CANONICAL_VAULT|WORKSPACE_VAULT)='"' && rtk sed -n '1525,1570p' cmd/tusker/commands_v7.go && rtk rg -n 'TUSKER_REPO_ROOT|TUSKER_CANONICAL_VAULT|TUSKER_WORKING_DIR' cmd/tuske...` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc "rtk pwd && rtk git rev-parse --show-toplevel && rtk git status --short --untracked-files=all docs/system/coordinator-batches.md docs/system/proposals/coordinator-batch-mode.md docs/system/INDEX.md docs/system/graph.json && rtk git diff HEAD -- do...` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc "rtk rg -n 'docs check|docsCheck|runDocs|checkDocs|run.*docs' cmd/tusker | rtk head -80 && rtk rg -n 'func v7RepoRoot|func.*RepoRoot|TUSKER_REPO_ROOT' cmd/tusker | rtk head -80 && rtk tusker docs check --help"` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc "rtk rg -n -C 2 'audit\\.d|audit directory|proposal|coordinator-batch-mode' docs/system/coordinator-batches.md docs/system/proposals/coordinator-batch-mode.md docs/system/proposals/00-index.md docs/system/INDEX.md docs/system/graph.json && rtk git...` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc "rtk rg -n -C 3 'audit\\.d|coordinator-batch|"'^---|''^subject:|''^title:|''^part_of:|''^status:|''^read_when:|''^skip_when:'"' docs/system/coordinator-batches.md docs/system/proposals/coordinator-batch-mode.md docs/system/proposals/00-index.md do...` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc "rtk sed -n '1,130p' cmd/tusker/docs_check_cmd.go && rtk sed -n '100,165p' cmd/tusker/check_cmd.go && rtk rg -n 'func docsCmd|docsRoot|docs.*RepoRoot|repoRoot.*Vault|v7RepoRoot' cmd/tusker/docs*.go cmd/tusker/*.go | rtk head -100 && rtk rg -n 'aud...` kind=command_completed result=exit status 1 exit_code=1
- `/bin/zsh -lc "rtk sed -n '120,210p' cmd/tusker/helpers.go && rtk sed -n '300,410p' cmd/tusker/helpers.go && rtk sed -n '1,45p' cmd/tusker/check_cmd.go"` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc "rtk sed -n '35,120p' cmd/tusker/check_cmd.go && rtk rg -n 'func docsMapCmd' cmd/tusker/docs_cmd.go && rtk sed -n '335,445p' cmd/tusker/docs_cmd.go && rtk tusker check --help"` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc "rtk sed -n '4710,4745p' cmd/tusker/commands_v7.go && rtk sed -n '1,80p' cmd/tusker/docs_cmd.go && rtk rg -n 'func resolveVaultPath' cmd/tusker"` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc "rtk tusker done RTH-T-0006 --by 01M595BTYRHKEJQPZZVMMGAME3 --deliverable 'Result: corrected the two coordinator docs and refreshed generated docs maps; Handoff: proposal is indexed in the system graph; Limitations: task-worktree docs check passes...` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc "rtk which tusker && rtk git rev-parse HEAD && rtk git -C '/Users/sarav/Downloads/side/tusker' rev-parse HEAD && rtk git -C '/Users/sarav/Downloads/side/tusker' status --short -- docs/system/coordinator-batches.md docs/system/proposals/coordinator...` kind=command_completed result=exit status 1 exit_code=1
- `/bin/zsh -lc 'cat .tusker/SKILL.md && cat knowledge/domains/project/INDEX.md && cat knowledge/domains/project/CANON.md && cat .tusker/specs/process-containment.md && cat /Users/sarav/.codex/skills/tusker/SKILL.md'` kind=command_completed result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk cat .tusker/specs/process-containment.md docs/system/coordinator-batches.md docs/system/proposals/coordinator-batch-mode.md docs/system/INDEX.md docs/system/graph.json && rtk git status --short && tusker check --only docs --json'` kind=command_completed result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk git add docs/system/00-overview.md docs/system/INDEX.md docs/system/graph.json && rtk git commit -m "docs: refresh generated system doc maps" && rtk git status --short'` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc 'rtk git diff -- docs/system/00-overview.md && rtk git status --short -- docs/system/00-overview.md'` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc 'rtk git log -2 --oneline'` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc 'rtk proxy env TUSKER_REPO_ROOT="$PWD" TUSKER_CANONICAL_VAULT="$PWD/.tusker" TUSKER_VAULT="$PWD/.tusker" tusker check --only docs --json'` kind=command_completed result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk proxy env TUSKER_REPO_ROOT="$PWD" tusker check --only docs --json && rtk proxy env TUSKER_REPO_ROOT="$PWD" sh -c '"'"'! tusker check --only docs --json | grep -q coordinator-batch'"'"` kind=command_completed result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk proxy env TUSKER_REPO_ROOT="$PWD" tusker check --only docs --use-project-vault --json'` kind=command_completed result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk tusker check --only docs --fix --vault "$PWD/.tusker" --json && rtk git status --short -- docs/system/INDEX.md docs/system/graph.json && rtk tusker check --only docs --vault "$PWD/.tusker" --json'` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc 'rtk tusker check --only docs --vault "$PWD/.tusker" --json && rtk tusker done --help'` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc 'rtk tusker check --only docs --vault "$PWD/.tusker" --json'` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc 'rtk tusker docs map --help'` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc 'rtk tusker docs map --vault "$PWD/.tusker" && rtk git status --short -- docs/system/INDEX.md docs/system/graph.json && rtk tusker check --only docs --vault "$PWD/.tusker" --json'` kind=command_completed result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk tusker help docs map && rtk tusker docs map --help'` kind=command_completed result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk tusker help work wait'` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc 'rtk tusker show RTH-T-0006 --json && rtk tusker show RTH-T-0006 --proof --json'` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc 'rtk tusker show RTH-T-0006 --progress --json && rtk tusker show RTH-T-0006 --proof --json'` kind=command_completed result=pass exit_code=0
- `/bin/zsh -lc 'rtk tusker work wait RTH-T-0006 --timeout 30 --json'` kind=command_completed result=exit status 1 exit_code=1
- `codex exec --json --skip-git-repo-check - --model gpt-6-luna -c 'model_reasoning_effort="xhigh"'` kind=attempt_started result=started

## Verification

- `/bin/zsh -lc "cat /Users/sarav/.codex/RTK.md && cat '/Users/sarav/Library/Application Support/tusker/workspaces/tusker/RTH-T-0006__task-rth-t-0006/.agents/skills/tusker/SKILL.md' && rtk rg --files knowledge .tusker docs/system | rtk head -100 && rtk status --s...` result=exit status 127 exit_code=127 at=2026-10-19T04:06:22Z
- `/bin/zsh -lc "rtk git diff -- docs/system/INDEX.md docs/system/graph.json && rtk proxy sh -c '"'! tusker check --only docs --json | grep -q coordinator-batch'"'; rtk git status --short"` result=pass exit_code=0 at=2026-10-19T04:10:19Z
- `/bin/zsh -lc "rtk git show --stat --oneline HEAD && rtk git show --format=fuller HEAD -- docs/system/coordinator-batches.md docs/system/proposals/coordinator-batch-mode.md docs/system/INDEX.md docs/system/graph.json && rtk ls -ld .tusker && rtk readlink .tuske...` result=exit status 1 exit_code=1 at=2026-10-19T04:07:34Z
- `/bin/zsh -lc "rtk proxy env | rtk cut -d= -f1 | rtk rg '"'^TUSKER|''^PWD$|''^GIT_'"' && tusker --help && rtk rg -n 'repo_root|TUSKER_REPO_ROOT|workspace\\.json' cmd internal | rtk head -100"` result=pass exit_code=0 at=2026-10-19T04:07:50Z
- `/bin/zsh -lc "rtk proxy env | rtk rg '"'^TUSKER_(REPO_ROOT|VAULT|WORKING_DIR|WORKSPACE|CANONICAL_VAULT|WORKSPACE_VAULT)='"' && rtk sed -n '1525,1570p' cmd/tusker/commands_v7.go && rtk rg -n 'TUSKER_REPO_ROOT|TUSKER_CANONICAL_VAULT|TUSKER_WORKING_DIR' cmd/tuske...` result=pass exit_code=0 at=2026-10-19T04:07:50Z
- `/bin/zsh -lc "rtk pwd && rtk git rev-parse --show-toplevel && rtk git status --short --untracked-files=all docs/system/coordinator-batches.md docs/system/proposals/coordinator-batch-mode.md docs/system/INDEX.md docs/system/graph.json && rtk git diff HEAD -- do...` result=pass exit_code=0 at=2026-10-19T04:06:55Z
- `/bin/zsh -lc "rtk rg -n 'docs check|docsCheck|runDocs|checkDocs|run.*docs' cmd/tusker | rtk head -80 && rtk rg -n 'func v7RepoRoot|func.*RepoRoot|TUSKER_REPO_ROOT' cmd/tusker | rtk head -80 && rtk tusker docs check --help"` result=pass exit_code=0 at=2026-10-19T04:08:07Z
- `/bin/zsh -lc "rtk rg -n -C 2 'audit\\.d|audit directory|proposal|coordinator-batch-mode' docs/system/coordinator-batches.md docs/system/proposals/coordinator-batch-mode.md docs/system/proposals/00-index.md docs/system/INDEX.md docs/system/graph.json && rtk git...` result=pass exit_code=0 at=2026-10-19T04:07:17Z
- `/bin/zsh -lc "rtk rg -n -C 3 'audit\\.d|coordinator-batch|"'^---|''^subject:|''^title:|''^part_of:|''^status:|''^read_when:|''^skip_when:'"' docs/system/coordinator-batches.md docs/system/proposals/coordinator-batch-mode.md docs/system/proposals/00-index.md do...` result=pass exit_code=0 at=2026-10-19T04:06:44Z
- `/bin/zsh -lc "rtk sed -n '1,130p' cmd/tusker/docs_check_cmd.go && rtk sed -n '100,165p' cmd/tusker/check_cmd.go && rtk rg -n 'func docsCmd|docsRoot|docs.*RepoRoot|repoRoot.*Vault|v7RepoRoot' cmd/tusker/docs*.go cmd/tusker/*.go | rtk head -100 && rtk rg -n 'aud...` result=exit status 1 exit_code=1 at=2026-10-19T04:08:35Z
- `/bin/zsh -lc "rtk sed -n '120,210p' cmd/tusker/helpers.go && rtk sed -n '300,410p' cmd/tusker/helpers.go && rtk sed -n '1,45p' cmd/tusker/check_cmd.go"` result=pass exit_code=0 at=2026-10-19T04:08:41Z
- `/bin/zsh -lc "rtk sed -n '35,120p' cmd/tusker/check_cmd.go && rtk rg -n 'func docsMapCmd' cmd/tusker/docs_cmd.go && rtk sed -n '335,445p' cmd/tusker/docs_cmd.go && rtk tusker check --help"` result=pass exit_code=0 at=2026-10-19T04:10:02Z
- `/bin/zsh -lc "rtk sed -n '4710,4745p' cmd/tusker/commands_v7.go && rtk sed -n '1,80p' cmd/tusker/docs_cmd.go && rtk rg -n 'func resolveVaultPath' cmd/tusker"` result=pass exit_code=0 at=2026-10-19T04:08:41Z
- `/bin/zsh -lc "rtk tusker done RTH-T-0006 --by 01M595BTYRHKEJQPZZVMMGAME3 --deliverable 'Result: corrected the two coordinator docs and refreshed generated docs maps; Handoff: proposal is indexed in the system graph; Limitations: task-worktree docs check passes...` result=pass exit_code=0 at=2026-10-19T04:11:05Z
- `/bin/zsh -lc "rtk which tusker && rtk git rev-parse HEAD && rtk git -C '/Users/sarav/Downloads/side/tusker' rev-parse HEAD && rtk git -C '/Users/sarav/Downloads/side/tusker' status --short -- docs/system/coordinator-batches.md docs/system/proposals/coordinator...` result=exit status 1 exit_code=1 at=2026-10-19T04:07:06Z
- `/bin/zsh -lc 'cat .tusker/SKILL.md && cat knowledge/domains/project/INDEX.md && cat knowledge/domains/project/CANON.md && cat .tusker/specs/process-containment.md && cat /Users/sarav/.codex/skills/tusker/SKILL.md'` result=exit status 1 exit_code=1 at=2026-10-19T04:06:22Z
- `/bin/zsh -lc 'rtk cat .tusker/specs/process-containment.md docs/system/coordinator-batches.md docs/system/proposals/coordinator-batch-mode.md docs/system/INDEX.md docs/system/graph.json && rtk git status --short && tusker check --only docs --json'` result=exit status 1 exit_code=1 at=2026-10-19T04:06:27Z
- `/bin/zsh -lc 'rtk git add docs/system/00-overview.md docs/system/INDEX.md docs/system/graph.json && rtk git commit -m "docs: refresh generated system doc maps" && rtk git status --short'` result=pass exit_code=0 at=2026-10-19T04:10:44Z
- `/bin/zsh -lc 'rtk git diff -- docs/system/00-overview.md && rtk git status --short -- docs/system/00-overview.md'` result=pass exit_code=0 at=2026-10-19T04:10:24Z
- `/bin/zsh -lc 'rtk git log -2 --oneline'` result=pass exit_code=0 at=2026-10-19T04:12:19Z
- `/bin/zsh -lc 'rtk proxy env TUSKER_REPO_ROOT="$PWD" TUSKER_CANONICAL_VAULT="$PWD/.tusker" TUSKER_VAULT="$PWD/.tusker" tusker check --only docs --json'` result=exit status 1 exit_code=1 at=2026-10-19T04:08:12Z
- `/bin/zsh -lc 'rtk proxy env TUSKER_REPO_ROOT="$PWD" tusker check --only docs --json && rtk proxy env TUSKER_REPO_ROOT="$PWD" sh -c '"'"'! tusker check --only docs --json | grep -q coordinator-batch'"'"` result=exit status 1 exit_code=1 at=2026-10-19T04:08:01Z
- `/bin/zsh -lc 'rtk proxy env TUSKER_REPO_ROOT="$PWD" tusker check --only docs --use-project-vault --json'` result=exit status 1 exit_code=1 at=2026-10-19T04:08:18Z
- `/bin/zsh -lc 'rtk tusker check --only docs --fix --vault "$PWD/.tusker" --json && rtk git status --short -- docs/system/INDEX.md docs/system/graph.json && rtk tusker check --only docs --vault "$PWD/.tusker" --json'` result=pass exit_code=0 at=2026-10-19T04:10:07Z
- `/bin/zsh -lc 'rtk tusker check --only docs --vault "$PWD/.tusker" --json && rtk tusker done --help'` result=pass exit_code=0 at=2026-10-19T04:10:44Z
- `/bin/zsh -lc 'rtk tusker check --only docs --vault "$PWD/.tusker" --json'` result=pass exit_code=0 at=2026-10-19T04:08:58Z
- `/bin/zsh -lc 'rtk tusker docs map --help'` result=pass exit_code=0 at=2026-10-19T04:09:32Z
- `/bin/zsh -lc 'rtk tusker docs map --vault "$PWD/.tusker" && rtk git status --short -- docs/system/INDEX.md docs/system/graph.json && rtk tusker check --only docs --vault "$PWD/.tusker" --json'` result=exit status 1 exit_code=1 at=2026-10-19T04:10:00Z
- `/bin/zsh -lc 'rtk tusker help docs map && rtk tusker docs map --help'` result=exit status 1 exit_code=1 at=2026-10-19T04:09:26Z
- `/bin/zsh -lc 'rtk tusker help work wait'` result=pass exit_code=0 at=2026-10-19T04:11:23Z
- `/bin/zsh -lc 'rtk tusker show RTH-T-0006 --json && rtk tusker show RTH-T-0006 --proof --json'` result=pass exit_code=0 at=2026-10-19T04:11:17Z
- `/bin/zsh -lc 'rtk tusker show RTH-T-0006 --progress --json && rtk tusker show RTH-T-0006 --proof --json'` result=pass exit_code=0 at=2026-10-19T04:12:02Z
- `/bin/zsh -lc 'rtk tusker work wait RTH-T-0006 --timeout 30 --json'` result=exit status 1 exit_code=1 at=2026-10-19T04:11:56Z

## Validation

- `/bin/zsh -lc "rtk git diff -- docs/system/INDEX.md docs/system/graph.json && rtk proxy sh -c '"'! tusker check --only docs --json | grep -q coordinator-batch'"'; rtk git status --short"` result=pass exit_code=0
- `/bin/zsh -lc "rtk sed -n '35,120p' cmd/tusker/check_cmd.go && rtk rg -n 'func docsMapCmd' cmd/tusker/docs_cmd.go && rtk sed -n '335,445p' cmd/tusker/docs_cmd.go && rtk tusker check --help"` result=pass exit_code=0
- `/bin/zsh -lc "rtk tusker done RTH-T-0006 --by 01M595BTYRHKEJQPZZVMMGAME3 --deliverable 'Result: corrected the two coordinator docs and refreshed generated docs maps; Handoff: proposal is indexed in the system graph; Limitations: task-worktree docs check passes...` result=pass exit_code=0
- `/bin/zsh -lc "rtk which tusker && rtk git rev-parse HEAD && rtk git -C '/Users/sarav/Downloads/side/tusker' rev-parse HEAD && rtk git -C '/Users/sarav/Downloads/side/tusker' status --short -- docs/system/coordinator-batches.md docs/system/proposals/coordinator...` result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk cat .tusker/specs/process-containment.md docs/system/coordinator-batches.md docs/system/proposals/coordinator-batch-mode.md docs/system/INDEX.md docs/system/graph.json && rtk git status --short && tusker check --only docs --json'` result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk proxy env TUSKER_REPO_ROOT="$PWD" TUSKER_CANONICAL_VAULT="$PWD/.tusker" TUSKER_VAULT="$PWD/.tusker" tusker check --only docs --json'` result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk proxy env TUSKER_REPO_ROOT="$PWD" tusker check --only docs --json && rtk proxy env TUSKER_REPO_ROOT="$PWD" sh -c '"'"'! tusker check --only docs --json | grep -q coordinator-batch'"'"` result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk proxy env TUSKER_REPO_ROOT="$PWD" tusker check --only docs --use-project-vault --json'` result=exit status 1 exit_code=1
- `/bin/zsh -lc 'rtk tusker check --only docs --fix --vault "$PWD/.tusker" --json && rtk git status --short -- docs/system/INDEX.md docs/system/graph.json && rtk tusker check --only docs --vault "$PWD/.tusker" --json'` result=pass exit_code=0
- `/bin/zsh -lc 'rtk tusker check --only docs --vault "$PWD/.tusker" --json && rtk tusker done --help'` result=pass exit_code=0
- `/bin/zsh -lc 'rtk tusker check --only docs --vault "$PWD/.tusker" --json'` result=pass exit_code=0
- `/bin/zsh -lc 'rtk tusker docs map --vault "$PWD/.tusker" && rtk git status --short -- docs/system/INDEX.md docs/system/graph.json && rtk tusker check --only docs --vault "$PWD/.tusker" --json'` result=exit status 1 exit_code=1

## Open risks

- No open risks were observed in normalized events or runtime status.
- Reviewer must still check claims against the current tree before approval.
- This packet summarizes daemon-observed runtime facts. It does not embed raw logs or full transcripts.
