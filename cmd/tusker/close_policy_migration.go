package main

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type closePolicyMigrationReport struct {
	WorkflowPath string   `json:"workflow_path"`
	ConfigPath   string   `json:"config_path"`
	Changed      []string `json:"changed"`
	Write        bool     `json:"write"`
}

func migrateClosePolicyCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	// Migration is a managed write seeded from the full effective document. A
	// root-level compatibility file stays readable and untouched; it cannot be
	// silently rewritten or become a second authority source.
	resolved, err := resolveTuskerConfig(vaultPath)
	if err != nil {
		return err
	}
	configRaw, err := yaml.Marshal(resolved.Raw)
	if err != nil {
		return err
	}
	report := closePolicyMigrationReport{WorkflowPath: workflowPath(vaultPath), ConfigPath: managedTuskerConfigPath(vaultPath), Write: args.Bool("write")}
	workflowChanged, workflowText, err := migratedObjectiveWorkflow(report.WorkflowPath)
	if err != nil {
		return err
	}
	configChanged, configText, err := migratedObjectiveCloseConfigText(string(configRaw), report.ConfigPath)
	if err != nil {
		return err
	}
	if workflowChanged {
		report.Changed = append(report.Changed, report.WorkflowPath)
	}
	if configChanged {
		report.Changed = append(report.Changed, report.ConfigPath)
	}
	if report.Write {
		if workflowChanged {
			if err := writeText(report.WorkflowPath, workflowText); err != nil {
				return err
			}
		}
		if configChanged {
			if err := writeConfigTextAtomically(report.ConfigPath, configText); err != nil {
				return err
			}
		}
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	mode := "Would migrate"
	if report.Write {
		mode = "Migrated"
	}
	fmt.Printf("%s objective close policy: %d file(s) changed.\n", mode, len(report.Changed))
	return nil
}

func migratedObjectiveWorkflow(path string) (bool, string, error) {
	text, err := readText(path)
	if err != nil {
		return false, "", err
	}
	data, body, err := parseFrontmatter(text)
	if err != nil {
		return false, "", tuskerError(errorConfigInvalid, "failed to parse WORKFLOW.md: "+err.Error(), withPath(path))
	}
	reviewer, ok := data["reviewer"].(map[string]any)
	if !ok || reviewer == nil {
		return false, text, nil
	}
	changed := false
	wanted := []string{"low", "medium", "high", "critical"}
	if strings.Join(normalizeList(reviewer["auto_close_risks"]), ",") != strings.Join(wanted, ",") {
		reviewer["auto_close_risks"] = wanted
		changed = true
	}
	if _, exists := reviewer["human_required_risks"]; exists {
		delete(reviewer, "human_required_risks")
		changed = true
	}
	prompt := stringField(reviewer, "prompt")
	lowerPrompt := strings.ToLower(prompt)
	if strings.Contains(prompt, "reviewer.human_required") || strings.Contains(lowerPrompt, "human close required") || strings.Contains(lowerPrompt, "high or critical risk, leave") {
		reviewer["prompt"] = defaultReviewerPrompt()
		changed = true
	}
	if !changed {
		return false, text, nil
	}
	data["reviewer"] = reviewer
	fm, err := stringifyFrontmatter(data, nil)
	if err != nil {
		return false, "", err
	}
	return true, fm + "\n" + strings.TrimLeft(body, "\n"), nil
}

func migratedObjectiveCloseConfig(path string) (bool, string, error) {
	if !fileExists(path) {
		return false, "", nil
	}
	text, err := readText(path)
	if err != nil {
		return false, "", err
	}
	return migratedObjectiveCloseConfigText(text, path)
}

func migratedObjectiveCloseConfigText(text, path string) (bool, string, error) {
	var data map[string]any
	if err := yaml.Unmarshal([]byte(text), &data); err != nil {
		return false, "", tuskerError(errorConfigInvalid, "failed to parse project config close_policy: "+err.Error(), withPath(path))
	}
	closePolicy, ok := data["close_policy"].(map[string]any)
	if !ok || closePolicy == nil {
		return false, text, nil
	}
	changed := false
	for _, risk := range []string{"low", "medium", "high", "critical"} {
		rule, ok := closePolicy[risk].(map[string]any)
		if !ok || rule == nil {
			continue
		}
		if stringField(rule, "required_acceptor") != "reviewer_agent" {
			rule["required_acceptor"] = "reviewer_agent"
			changed = true
		}
		if _, exists := rule["required_gates"]; exists {
			delete(rule, "required_gates")
			changed = true
		}
		closePolicy[risk] = rule
	}
	if !changed {
		return false, text, nil
	}
	data["close_policy"] = closePolicy
	raw, err := yaml.Marshal(data)
	if err != nil {
		return false, "", err
	}
	return true, string(raw), nil
}
