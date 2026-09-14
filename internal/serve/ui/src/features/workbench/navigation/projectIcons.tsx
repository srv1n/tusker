import { Code2, Database, FileText, FolderKanban, Globe2, Headphones, Mic2, Music2, Package, Smartphone, Sparkles, Terminal, type LucideIcon } from "lucide-react";
import type { ProjectIconName } from "./navigationState";

export const PROJECT_ICON_CHANGED_EVENT = "tusker.project-icon.changed";

export interface ProjectIconChoice {
  name: Exclude<ProjectIconName, "auto">;
  label: string;
  Icon: LucideIcon;
  className: string;
}

export const projectIconChoices: readonly ProjectIconChoice[] = [
  { name: "code", label: "Code", Icon: Code2, className: "text-info" },
  { name: "database", label: "Database", Icon: Database, className: "text-accent" },
  { name: "folder", label: "Folder", Icon: FolderKanban, className: "text-warn" },
  { name: "globe", label: "Web", Icon: Globe2, className: "text-pass" },
  { name: "mic", label: "Microphone", Icon: Mic2, className: "text-fail" },
  { name: "music", label: "Music", Icon: Music2, className: "text-accent" },
  { name: "package", label: "Package", Icon: Package, className: "text-info" },
  { name: "phone", label: "Phone", Icon: Smartphone, className: "text-pass" },
  { name: "sparkles", label: "Sparkles", Icon: Sparkles, className: "text-warn" },
  { name: "terminal", label: "Terminal", Icon: Terminal, className: "text-ink-soft" },
  { name: "text", label: "Documents", Icon: FileText, className: "text-info" },
  { name: "audio", label: "Audio", Icon: Headphones, className: "text-fail" },
];

export function projectInitials(name: string): string {
  const words = name.trim().split(/[\s_-]+/).filter(Boolean);
  return (words.length > 1 ? words.slice(0, 2).map((word) => word[0]) : [words[0]?.slice(0, 2) ?? "?"]).join("").toUpperCase();
}

export function projectIconChoice(name: ProjectIconName | undefined): ProjectIconChoice | undefined {
  return projectIconChoices.find((choice) => choice.name === name);
}
