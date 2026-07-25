package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestScheduledPromotionPolicyDefaultsAndMigrationAreDisabled(t *testing.T) {
	fresh := defaultWorkflow()
	if got := fresh.ScheduledPromotion.Effective; got.Mode != scheduledPromotionDisabled || got.Observe || got.Stage || got.Promote || got.Release || got.ModelTriage || got.Provenance != "fresh default" {
		t.Fatalf("fresh policy granted authority: %#v", got)
	}

	vault := t.TempDir()
	data, body, err := parseFrontmatter(defaultWorkflowMarkdown())
	if err != nil {
		t.Fatal(err)
	}
	delete(data, "scheduled_promotion")
	migrated, err := serializeDocument(data, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), migrated); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Data.ScheduledPromotion.Effective; got.Configured || got.Mode != scheduledPromotionDisabled || got.Observe || got.Stage || got.Promote || got.Release || got.ModelTriage || got.Provenance != "migration default (scheduled_promotion absent)" {
		t.Fatalf("absent migration policy was not harmless: %#v", got)
	}
}

func TestScheduledPromotionPolicyCapabilityMatrixAndProvenance(t *testing.T) {
	for _, tc := range []struct {
		mode                    string
		observe, stage, promote bool
	}{
		{scheduledPromotionDisabled, false, false, false},
		{scheduledPromotionShadow, true, false, false},
		{scheduledPromotionStage, true, true, false},
		{scheduledPromotionPromote, true, true, true},
	} {
		policy := defaultScheduledPromotionPolicy()
		policy.Mode = tc.mode
		projection := scheduledPromotionProjection(policy, true, "workflow")
		if projection.Observe != tc.observe || projection.Stage != tc.stage || projection.Promote != tc.promote || projection.Release || projection.ModelTriage || projection.Provenance != "workflow" {
			t.Fatalf("%s projection: %#v", tc.mode, projection)
		}
	}

	policy := defaultScheduledPromotionPolicy()
	policy.Mode = scheduledPromotionPromote
	policy.Release = ScheduledPromotionRelease{Profile: "production", Authorized: true}
	policy.ModelTriage.Authorized = true
	projection := scheduledPromotionProjection(policy, true, "workflow")
	if !projection.Release || !projection.ModelTriage {
		t.Fatalf("explicit separate authorities not projected: %#v", projection)
	}
}

func TestScheduledPromotionPolicyValidationRemedies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "WORKFLOW.md")
	for _, tc := range []struct {
		name   string
		mutate func(*ScheduledPromotionPolicy)
		want   string
	}{
		{"version", func(p *ScheduledPromotionPolicy) { p.Version = 2 }, "scheduled_promotion.version must be 1"},
		{"mode", func(p *ScheduledPromotionPolicy) { p.Mode = "run_wild" }, "scheduled_promotion.mode must be disabled, shadow, stage, or promote"},
		{"release_pair", func(p *ScheduledPromotionPolicy) {
			p.Mode = scheduledPromotionPromote
			p.Release.Profile = "production"
		}, "scheduled_promotion.release.profile and scheduled_promotion.release.authorized must be set together"},
		{"release_mode", func(p *ScheduledPromotionPolicy) {
			p.Release = ScheduledPromotionRelease{Profile: "production", Authorized: true}
		}, "scheduled_promotion release and paid model triage require mode promote"},
		{"triage_mode", func(p *ScheduledPromotionPolicy) { p.ModelTriage.Authorized = true }, "scheduled_promotion release and paid model triage require mode promote"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := defaultScheduledPromotionPolicy()
			tc.mutate(&policy)
			err := validateScheduledPromotionPolicy(policy, path)
			typed, ok := err.(*TuskerError)
			if !ok || !strings.Contains(typed.Message, tc.want) || typed.Hint == "" {
				t.Fatalf("want stable validation plus remedy %q, got %v", tc.want, err)
			}
		})
	}
}
