package main

import (
	"strings"
	"testing"
)

func dogfoodClaimStart(t *testing.T, vault, id string) error {
	t.Helper()
	var err error
	captureStdout(t, func() {
		err = workSessionStartCmd(Args{"vault": vault, "id": id, "by": "agent:dogfood", "source": "codex", "current-workspace": "true"})
	})
	return err
}

func TestDogfoodClaimSharedCheckout(t *testing.T) {
	for _, tc := range []struct{ name, first, second, code string }{
		{"disjoint", "owned/first", "owned/second", ""},
		{"candidate unscoped", "owned/first", "", "WORK_SESSION_CHECKOUT_BUSY"},
		{"holder unscoped", "", "owned/second", "WORK_SESSION_CHECKOUT_BUSY"},
		{"both unscoped", "", "", "WORK_SESSION_CHECKOUT_BUSY"},
		{"overlap", "owned", "owned/second", "OWNED_PATH_CONFLICT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault, project := workSessionFixture(t, 2)
			setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"owned_paths": splitCSV(tc.first)})
			setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{"owned_paths": splitCSV(tc.second)})
			t.Chdir(project.RepoRoot)
			t.Setenv("CODEX_THREAD_ID", "dogfood-conversation")
			t.Setenv("TUSKER_ATTEMPT_ID", "")
			if err := dogfoodClaimStart(t, vault, "APP-T-0001"); err != nil {
				t.Fatal(err)
			}
			err := dogfoodClaimStart(t, vault, "APP-T-0002")
			if workSessionErrorCode(err) != tc.code {
				t.Fatalf("claim error = %v; want %s", err, tc.code)
			}
			if tc.code == "WORK_SESSION_CHECKOUT_BUSY" {
				issue := errorToIssue(err)
				if !strings.Contains(issue.Message, "APP-T-0001") || !(strings.Contains(issue.Message, "declares no owned paths") || strings.Contains(issue.Message, "neither declares owned paths")) {
					t.Fatalf("busy refusal = %#v", issue)
				}
				context, ok := issue.Context.(map[string]any)
				if !ok || context["task_id"] != "APP-T-0002" || context["conflicting_task_id"] != "APP-T-0001" {
					t.Fatalf("busy context = %#v", issue.Context)
				}
			}
			if tc.code == "" {
				store, err := OpenRuntimeStore(DefaultStateRoot())
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
					run, err := store.FindRunScoped(project.ProjectID, id)
					if err != nil || run == nil || run.LeaseState != string(LeaseStateClaimed) || run.WorkspacePath != project.RepoRoot {
						t.Fatalf("claim %s: %#v, %v", id, run, err)
					}
				}
			}
		})
	}
}

func TestDogfoodClaimHardDependencyReview(t *testing.T) {
	vault, project := workSessionFixture(t, 2)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "proof_status": "satisfied"})
	setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{"dependencies": []string{"APP-T-0001:hard"}})
	t.Chdir(project.RepoRoot)
	t.Setenv("CODEX_THREAD_ID", "dogfood-conversation")
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	err := dogfoodClaimStart(t, vault, "APP-T-0002")
	if err == nil {
		t.Fatal("hard dependency in review admitted")
	}
	issue := errorToIssue(err)
	context, ok := issue.Context.(map[string]any)
	if !ok {
		t.Fatalf("missing blocker: %#v", issue)
	}
	blocker, ok := context["readiness_blocker"].(ReadinessBlocker)
	if !ok || !strings.Contains(blocker.Reason, "is in review; it is a hard dependency") || !strings.Contains(blocker.Remedy, "APP-T-0001:soft") {
		t.Fatalf("dependency refusal = %#v", context)
	}
}

func TestDogfoodClaimOtherWorkspace(t *testing.T) {
	vault, project := workSessionFixture(t, 2)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:other"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project.RepoRoot)
	t.Setenv("CODEX_THREAD_ID", "dogfood-conversation")
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	if err := dogfoodClaimStart(t, vault, "APP-T-0002"); err != nil {
		t.Fatal(err)
	}
}

func TestDogfoodClaimSoftDependencySuffix(t *testing.T) {
	vault, _ := workSessionFixture(t, 2)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := validateV7TaskUpdateDependencies(idx, "APP-T-0002", []string{"APP-T-0001:soft"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0] != "APP-T-0001:soft" || parseV7DependencyEdge(deps[0]).Hardness != v7DependencyHardnessSoft {
		t.Fatalf("soft edge lost: %v", deps)
	}
}
