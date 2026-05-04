// Templater user script - Tusker V5
//
// Drop this file into your Templater user script folder. Templater exposes
// each exported function as tp.user.<name>.

const TRANSITION_DATE_FIELDS = {
  active: "started",
  blocked: "blocked_since",
  review: "review_requested_at",
  done: "completed",
  cancelled: "cancelled_at",
};

const VALID_TASK_STATUSES = [
  "draft",
  "ready",
  "active",
  "blocked",
  "review",
  "rework",
  "done",
  "cancelled",
];

async function setTaskStatus(tp, newStatus) {
  if (!VALID_TASK_STATUSES.includes(newStatus)) {
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
    if (fm.type !== "task") {
      new Notice("Active file is not a Tusker task.");
      return;
    }

    fm.status = newStatus;
    fm.updated = today;

    const field = TRANSITION_DATE_FIELDS[newStatus];
    if (!field) return;
    if (field === "started" && fm.started) return;
    fm[field] = today;
  });

  new Notice(`Status -> ${newStatus}`);
}

async function markReady(tp) { return setTaskStatus(tp, "ready"); }
async function markActive(tp) { return setTaskStatus(tp, "active"); }
async function markBlocked(tp) { return setTaskStatus(tp, "blocked"); }
async function markReview(tp) { return setTaskStatus(tp, "review"); }
async function markRework(tp) { return setTaskStatus(tp, "rework"); }
async function markDone(tp) { return setTaskStatus(tp, "done"); }
async function markCancelled(tp) { return setTaskStatus(tp, "cancelled"); }

module.exports = {
  setTaskStatus,
  markReady,
  markActive,
  markBlocked,
  markReview,
  markRework,
  markDone,
  markCancelled,
};
