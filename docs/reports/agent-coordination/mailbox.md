# Mailbox behavior

| Case | Result |
|---|---|
| reopen after question/reply | preserved |
| duplicate enqueue | original returned |
| forged reply recipient | refused |
| unsupported address kind | refused |
| delivery without transport proof | remains pending/unknown |

Verification: `TestAgentMessages` — PASS.
