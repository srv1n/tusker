import type { DocContent, DocOutlineEntry, TaskDetail } from "@/types/domain";

const TASK_DOC_RE = /^\.tusker\/work\/tasks\/([A-Z]{2,4}-T-\d{3,})\.md$/;

export function taskDocPath(taskId: string): string {
  return `.tusker/work/tasks/${taskId}.md`;
}

export function taskIdFromDocPath(path: string): string | null {
  return TASK_DOC_RE.exec(path.trim())?.[1] ?? null;
}

export function taskDetailToDocContent(task: TaskDetail): DocContent {
  const sections = [
    "# " + task.id + " · " + task.title,
    "",
    "## Intent",
    "",
    task.intent,
  ];

  if (task.acceptance.length > 0) {
    sections.push(
      "",
      "## Acceptance",
      "",
      "| criterion | proof |",
      "| --- | --- |",
      ...task.acceptance.map((row) => `| ${escapeTableCell(row.text)} | ${row.proof} |`),
    );
  }

  if (task.nonGoals.length > 0) {
    sections.push("", "## Non-goals", "", ...task.nonGoals.map((goal) => `- ${goal}`));
  }

  if (task.verification.length > 0) {
    sections.push(
      "",
      "## Verification",
      "",
      ...task.verification.flatMap((row) => [
        "```bash",
        row.command,
        "```",
        row.detail ? row.detail : "",
      ]),
    );
  }

  if (task.knowledgeDelta) {
    sections.push("", "## Knowledge delta", "", task.knowledgeDelta);
  }

  const outline = [
    { level: 2, text: "Intent", slug: "intent" },
    task.acceptance.length > 0 && { level: 2, text: "Acceptance", slug: "acceptance" },
    task.nonGoals.length > 0 && { level: 2, text: "Non-goals", slug: "non-goals" },
    task.verification.length > 0 && { level: 2, text: "Verification", slug: "verification" },
    task.knowledgeDelta && { level: 2, text: "Knowledge delta", slug: "knowledge-delta" },
  ].filter(Boolean) as DocOutlineEntry[];

  return {
    path: taskDocPath(task.id),
    title: `${task.id} · ${task.title}`,
    kind: "task",
    updatedAt: task.updatedAt,
    rev: "task-detail",
    frontmatter: [
      { key: "id", value: task.id, locked: true },
      { key: "status", value: task.status, locked: true },
      { key: "readiness", value: task.readiness, locked: true },
      { key: "priority", value: task.priority, locked: true },
      { key: "risk", value: task.risk, locked: true },
      { key: "epic", value: task.epicId, locked: true },
      { key: "state_rev", value: "1", locked: true },
    ],
    outline,
    markdown: sections.join("\n").replace(/\n{3,}/g, "\n\n").trimEnd(),
  };
}

export function readMarkdownSection(markdown: string, heading: string): string | null {
  const bounds = findSection(markdown, heading);
  if (!bounds) return null;
  return trimOuterBlankLines(markdown.split("\n").slice(bounds.bodyStart, bounds.end)).join("\n");
}

export function replaceMarkdownSection(markdown: string, heading: string, body: string): string {
  const lines = markdown.split("\n");
  const bounds = findSection(markdown, heading);
  const bodyLines = trimOuterBlankLines(body.split("\n"));
  const replacement = [`## ${heading}`, "", ...bodyLines];

  if (!bounds) {
    const prefix = trimTrailingBlankLines(lines);
    return [...prefix, "", ...replacement].join("\n").trimEnd();
  }

  const before = lines.slice(0, bounds.heading);
  const after = lines.slice(bounds.end);
  const spacer = after.length > 0 && bodyLines.length > 0 ? [""] : [];
  return [...before, ...replacement, ...spacer, ...after].join("\n").trimEnd();
}

function findSection(
  markdown: string,
  heading: string,
): { heading: number; bodyStart: number; end: number } | null {
  const lines = markdown.split("\n");
  const wanted = normalizeHeading(heading);

  for (let i = 0; i < lines.length; i++) {
    const match = /^(#{1,6})\s+(.*\S)\s*$/.exec(lines[i] ?? "");
    if (!match || match[1] !== "##" || normalizeHeading(match[2] ?? "") !== wanted) continue;

    let end = lines.length;
    for (let j = i + 1; j < lines.length; j++) {
      const next = /^(#{1,6})\s+\S/.exec(lines[j] ?? "");
      if (next && next[1].length <= 2) {
        end = j;
        break;
      }
    }
    return { heading: i, bodyStart: i + 1, end };
  }

  return null;
}

function normalizeHeading(value: string): string {
  return value.trim().toLowerCase().replace(/\s+/g, " ");
}

function trimOuterBlankLines(lines: string[]): string[] {
  return trimTrailingBlankLines(trimLeadingBlankLines(lines));
}

function trimLeadingBlankLines(lines: string[]): string[] {
  let start = 0;
  while (start < lines.length && (lines[start] ?? "").trim() === "") start++;
  return lines.slice(start);
}

function trimTrailingBlankLines(lines: string[]): string[] {
  let end = lines.length;
  while (end > 0 && (lines[end - 1] ?? "").trim() === "") end--;
  return lines.slice(0, end);
}

function escapeTableCell(value: string): string {
  return value.replace(/\|/g, "\\|").replace(/\n/g, " ");
}
