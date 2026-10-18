import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { Card, Chip, Mono } from "@/components/ui/primitives";
import { Button } from "@/components/ui/controls";
import { SectionLabel } from "@/components/ui/page";
import { QueryBoundary, SkeletonRows } from "@/components/ui/states";
import type { Tone } from "@/components/ui/tone";
import { useProcesses, useProcessStop } from "@/lib/queries";
import { duration } from "@/lib/time";
import type { ProcessState, TuskerProcess } from "@/types/domain";

/** Silent is the only live state that needs attention; long-running is not a problem. */
export function processStateTone(state: ProcessState): Tone {
  switch (state) {
    case "running": return "pass";
    case "long_running": return "info";
    case "silent": return "warn";
    case "stopping": return "muted";
    default: return "neutral";
  }
}

export function processStateLabel(state: ProcessState): string {
  return state === "long_running" ? "long-running" : state;
}

/** Every process Tusker started, plus those stopped in the last 24 hours. */
export function ProcessesSection({ projectId }: { projectId: string }) {
  const processes = useProcesses(projectId);
  return <section aria-label="Processes" className="mb-7">
    <SectionLabel>Processes</SectionLabel>
    <p className="mb-2 mt-1 text-[12px] text-muted">Agent sessions and checks Tusker started. Stop ends the whole process group. Stopped processes stay listed with their reason for 24 hours.</p>
    <QueryBoundary q={processes} loading={<SkeletonRows rows={3} />}>{(data) => data.processes.length === 0
      ? <Card className="border-dashed p-4 text-[13px] text-muted">No Tusker processes are running.</Card>
      : <Card className="overflow-x-auto"><table className="w-full min-w-[860px] text-left text-[12px]">
        <thead><tr className="border-b border-line text-[10px] uppercase tracking-[0.08em] text-faint">
          <th className="px-3 py-2 font-medium">What</th><th className="px-3 py-2 font-medium">State</th><th className="px-3 py-2 font-medium">Runtime · last sign of life</th><th className="px-3 py-2 font-medium">Owner</th><th className="px-3 py-2 font-medium">PID · group</th><th className="px-3 py-2 font-medium"><span className="sr-only">Action</span></th>
        </tr></thead>
        <tbody className="divide-y divide-line-soft">{data.processes.map((process) => <ProcessRow key={process.id} projectId={projectId} process={process} />)}</tbody>
      </table></Card>}
    </QueryBoundary>
  </section>;
}

function ProcessRow({ projectId, process }: { projectId: string; process: TuskerProcess }) {
  const stopped = process.state === "stopped";
  return <tr className={stopped ? "text-muted" : undefined}>
    <td className="px-3 py-2"><span className="block font-medium text-ink">{process.what}</span><Mono className="text-[10px] text-faint">{process.kind === "agent_session" ? "agent session" : "check"}</Mono></td>
    <td className="px-3 py-2"><Chip tone={processStateTone(process.state)}>{processStateLabel(process.state)}</Chip>{process.reason && <span className="mt-1 block max-w-[16rem] text-[10.5px] text-muted">{process.reason}</span>}</td>
    <td className="px-3 py-2"><Mono className="block text-[11px]">{duration(process.runtimeSec)}</Mono><Mono className="block text-[10px] text-faint">{process.quietSec < 10 ? "active now" : `quiet ${duration(process.quietSec)}`}</Mono></td>
    <td className="px-3 py-2"><ProcessOwner projectId={projectId} process={process} /></td>
    <td className="px-3 py-2"><Mono className="text-[11px]">{process.pid} · {process.pgid}</Mono></td>
    <td className="px-3 py-2 text-right">{process.canStop && <StopControl projectId={projectId} process={process} />}</td>
  </tr>;
}

function ProcessOwner({ projectId, process }: { projectId: string; process: TuskerProcess }) {
  const tasks = process.taskIds ?? [];
  if (!tasks.length) return <Mono className="break-all text-[10px] text-faint">{process.owner}</Mono>;
  return <span className="flex flex-col gap-0.5">
    {tasks.map((taskId) => <Link key={taskId} to="/p/$projectId/tasks/$taskId" params={{ projectId: process.projectId || projectId, taskId }} className="font-mono text-[11px] text-info hover:underline">{taskId}</Link>)}
    {process.attemptId && <Mono className="text-[10px] text-faint">attempt {process.attemptId}</Mono>}
  </span>;
}

/** Inline two-step confirm: no browser dialogs. */
function StopControl({ projectId, process }: { projectId: string; process: TuskerProcess }) {
  const [confirming, setConfirming] = useState(false);
  const stop = useProcessStop(projectId);
  if (stop.error) return <span role="alert" className="block max-w-[14rem] text-[11px] text-fail">{stop.error instanceof Error ? stop.error.message : String(stop.error)}</span>;
  if (!confirming) return <Button size="sm" variant="default" onClick={() => setConfirming(true)}>Stop</Button>;
  return <span className="inline-flex items-center gap-1.5">
    <span className="text-[11px] text-muted">Stop group {process.pgid}?</span>
    <Button size="sm" variant="danger" disabled={stop.isPending} onClick={() => stop.mutate(process.id)}>{stop.isPending ? "Stopping…" : "Confirm"}</Button>
    <Button size="sm" variant="ghost" disabled={stop.isPending} onClick={() => setConfirming(false)}>Cancel</Button>
  </span>;
}
