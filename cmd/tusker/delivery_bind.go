package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// delivery bind is intentionally a narrow bridge for pre-existing held tasks:
// it supplies identity to the normal atomic import instead of maintaining a
// second task-contract writer.
func deliveryBindCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if !args.Bool("dry-run") {
		if err := ensureV7ControlMutation(vaultPath, args); err != nil {
			return err
		}
	}
	planPath := strings.TrimSpace(firstNonEmpty(args.String("plan"), args.String("_pos0")))
	if planPath == "" {
		return tuskerError(errorMissingArg, "Usage: tusker delivery bind --plan <plan.yaml> --task <source_key=task-ID> [--task <source_key=task-ID>] [--dry-run]")
	}
	if !filepath.IsAbs(planPath) {
		planPath = filepath.Join(v7RepoRoot(vaultPath), planPath)
	}
	if schema, err := deliveryPlanSchemaAt(planPath); err != nil {
		return err
	} else if schema != deliveryPlanV2Schema {
		return tuskerError(errorInvalidArg, "unsupported delivery plan schema: "+fallback(schema, "<missing>")+"; expected "+deliveryPlanV2Schema)
	}
	bindings, err := deliveryBindMappings(args.String("task"))
	if err != nil {
		return tuskerError(errorInvalidArg, err.Error())
	}
	internal := copyArgsForInternalMutation(args)
	internal["delivery-bindings"] = deliveryEncodeBindMappings(bindings)
	return deliveryV2ImportCmd(vaultPath, planPath, internal)
}

func deliveryBindMappings(raw string) (map[string]string, error) {
	bindings := map[string]string{}
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' }) {
		key, id, ok := strings.Cut(strings.TrimSpace(item), "=")
		key, id = strings.TrimSpace(key), strings.TrimSpace(id)
		if !ok || key == "" || id == "" || bindings[key] != "" {
			return nil, fmt.Errorf("each --task must be one unique source_key=task-ID mapping")
		}
		bindings[key] = id
	}
	if len(bindings) == 0 {
		return nil, fmt.Errorf("delivery bind requires at least one --task source_key=task-ID mapping")
	}
	return bindings, nil
}

func deliveryEncodeBindMappings(bindings map[string]string) string {
	keys := make([]string, 0, len(bindings))
	for key := range bindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]string, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, key+"="+bindings[key])
	}
	return strings.Join(rows, "\n")
}
