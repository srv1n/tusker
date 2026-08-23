export function isTaskId(path: string): boolean {
  return /^[A-Z]{2,4}-T-\d{3,}$/.test(path.trim());
}

export function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/(^-|-$)/g, "");
}
