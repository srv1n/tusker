// Templater user script — Obsidian Vault Tracker v0.4
//
// Drop this file into your Templater "User script folder" (set via
// Templater settings → User Script Functions Folder). Templater will expose
// each exported function as tp.user.<name>.
//
// Bind any of the commands below to a hotkey or palette command to move the
// active change note through its status lifecycle. The script stamps the
// matching transition date field in the same write so Bases/Kanban views
// reflect the move immediately.

const TRANSITION_DATE_FIELDS = {
  active: "started",
  blocked: "blocked_since",
  in_review: "review_opened",
  done: "completed",
  cancelled: "cancelled_at",
};

async function setChangeStatus(tp, newStatus) {
  const valid = ["intake", "active", "blocked", "in_review", "done", "cancelled"];
  if (!valid.includes(newStatus)) {
    new Notice(`Invalid status: ${newStatus}`);
    return;
  }

  const file = tp.file.find_tfile(tp.file.path(true));
  if (!file) {
    new Notice("No active file.");
    return;
  }

  const today = window.moment().format("YYYY-MM-DD");

  await app.fileManager.processFrontMatter(file, (fm) => {
    if (fm.type !== "change") {
      new Notice("Active file is not a change note.");
      return;
    }

    fm.status = newStatus;
    fm.updated = today;

    const field = TRANSITION_DATE_FIELDS[newStatus];
    if (!field) return;
    if (field === "started" && fm.started) return;
    fm[field] = today;
  });

  new Notice(`Status → ${newStatus}`);
}

async function markActive(tp) { return setChangeStatus(tp, "active"); }
async function markBlocked(tp) { return setChangeStatus(tp, "blocked"); }
async function markInReview(tp) { return setChangeStatus(tp, "in_review"); }
async function markDone(tp) { return setChangeStatus(tp, "done"); }
async function markCancelled(tp) { return setChangeStatus(tp, "cancelled"); }

module.exports = {
  setChangeStatus,
  markActive,
  markBlocked,
  markInReview,
  markDone,
  markCancelled,
};
