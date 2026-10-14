package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

const docsFreshnessBegin = "<!-- tusker:docs-freshness:begin -->"

func statusCmd(args Args) error {
	if firstNonEmpty(args.String("id"), args.String("_pos0"), args.String("status"), args.String("_pos1")) == "" {
		vaultPath, err := resolveVaultPath(args, false)
		if err != nil {
			return err
		}
		args["vault"] = vaultPath
		if !args.Bool("json") {
			fmt.Printf("Vault: %s\n", vaultPath)
		}
		return listCmd(args)
	}
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	status := firstNonEmpty(args.String("status"), args.String("_pos1"))
	if args.String("id") == "" && args.String("status") == "" {
		first := strings.ToLower(args.String("_pos0"))
		second := strings.ToUpper(args.String("_pos1"))
		if _, ok := v7TaskStatuses[first]; ok && v7TaskIDPattern.MatchString(second) {
			id = second
			status = first
		}
	}
	args["id"] = id
	args["status"] = status
	return statusV7Cmd(args)
}

func evidenceCmd(args Args) error {
	switch strings.ToLower(args.String("_pos0")) {
	case "add":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return evidenceV7AddCmd(args)
	case "promote":
		return evidenceV7PromoteCmd(args)
	case "prune":
		return evidenceV7PruneCmd(args)
	default:
		return tuskerError(errorMissingArg, "evidence requires add, promote, or prune")
	}
}

func firstNonEmptyList(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func validateKnowledgeNodePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "must not be empty"
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return "must not start or end with /"
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return `must not contain empty, ".", or ".." segments`
		}
		if strings.ContainsAny(segment, `\:`) {
			return "must use portable path segments"
		}
	}
	return ""
}

func mapField(data map[string]any, key string) map[string]any {
	switch v := data[key].(type) {
	case map[string]any:
		return v
	case map[any]any:
		out := map[string]any{}
		for key, value := range v {
			out[toString(key)] = value
		}
		return out
	default:
		return nil
	}
}

func hasV7ProjectSkill(vaultPath string) bool {
	return fileExists(filepath.Join(vaultPath, "SKILL.md"))
}

func hasV7KnowledgeDomains(vaultPath string) bool {
	return dirExists(filepath.Join(vaultPath, "knowledge", "domains"))
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
