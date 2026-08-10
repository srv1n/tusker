package main

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func scratchGCCmd(args Args) error {
	if args.Bool("help") {
		printGCHelp()
		return nil
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	ttlDays, err := scratchGCTTLDays(args)
	if err != nil {
		return err
	}
	// The confirmation is parsed strictly: Args.Bool treats an unparseable value
	// as true, which would turn `--yes definitely-not` into a deletion.
	apply, err := scratchGCConfirmed(args)
	if err != nil {
		return err
	}
	ttl := time.Duration(ttlDays) * 24 * time.Hour
	now := time.Now()
	cutoff := now.Add(-ttl)
	stale, err := planScratchGC(vaultPath, ttl, now)
	if err != nil {
		return scratchGCVaultError(vaultPath, err)
	}
	sort.Slice(stale, func(i, j int) bool { return stale[i].Bytes > stale[j].Bytes })
	total := totalScratchBytes(stale)

	var outcome scratchGCOutcome
	var applyErr error
	if apply {
		outcome, applyErr = applyScratchGC(vaultPath, stale, cutoff)
		if applyErr != nil {
			// Partial progress is real deletion; report it before failing.
			if !args.Bool("quiet") && !args.Bool("json") {
				printScratchGCOutcome(outcome)
			}
			return tuskerError("SCRATCH_GC_FAILED", fmt.Sprintf("scratch gc stopped after deleting %d of %d entries: %v", len(outcome.Deleted), len(stale), applyErr),
				withPath(outcome.Failed),
				withContext(scratchGCOutcomeJSON(outcome)))
		}
	}

	if args.Bool("json") {
		payload := map[string]any{
			"ok": true, "vault": vaultPath, "dry_run": !apply, "ttl_days": ttlDays,
			"count": len(stale), "bytes": total, "entries": scratchEntriesJSON(stale, now),
		}
		if apply {
			for key, value := range scratchGCOutcomeJSON(outcome) {
				payload[key] = value
			}
		}
		emitJSON(payload)
		return nil
	}
	if args.Bool("quiet") {
		return nil
	}
	for _, entry := range stale {
		fmt.Printf("- %s  %s  %dd old\n", entry.Name, humanBytes(entry.Bytes), int(now.Sub(entry.Newest).Hours()/24))
	}
	if apply {
		printScratchGCOutcome(outcome)
		return nil
	}
	fmt.Printf("%d stale scratch entries older than %dd (%s). Re-run with --yes to apply.\n", len(stale), ttlDays, humanBytes(total))
	return nil
}

// scratchGCTTLDays rejects values that would overflow an int64 nanosecond
// time.Duration: past maxScratchTTLDays the cutoff wraps into the future and
// every entry looks stale.
func scratchGCTTLDays(args Args) (int64, error) {
	raw := strings.TrimSpace(args.String("ttl"))
	if raw == "" {
		return defaultScratchTTLDays, nil
	}
	days, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || days < 0 || days > maxScratchTTLDays {
		return 0, tuskerError(errorInvalidArg, fmt.Sprintf("--ttl must be a whole number of days between 0 and %d, got: %s", maxScratchTTLDays, raw))
	}
	return days, nil
}

// scratchGCConfirmed reads --yes strictly. A bare --yes means true; anything
// that is not boolean-like is an error rather than a silent deletion.
func scratchGCConfirmed(args Args) (bool, error) {
	raw, ok := args["yes"]
	if !ok {
		return false, nil
	}
	if strings.TrimSpace(raw) == "" {
		return true, nil
	}
	confirmed, err := parseBooleanArg(raw, true)
	if err != nil {
		return false, tuskerError(errorInvalidArg, fmt.Sprintf("--yes takes no value or one of true/false/yes/no/1/0, got: %s", raw))
	}
	return confirmed, nil
}

// scratchGCVaultError turns a refusal to authorize deletion into an operator
// message instead of a crash or a silent empty result.
func scratchGCVaultError(vaultPath string, err error) error {
	switch {
	case errors.Is(err, errNotTuskerVault):
		return tuskerError(errorInvalidArg, fmt.Sprintf("%s is not a recognized Tusker vault; refusing to delete anything under it", vaultPath),
			withHint("point --vault at a vault with work/tasks (or work/epics, work/gates) and one of WORKFLOW.md, SKILL.md, config.yaml, _system"))
	case errors.Is(err, errScratchRootUnsafe):
		return tuskerError(errorInvalidArg, fmt.Sprintf("%s/scratch is not a real directory; refusing to delete through it", vaultPath),
			withHint("a symlinked scratch root would redirect deletion outside the vault"))
	default:
		return err
	}
}

func scratchEntriesJSON(entries []scratchEntry, now time.Time) []map[string]any {
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		items = append(items, map[string]any{
			"name":     entry.Name,
			"path":     entry.Path,
			"bytes":    entry.Bytes,
			"age_days": int(now.Sub(entry.Newest).Hours() / 24),
		})
	}
	return items
}

func scratchGCOutcomeJSON(outcome scratchGCOutcome) map[string]any {
	return map[string]any{
		"deleted":       scratchEntriesJSON(outcome.Deleted, time.Now()),
		"skipped":       scratchEntriesJSON(outcome.Skipped, time.Now()),
		"bytes_removed": outcome.Reclaimed,
		"failed_path":   outcome.Failed,
	}
}

func printScratchGCOutcome(outcome scratchGCOutcome) {
	fmt.Printf("Deleted %d scratch entries (%s of logical file size removed).\n", len(outcome.Deleted), humanBytes(outcome.Reclaimed))
	for _, entry := range outcome.Skipped {
		fmt.Printf("- kept %s: became active after planning\n", entry.Name)
	}
	if outcome.Failed != "" {
		fmt.Printf("Stopped at %s.\n", outcome.Failed)
	}
}

// humanBytes reports binary units. These are logical FileInfo sizes; hardlinks,
// sparse files, and open descriptors all make actual freed disk differ.
func humanBytes(n int64) string {
	const unit = 1024
	if n > -unit && n < unit {
		return fmt.Sprintf("%dB", n)
	}
	sign, value := "", float64(n)
	if value < 0 {
		sign, value = "-", -value
	}
	value /= unit
	exp := 0
	// Roll over at the point %.1f would round up to a full unit, so no output
	// ever reads "1024.0KiB".
	for value >= unit-0.05 && exp < 3 {
		value /= unit
		exp++
	}
	return fmt.Sprintf("%s%.1f%ciB", sign, value, "KMGT"[exp])
}

func printGCHelp() {
	fmt.Println(`Usage:
  tusker gc [--ttl <days>] [--yes] [--vault <path>] [--json] [--quiet]

Purpose:
  Sweep .tusker/scratch/, deleting any top-level entry — task-keyed or
  hand-named — whose newest content is older than the TTL. Scratch is
  ephemeral by contract; anything durable belongs in evidence.

Behavior:
  - default is dry-run; pass --yes to apply (--yes is the only apply flag)
  - --ttl overrides the 14-day default; --ttl 0 purges every entry
  - staleness of an entry is the newest mtime found beneath it
  - the target must be a recognized Tusker vault or the command refuses
  - reported sizes are logical file sizes, not measured freed disk space
  - a failed deletion still reports what was already deleted, and exits non-zero

Examples:
  tusker gc
  tusker gc --ttl 7 --json
  tusker gc --yes`)
}
