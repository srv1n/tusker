package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	command, args := parseCLI(os.Args)
	exitCode, err := run(command, args)
	if err != nil {
		issue := errorToIssue(err)
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": false, "error": issue})
		} else {
			loc := ""
			if issue.Path != "" {
				loc = issue.Path + ": "
			}
			fmt.Fprintf(os.Stderr, "[%s] %s%s\n", issue.Code, loc, issue.Message)
			if issue.Hint != "" {
				fmt.Fprintf(os.Stderr, "  hint: %s\n", issue.Hint)
			}
		}
		if exitCode == 0 {
			exitCode = 1
		}
		os.Exit(exitCode)
	}
	os.Exit(exitCode)
}

func emitJSON(payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"ok":false,"error":{"code":"UNKNOWN","message":%q}}`+"\n", err.Error())
		return
	}
	fmt.Printf("%s\n", encoded)
}
