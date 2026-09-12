# Transport capabilities

Transport controls are recorded per exact endpoint and version. Attach, idle resume, active delivery, turn observation, reply capture, and uncertain-delivery reconciliation are independent booleans. Resume support does not imply active injection. No relay model exists.

Offline protocol checks prove that resume does not imply active delivery and that Codex steering carries the exact thread ID, expected turn ID, and durable message ID. Installed live conformance passed for both `codex_exec` and `muse`; Muse remains CLI-only. Codex active-turn steering remains protocol-qualified rather than live-certified.
