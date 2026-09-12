import { useId } from "react";
import { cn } from "@/lib/cn";
import { Chip } from "@/components/ui/primitives";
import type { AgentAccessV1, CommandBehavior, CommandPolicy, ControlSupport } from "@/types/domain";

const BEHAVIORS: Array<{ behavior: CommandBehavior; label: string; tone: "pass" | "warn" | "fail" }> = [
  { behavior: "automatic", label: "Automatic", tone: "pass" },
  { behavior: "ask", label: "Ask each time", tone: "warn" },
  { behavior: "block", label: "Block", tone: "fail" },
];

function humanize(value: string): string {
  return value.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function mechanismLabel(value: ControlSupport["mechanism"]): string {
  return value === "native_hook" ? "Native hook" : value === "native_setting" ? "Native setting" : humanize(value);
}

function behaviorRules(policy: CommandPolicy | undefined, behavior: CommandBehavior) {
  return policy?.rules?.filter((rule) => rule.behavior === behavior) || [];
}

export function CommandPolicyDetails({
  access,
  policy,
  controls,
  state,
  issues = [],
}: {
  access: AgentAccessV1;
  policy?: CommandPolicy;
  controls: ControlSupport[];
  state?: "ready" | "needs_setup" | "unsupported" | "stale";
  issues?: Array<{ code: string; field: string; message: string; remedy: string }>;
}) {
  const titleId = `command-policy-${useId().replaceAll(":", "")}`;
  const unsupportedControls = controls.filter((control) => control.mechanism === "unsupported");
  const unavailable = state === "unsupported" || unsupportedControls.length > 0;
  const policyPending = !policy;

  return (
    <section aria-labelledby={titleId} className="min-w-0 max-w-full rounded-lg border border-line bg-surface px-3 py-3">
      <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h4 id={titleId} className="text-[12px] font-semibold text-ink-soft">Agent adapter</h4>
          <p className="mt-1 max-w-[62ch] text-[12px] leading-relaxed text-muted">
            Translates these access choices into the installed agent's supported controls.
          </p>
        </div>
        <Chip tone={unavailable ? "fail" : policyPending ? "warn" : "info"} variant="soft" className="rounded-md">
          {unavailable ? "Unavailable" : policyPending ? "Needs route check" : "Resolved policy"}
        </Chip>
      </div>

      {unavailable && (
        <div role="alert" className="mt-3 rounded-lg border border-fail/25 bg-fail-soft px-3 py-2 text-[12px] text-fail">
          <p className="font-medium">Required access restriction is unavailable for this provider.</p>
          {issues.length > 0 ? <ul className="mt-1 list-disc space-y-0.5 pl-4">{issues.map((issue) => <li key={`${issue.code}:${issue.field}`}>{issue.message} {issue.remedy}</li>)}</ul> : <p className="mt-1">This profile cannot run until the provider reports an operative native restriction.</p>}
        </div>
      )}

      {access.mode === "review_only" && (
        <p className="mt-3 rounded-lg border border-fail/25 bg-fail-soft px-3 py-2 text-[12px] text-fail">
          <strong>Review only blocks project writes.</strong> Allow-once approval cannot override this mandatory block.
        </p>
      )}

      <div className="mt-3 grid gap-2" aria-label="Effective command behaviors">
        {BEHAVIORS.map(({ behavior, label, tone }) => {
          const rules = behaviorRules(policy, behavior);
          return (
            <div key={behavior} className="min-w-0 rounded-lg border border-line bg-panel px-3 py-2">
              <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
                <p className="text-[12px] font-medium text-ink-soft">{label}</p>
                <Chip tone={tone} variant="soft" className="rounded-md text-[10px]">{label}</Chip>
              </div>
              {rules.length > 0 ? <ul className="mt-1 space-y-1 text-[12px] text-muted">{rules.map((rule) => <li key={rule.id}><span className="text-ink-soft">{rule.examples.join(", ")}</span><span className="block text-[11px]">Coverage: {rule.coverage}</span></li>)}</ul> : <p className="mt-1 text-[12px] text-muted">{policyPending ? "Resolve the installed route to show its effective examples." : "No examples reported for this behavior."}</p>}
            </div>
          );
        })}
      </div>

      <div className="mt-4 border-t border-line-soft pt-3">
        <h5 className="text-[12px] font-medium text-ink-soft">Provider coverage</h5>
        {controls.length > 0 ? <ul className="mt-2 grid gap-2">{controls.map((control) => {
          const unsupported = control.mechanism === "unsupported";
          return <li key={control.control} className="flex min-w-0 flex-wrap items-start gap-2 rounded-lg border border-line bg-panel px-3 py-2"><Chip tone={unsupported ? "fail" : control.mechanism === "advisory" ? "warn" : "info"} variant="soft" className="rounded-md text-[10px]">{unsupported ? "Unavailable" : mechanismLabel(control.mechanism)}</Chip><div className="min-w-0 flex-1"><p className={cn("text-[12px]", unsupported ? "text-fail" : "text-ink-soft")}>{humanize(control.control)}</p><p className="mt-0.5 break-words text-[11px] text-muted">{control.coverage}</p></div></li>;
        })}</ul> : <p className="mt-1 text-[12px] text-muted">Provider coverage is not reported until the installed route is resolved.</p>}
      </div>

    </section>
  );
}
