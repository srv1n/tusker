/*
  Notifications tab — the two toggles (human gate, stale run) plus a delivery
  method. Nothing else: no digests, no email (addendum §2.6). Every row still
  carries its provenance chip (addendum §1.3).
*/

import { useState } from "react";
import { SegmentedControl, Toggle } from "@/components/ui/controls";
import type { SegmentOption } from "@/components/ui/controls";
import { SettingRow, SettingsCard } from "./parts";
import { deliveryOptions, notifyRows, type Delivery } from "./mock";

const deliverySegments: SegmentOption<Delivery>[] = deliveryOptions.map((d) => ({ value: d, label: d }));

export function NotificationsSection() {
  // TODO(api): persist notification prefs through the settings API.
  const [notify, setNotify] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(notifyRows.map((r) => [r.key, r.on])),
  );
  const [delivery, setDelivery] = useState<Delivery>("Both");

  return (
    <div className="animate-rise">
      <SettingsCard>
        {notifyRows.map((r) => (
          <SettingRow
            key={r.key}
            label={`Notify on ${r.key}`}
            source={r.source}
            control={
              <Toggle
                checked={notify[r.key]}
                onChange={(v) => setNotify((prev) => ({ ...prev, [r.key]: v }))}
              />
            }
          />
        ))}
        <SettingRow
          label="Delivery method"
          source="global"
          control={
            <SegmentedControl<Delivery>
              size="sm"
              options={deliverySegments}
              value={delivery}
              onChange={setDelivery}
            />
          }
        />
      </SettingsCard>
    </div>
  );
}
