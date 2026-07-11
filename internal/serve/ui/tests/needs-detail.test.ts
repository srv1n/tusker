import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const readSource = (path: string) => readFileSync(path, "utf8");

test("Needs me cards open from the card surface while preserving review actions", () => {
  const card = readSource("src/components/needs/NeedCard.tsx");
  const inbox = readSource("src/features/inbox/GlobalInbox.tsx");

  const focusTarget = card.indexOf("data-need-focus-target");
  const actions = card.indexOf("<PrimaryAction");

  expect(focusTarget).toBeGreaterThan(-1);
  expect(card).toContain("function openTaskDetail()");
  expect(card).toContain('to: "/p/$projectId/docs"');
  expect(card).toContain("search: { path: need.taskId }");
  expect(card).toContain("onClick={onCardClick}");
  expect(card).toContain("onKeyDown={onCardKeyDown}");
  expect(card).toContain("target?.closest(CARD_INTERACTIVE_TARGET)");
  expect(card).toContain("window.getSelection()?.toString()");
  expect(card).not.toContain("Open details");
  expect(actions).toBeGreaterThan(focusTarget);
  expect(card).toContain('need.kind === "review" ? "Send back"');
  expect(card).toContain('need.kind === "review"\n            ? "Accept & close"');

  expect(inbox).toContain('target.querySelector<HTMLElement>("[data-need-focus-target]")');
  expect(inbox).toContain("focusTarget.focus()");
});

test("task-detail navigation renders the complete existing task-detail API surface", () => {
  const documentView = readSource("src/features/docs/DocumentView.tsx");
  const taskContract = readSource("src/features/docs/TaskContract.tsx");

  expect(documentView).toContain("if (isTaskId(path)) return <TaskContract projectId={projectId} taskId={path} />");
  expect(taskContract).toContain("const q = useTask(taskId, projectId);");
  for (const section of ["Intent", "Acceptance", "Verification", "Evidence", "Run history", "Gates"]) {
    expect(taskContract).toContain(section);
  }
});
