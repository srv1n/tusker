package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func setStatus(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	nextStatus, err := requireArg(args, "status")
	if err != nil {
		return err
	}
	actor := fallback(fallback(args.String("actor"), args.String("by")), "automation")
	reason := args.String("reason")
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	noteType := stringField(note.Data, "type")
	var statusSet map[string]struct{}
	switch noteType {
	case "epic":
		statusSet = epicStatuses
	case "bug":
		statusSet = storyStatuses
	case "doc":
		statusSet = docStatuses
	default:
		statusSet = storyStatuses
	}
	if _, ok := statusSet[nextStatus]; !ok {
		return tuskerError(errorInvalidField, fmt.Sprintf("invalid %s status: %s", noteType, nextStatus), withContext(map[string]any{"field": "status", "value": nextStatus}))
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	prev := stringField(data, "status")
	date := todayISO()
	now := time.Now().UTC().Format(time.RFC3339)
	if prev == nextStatus {
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "id": id, "status": nextStatus, "unchanged": true})
		} else if !args.Bool("quiet") {
			fmt.Printf("%s already at status %q\n", id, nextStatus)
		}
		return nil
	}
	if noteType == "story" || noteType == "bug" {
		if nextStatus == "in_review" {
			if err := assertEvidenceGate(data, body, id); err != nil {
				return err
			}
			if stringField(data, "review_state") == "" || stringField(data, "review_state") == "none" || stringField(data, "review_state") == "changes_requested" {
				data["review_state"] = "requested"
			}
			if stringField(data, "review_requested_at") == "" {
				data["review_requested_at"] = now
			}
		}
		if nextStatus == "done" {
			if err := assertEvidenceGate(data, body, id); err != nil {
				return err
			}
			if err := assertAttestationGate(data, id); err != nil {
				return err
			}
		}
	}
	if noteType == "epic" && nextStatus == "done" {
		allNotes, err := listAllNotes(vaultPath)
		if err != nil {
			return err
		}
		var unfinished []string
		for _, current := range allNotes {
			t := stringField(current.Data, "type")
			if t != "story" && t != "bug" {
				continue
			}
			if wikiTarget(current.Data["epic"]) != stringField(data, "id") {
				continue
			}
			status := stringField(current.Data, "status")
			if status != "done" && status != "cancelled" {
				unfinished = append(unfinished, stringField(current.Data, "id"))
			}
		}
		if len(unfinished) > 0 {
			return tuskerError(errorChildrenUnfinished, fmt.Sprintf("Epic %s has %d unfinished child(ren): %s", id, len(unfinished), strings.Join(unfinished, ", ")), withContext(map[string]any{"unfinished": unfinished}))
		}
	}
	data["status"] = nextStatus
	data["updated"] = date
	if field := statusTransitionDateFields[nextStatus]; field != "" {
		if !(field == "started" && stringField(data, "started") != "") {
			data[field] = date
		}
	}
	if noteType == "doc" && nextStatus == "published" && stringField(data, "published_at") == "" {
		data["published_at"] = date
	}
	appendTransition(data, orderedTransition(now, "status", prev, nextStatus, actor, reason))
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — %s — status: %s → %s%s", date, actor, fallback(prev, "(unset)"), nextStatus, suffixReason(reason)))
	content, err := serializeDocument(data, body, frontmatterOrderForType(noteType))
	if err != nil {
		return err
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "id": id, "from": nilIfEmpty(prev), "to": nextStatus})
	} else if !args.Bool("quiet") {
		fmt.Printf("%s: %s → %s\n", id, fallback(prev, "(unset)"), nextStatus)
	}
	autoReindex(vaultPath)
	return nil
}

func pickup(args Args) error {
	return tuskerError(errorInvalidTransition, "pickup no longer writes runtime state into notes. Use `tusker workflow init`, `tusker projects add`, and daemon mode instead.")
}

func release(args Args) error {
	return tuskerError(errorInvalidTransition, "release no longer writes runtime state into notes. Use daemon-managed runs instead.")
}

func attest(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	by, err := requireArg(args, "by")
	if err != nil {
		return err
	}
	role, err := requireArg(args, "role")
	if err != nil {
		return err
	}
	if role != "agent" && role != "human" {
		return tuskerError(errorInvalidArg, `--role must be agent or human`, withContext(map[string]any{"arg": "--role", "value": role}))
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	rule := attestationRequirement(stringField(data, "risk"))
	if rule.NeedsHuman && role != "human" {
		return tuskerError(errorAttestationRole, fmt.Sprintf(`%s: risk "%s" requires a human attestation — cannot attest as "%s"`, id, stringField(data, "risk"), role), withContext(map[string]any{"id": id, "risk": stringField(data, "risk"), "role": role}))
	}
	date := todayISO()
	data["attested_by"] = by
	data["attested_at"] = date
	data["attested_role"] = role
	data["updated"] = date
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — automation — attested by %s (%s)", date, by, role))
	content, err := serializeDocument(data, body, frontmatterOrderForType(stringField(data, "type")))
	if err != nil {
		return err
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s attested by %s (%s)\n", id, by, role)
	}
	return nil
}

func signoff(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	by, err := requireArg(args, "by")
	if err != nil {
		return err
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	if strings.ToLower(stringField(data, "risk")) != "critical" {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`signoff is only required for critical risk; %s is "%s"`, id, stringField(data, "risk")), withContext(map[string]any{"id": id, "risk": stringField(data, "risk")}))
	}
	date := todayISO()
	data["signoff_by"] = by
	data["signoff_at"] = date
	data["updated"] = date
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — automation — code-owner signoff by %s", date, by))
	content, err := serializeDocument(data, body, frontmatterOrderForType(stringField(data, "type")))
	if err != nil {
		return err
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s signed off by %s\n", id, by)
	}
	return nil
}

func attachEvidence(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	kind, err := requireArg(args, "kind")
	if err != nil {
		return err
	}
	inputPath, err := requireArg(args, "path")
	if err != nil {
		return err
	}
	noteText := args.String("note")
	date := todayISO()
	noteRef, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(noteRef.AbsolutePath)
	if err != nil {
		return err
	}
	link := inputPath
	if !strings.HasPrefix(inputPath, "http://") && !strings.HasPrefix(inputPath, "https://") {
		resolved, err := filepath.Abs(inputPath)
		if err != nil {
			return err
		}
		if !fileExists(resolved) {
			return tuskerError(errorNotFound, "File not found: "+resolved, withPath(resolved))
		}
		attachmentDir := filepath.Join(vaultPath, "Attachments", id)
		if err := ensureDir(attachmentDir); err != nil {
			return err
		}
		target := filepath.Join(attachmentDir, filepath.Base(resolved))
		if err := copyFile(resolved, target); err != nil {
			return err
		}
		link = filepath.ToSlash(filepath.Join("Attachments", id, filepath.Base(resolved)))
	}
	body = appendSectionBullet(body, "## Evidence", buildEvidenceBullet(kind, link, noteText, date), false)
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — automation — attached %s: %s%s", date, kind, link, suffixReason(noteText)))
	data["updated"] = date
	content, err := serializeDocument(data, body, frontmatterOrderForType(stringField(data, "type")))
	if err != nil {
		return err
	}
	if err := writeText(noteRef.AbsolutePath, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Attached %s to %s: %s\n", kind, id, link)
	}
	return nil
}

func promoteDecision(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	summary, err := requireArg(args, "summary")
	if err != nil {
		return err
	}
	target := fallback(args.String("target"), "architecture")
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	date := todayISO()
	var outputPath string
	if target == "architecture" {
		parsed := parseID(stringField(note.Data, "id"))
		switch {
		case parsed != nil && parsed.Kind == "epic":
			outputPath = filepath.Join(vaultPath, "epics", parsed.Acronym, "architecture.md")
			if !fileExists(outputPath) {
				outputPath = filepath.Join(vaultPath, "architecture.md")
			}
		case parsed != nil && (parsed.Kind == "story" || parsed.Kind == "bug"):
			epicArch := filepath.Join(vaultPath, "epics", parsed.Acronym, "architecture.md")
			if fileExists(epicArch) {
				outputPath = epicArch
			} else {
				outputPath = filepath.Join(vaultPath, "architecture.md")
			}
		default:
			outputPath = filepath.Join(vaultPath, "architecture.md")
		}
	} else if target == "agents" {
		return tuskerError(errorInvalidArg, `--target agents is a v1.1 feature — use --target architecture for now`, withContext(map[string]any{"arg": "--target", "value": target}))
	} else {
		return tuskerError(errorInvalidArg, "Invalid --target: "+target, withContext(map[string]any{"arg": "--target", "value": target}))
	}
	if err := ensureDecisionsFile(outputPath, date); err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(outputPath)
	if err != nil {
		return err
	}
	body = appendSectionBullet(body, "## Decisions", fmt.Sprintf("- %s — %s — see [[%s]]", date, summary, stringField(note.Data, "id")), true)
	data["updated"] = date
	content, err := serializeDocument(data, body, frontmatterOrder["note"])
	if err != nil {
		return err
	}
	if err := writeText(outputPath, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Promoted decision from %s to %s\n", id, outputPath)
	}
	return nil
}

func assertEvidenceGate(data map[string]any, body, id string) error {
	risk := strings.ToLower(stringField(data, "risk"))
	if risk == "medium" || risk == "high" || risk == "critical" {
		if !sectionHasSubstance(body, "## Evidence") {
			return tuskerError(errorEvidenceGate, fmt.Sprintf(`%s: risk "%s" requires substantive "## Evidence" before this transition`, id, risk), withContext(map[string]any{"id": id, "risk": risk}))
		}
	}
	if stringField(data, "type") == "story" && stringField(data, "change_type") == "feature" && isUISurface(data["surfaces"]) && (risk == "medium" || risk == "high" || risk == "critical") {
		if !evidenceHasAsset(body) {
			return tuskerError(errorUIDemoMissing, fmt.Sprintf(`%s: UI feature at risk "%s" needs a demo asset (video/gif/screenshot) in "## Evidence"`, id, risk), withContext(map[string]any{"id": id, "risk": risk}))
		}
	}
	return nil
}

func assertAttestationGate(data map[string]any, id string) error {
	rule := attestationRequirement(stringField(data, "risk"))
	if stringField(data, "attested_by") == "" || stringField(data, "attested_at") == "" || stringField(data, "attested_role") == "" {
		return tuskerError(errorAttestationMissing, fmt.Sprintf("%s: done requires attested_by, attested_at, attested_role (use `tusker attest`)", id), withContext(map[string]any{"id": id}))
	}
	if rule.NeedsHuman && stringField(data, "attested_role") != "human" {
		return tuskerError(errorAttestationRole, fmt.Sprintf(`%s: risk "%s" requires a human attestation`, id, stringField(data, "risk")), withContext(map[string]any{"id": id, "risk": stringField(data, "risk")}))
	}
	if rule.NeedsSignoff && (stringField(data, "signoff_by") == "" || stringField(data, "signoff_at") == "") {
		return tuskerError(errorSignoffMissing, fmt.Sprintf(`%s: risk "critical" requires signoff (use `+"`tusker signoff`"+`)`, id), withContext(map[string]any{"id": id}))
	}
	return nil
}

func buildEvidenceBullet(kind, link, note, date string) string {
	noteText := ""
	if note != "" {
		noteText = " — " + note
	}
	if kind == "screenshot" || kind == "video" {
		if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
			return fmt.Sprintf("- %s — %s: [view](%s)%s", date, kind, link, noteText)
		}
		return fmt.Sprintf("- %s — %s: ![[%s]]%s", date, kind, link, noteText)
	}
	if kind == "pr" {
		return fmt.Sprintf("- %s — PR: %s%s", date, link, noteText)
	}
	if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		return fmt.Sprintf("- %s — %s: [link](%s)%s", date, kind, link, noteText)
	}
	return fmt.Sprintf("- %s — %s: [[%s]]%s", date, kind, link, noteText)
}

func ensureDecisionsFile(filePath, date string) error {
	if fileExists(filePath) {
		return nil
	}
	title := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	return writeText(filePath, fmt.Sprintf("---\ntitle: \"%s\"\ntype: \"note\"\ncreated: \"%s\"\nupdated: \"%s\"\ntags:\n  - architecture\n---\n\n# %s\n\n## Decisions\n\n", title, date, date, title))
}
