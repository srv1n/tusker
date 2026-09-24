
import React from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "@/components/ui/action-feedback";
import { HumanActionCard } from "@/features/human-action/HumanActionCard";
const base = { kind: "decision", rawKind: "decision", gateId: "G-1", title: "Gate", action: "Approve", whyAgentCannot: "Human", completionCondition: "Recorded", materialRevision: "rev", blockedTaskIds: ["T-1"], covers: [], acceptance: [] };
const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
(window as any).__errors = [];
window.addEventListener("error", (event) => (window as any).__errors.push(event.message));
const root = createRoot(document.getElementById("root")!);
(window as any).__show = (kind: string) => root.render(<QueryClientProvider client={client}><ConfirmProvider><HumanActionCard action={{ ...base, kind, title: kind, messageId: "m-1", requestId: "r-1" }} taskId="T-1" taskTitle="Task" projectId="p" approvals={[]} /></ConfirmProvider></QueryClientProvider>);
(window as any).__show("decision");
