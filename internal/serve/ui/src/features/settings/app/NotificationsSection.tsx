/*
  Notifications tab — the two toggles (human gate, stale run) plus a delivery
  method. Nothing else: no digests, no email (addendum §2.6). Every row still
  carries its provenance chip (addendum §1.3).
*/

import { SettingRow, SettingsCard } from "./parts";
import { notifyRows } from "./mock";

export function NotificationsSection() {
  return (
    <div className="animate-rise">
      <SettingsCard>
        {notifyRows.map((r) => (
          <SettingRow
            key={r.key}
            label={`Notify on ${r.key}`}
            source={r.source}
            locked
            description="Persistence is not available yet."
            control={
              <span className="font-mono text-[11.5px] text-muted">{r.on ? "Enabled" : "Disabled"} · coming soon</span>
            }
          />
        ))}
        <SettingRow
          label="Delivery method"
          source="global"
          locked
          description="Persistence is not available yet."
          control={<span className="font-mono text-[11.5px] text-muted">Both · coming soon</span>}
        />
      </SettingsCard>
    </div>
  );
}
