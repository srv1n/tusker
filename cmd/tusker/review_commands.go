package main

import (
	"fmt"
	"time"
)

func reviewApproveCmd(args Args) error {
	return applyReviewVerdict(args, "approved")
}

func reviewVerifyCmd(args Args) error {
	return applyReviewVerdict(args, "requested")
}

func reviewRequestChangesCmd(args Args) error {
	return applyReviewVerdict(args, "changes_requested")
}

func reviewCommentCmd(args Args) error {
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
	summary := args.String("summary")
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — %s — review comment%s", todayISO(), by, suffixReason(summary)))
	content, err := serializeDocument(data, body, frontmatterOrderForType(stringField(data, "type")))
	if err != nil {
		return err
	}
	return writeText(note.AbsolutePath, content)
}

func applyReviewVerdict(args Args, verdict string) error {
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
	now := time.Now().UTC().Format(time.RFC3339)
	date := todayISO()
	prevReview := stringField(data, "review_state")
	if verdict == "requested" {
		if stringField(data, "status") != "in_review" {
			return tuskerError(errorInvalidTransition, fmt.Sprintf("%s: verification requires status \"in_review\"", id), withContext(map[string]any{"id": id, "status": stringField(data, "status")}))
		}
		if prevReview != "verification_requested" && prevReview != "requested" {
			return tuskerError(errorInvalidTransition, fmt.Sprintf("%s: verify expects review_state \"verification_requested\" or \"requested\", got %q", id, prevReview), withContext(map[string]any{"id": id, "review_state": prevReview}))
		}
		data["review_state"] = "requested"
		data["verified_by"] = by
		data["verified_at"] = now
		data["updated"] = date
		appendTransition(data, orderedTransition(now, "review", prevReview, "requested", by, fallback(args.String("summary"), "verification passed")))
		body = appendWorkLogBullet(body, fmt.Sprintf("%s — %s — verification passed%s", date, by, suffixReason(args.String("summary"))))
		content, err := serializeDocument(data, body, frontmatterOrderForType(stringField(data, "type")))
		if err != nil {
			return err
		}
		if err := writeText(note.AbsolutePath, content); err != nil {
			return err
		}
		autoReindex(vaultPath)
		return nil
	}
	if verdict == "approved" {
		if stringField(data, "status") != "in_review" {
			return tuskerError(errorInvalidTransition, fmt.Sprintf("%s: approve requires status \"in_review\"", id), withContext(map[string]any{"id": id, "status": stringField(data, "status")}))
		}
		if prevReview != "requested" && prevReview != "approved" {
			return tuskerError(errorInvalidTransition, fmt.Sprintf("%s: approve expects review_state \"requested\" or \"approved\", got %q", id, prevReview), withContext(map[string]any{"id": id, "review_state": prevReview}))
		}
	}
	data["review_state"] = verdict
	data["reviewed_by"] = by
	data["reviewed_at"] = now
	if verdict == "changes_requested" {
		data["status"] = "rework"
		data["work_revision"] = intField(data, "work_revision") + 1
	} else {
		if stringField(data, "status") == "in_review" {
			data["status"] = "merging"
		}
	}
	data["updated"] = date
	appendTransition(data, orderedTransition(now, "review", prevReview, verdict, by, args.String("summary")))
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — %s — review %s%s", date, by, verdict, suffixReason(args.String("summary"))))
	content, err := serializeDocument(data, body, frontmatterOrderForType(stringField(data, "type")))
	if err != nil {
		return err
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		return err
	}
	autoReindex(vaultPath)
	return nil
}
