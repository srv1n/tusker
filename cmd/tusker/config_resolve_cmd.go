package main

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func configResolveCmd(args Args) error {
	key := firstNonEmpty(strings.TrimSpace(args.String("key")), strings.TrimSpace(args.String("_pos0")))
	if key == "" {
		return tuskerError(errorInvalidArg, "config resolve requires a key", withHint("example: tusker config resolve runtime.max_active_runs_per_project"))
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	report, err := configResolve(vaultPath, key)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "resolution": report})
		return nil
	}
	printConfigResolve(report)
	return nil
}

func printConfigResolve(report configResolveReport) {
	fmt.Printf("%s\n", report.Key)
	if report.Lookup != report.Key {
		fmt.Printf("lookup: %s\n", report.Lookup)
	}
	fmt.Printf("effective: %s\n", oneLineConfigValue(report.Value))
	fmt.Printf("source: %s", report.Source)
	if strings.TrimSpace(report.Path) != "" {
		fmt.Printf(" (%s)", report.Path)
	}
	fmt.Println()
	fmt.Println("sources:")
	for _, source := range report.Sources {
		status := "losing"
		if source.Winning {
			status = "winning"
		}
		present := "unset"
		if source.Present {
			present = oneLineConfigValue(source.Value)
		}
		line := fmt.Sprintf("  - %s [%s]", source.Source, status)
		if strings.TrimSpace(source.Path) != "" {
			line += " " + source.Path
		}
		line += ": " + present
		if strings.TrimSpace(source.Note) != "" {
			line += " (" + source.Note + ")"
		}
		fmt.Println(line)
	}
}

func oneLineConfigValue(value any) string {
	raw, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	text := strings.TrimSpace(string(raw))
	text = strings.ReplaceAll(text, "\n", " ")
	for strings.Contains(text, "  ") {
		text = strings.ReplaceAll(text, "  ", " ")
	}
	return text
}
