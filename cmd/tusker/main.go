package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

func main() {
	command, args := parseCLI(os.Args)
	exitCode, err := run(command, args)
	if err != nil {
		reportCLIError(err, args.Bool("json"))
		if exitCode == 0 {
			exitCode = 1
		}
		os.Exit(exitCode)
	}
	os.Exit(exitCode)
}

// resultEmittedError marks a failure whose command already wrote its JSON
// result document, so --json mode must not append a second error envelope.
type resultEmittedError struct{ err error }

func (e resultEmittedError) Error() string { return e.err.Error() }
func (e resultEmittedError) Unwrap() error { return e.err }

// afterResultEmitted wraps err (if any) as already reported by a JSON result.
func afterResultEmitted(err error) error {
	if err == nil {
		return nil
	}
	return resultEmittedError{err: err}
}

func reportCLIError(err error, jsonMode bool) {
	issue := errorToIssue(err)
	if jsonMode {
		var emitted resultEmittedError
		if !errors.As(err, &emitted) {
			emitJSON(map[string]any{"ok": false, "error": issue})
		}
		return
	}
	loc := ""
	if issue.Path != "" {
		loc = issue.Path + ": "
	}
	fmt.Fprintf(os.Stderr, "[%s] %s%s\n", issue.Code, loc, issue.Message)
	if issue.Hint != "" {
		fmt.Fprintf(os.Stderr, "  hint: %s\n", issue.Hint)
	}
}

func emitJSON(payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"ok":false,"error":{"code":"UNKNOWN","message":%q}}`+"\n", err.Error())
		return
	}
	fmt.Printf("%s\n", encoded)
}
