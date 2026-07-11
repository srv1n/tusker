# Feedback intake, review, promote, fanout

Use this workflow when feedback notes from one or more repositories should become structured Tusker work:

1. Intake explicit notes with `tusker feedback ingest --since <date> --repo <repo[,repo...]> --output-vault <vault> --write`.
2. Review all signals with `tusker feedback review --since <date> --vault <output-vault> --json`.
3. Promote one finding with `tusker feedback promote <finding-id> --vault <output-vault> --write`.
4. Fanout only when the parent task or workflow explicitly enables fanout; promotion itself creates or links work, it does not dispatch agents.

Feedback notes are explicit human or agent observations under `feedback/agents/`. Task and event signals are mechanical reducer facts from Tusker state. Review combines both, but citations must preserve the source note, source project key, repo root, vault root, import run ID, and dedupe key so promotion can link the resulting task back to the original feedback.
