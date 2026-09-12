import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const read = (path: string) => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const card = read("src/features/human-action/HumanActionCard.tsx");
const api = read("src/lib/api.ts");
const queries = read("src/lib/queries.ts");
const domain = read("src/types/domain.ts");
const product = read("src/features/product/TaskScreens.tsx");
const inspector = read("src/features/workbench/inspector/TaskInspector.tsx");

test("AgentAccessApprovalUI renders one safe immutable request surface", () => {
  for (const value of [
    "redactedArguments",
    "workingDirectory",
    "targets",
    "policyFingerprint",
    "Allow once",
    "Block",
    "Dismiss",
    "pending",
    "allowed",
    "denied",
    "expired",
    "expectedRevision",
    "nativeOptionKind",
    "No approval action is available",
    "Request {approval.requestId}",
  ]) expect(card).toContain(value);
  expect(card).toContain("new Map(source.map((item) => [item.requestId, item]))");
  expect(card).toContain("approvalIsLive(approval)");
  expect(card).not.toContain("approval.arguments");
  expect(card).not.toContain("Allow all");
});

test("approval API and query mutation bind decisions to request revision", () => {
  expect(api).toContain("agentAccessApprovals");
  expect(api).toContain("agentAccessApprovalRespond");
  expect(api).toContain("/approvals/${encodeURIComponent(requestId)}/respond");
  expect(api).toContain("expectedRevision");
  expect(queries).toContain("useAgentAccessApprovals");
  expect(queries).toContain("useAgentAccessApprovalAction");
  expect(queries).toContain("invalidateQueries({ queryKey: qk.agentAccessApprovals");
  expect(domain).toContain("export interface AgentAccessApproval");
  expect(domain).toContain("agentAccessApprovals?: AgentAccessApproval[]");
});

test("task page and inspector use the shared approval projection", () => {
  expect(product).toContain("<AgentAccessApprovalList");
  expect(product).toContain("approvals={detail.agentAccessApprovals}");
  expect(inspector).toContain("<AgentAccessApprovalList");
  expect(inspector).toContain("approvals={task.agentAccessApprovals}");
});
