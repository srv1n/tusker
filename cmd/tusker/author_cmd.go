package main

import (
	"strconv"
	"strings"
)

// newCmd creates any record: `tusker new <kind> ...`. The kind is the first
// positional; the rest of the arguments go to that kind's creator unchanged.
func newCmd(args Args) error {
	kind := strings.ToLower(strings.TrimSpace(args.String("_pos0")))
	rest := shiftV7WaveArgs(args, 1)
	switch kind {
	case "epic":
		return newV7Epic(rest)
	case "task":
		return newAuthoredV7Task(rest)
	case "gate":
		return newV7Gate(rest)
	case "decision":
		return newV7Decision(rest)
	case "wave":
		return waveV7CreateCmd(rest)
	case "batch":
		return coordinatorBatchCreateCmd(rest)
	case "flaky":
		return coordinatorFlakyCmd(rest, "add")
	case "doc":
		return docsCmd("new", rest)
	case "fork":
		if err := coordinatorForkCmd(rest, "add"); err != nil {
			return err
		}
		if old := strings.TrimSpace(rest.String("supersedes")); old != "" {
			return coordinatorForkCmd(Args{"_pos0": old, "_pos": old, "epic": rest.String("epic"), "by": rest.String("id"), "vault": rest.String("vault")}, "supersede")
		}
		return nil
	}
	return tuskerError(errorMissingArg, "Usage: tusker new epic|task|gate|decision|wave|batch|fork|flaky|doc ...", withHint("run `tusker help new` for each kind's flags"))
}

// taskUpdateFlags are the contract fields `tusker edit` passes to task update.
var taskUpdateFlags = []string{
	"body-file", "title", "work-level", "review-level", "review-reason", "spec-refs", "dependencies",
	"rebind-contract", "rebind-dependency-contracts", "owned-paths", "weight", "generated-outputs",
	"execute-profile", "clear-execute-profile", "review-profile", "clear-review-profile",
}

// editCmd changes any existing record: `tusker edit <ID> ...`. A task takes
// contract fields, --verify rows, --proof-mode, and --status; a wave takes
// --add, --remove, and --outcome; a gate takes --status
// satisfied|waived|obsolete. Content changes apply before the status change.
func editCmd(args Args) error {
	id := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if id == "" {
		return tuskerError(errorMissingArg, "Usage: tusker edit <ID> [flags]", withHint("run `tusker help edit`"))
	}
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	note, err := resolveNote(vault, id)
	if err != nil {
		return err
	}
	args["id"] = id
	switch daemonNoteKind(note) {
	case "task":
		return editTask(args, id)
	case "wave":
		return editWave(args, id)
	case "gate":
		return editGate(args, id)
	}
	return tuskerError(errorInvalidArg, "tusker edit supports tasks, waves, and gates; "+id+" is a "+daemonNoteKind(note))
}

func editTask(args Args, id string) error {
	did := false
	for _, flag := range taskUpdateFlags {
		if _, ok := args[flag]; ok {
			if err := taskUpdateCLICmd(withPositionals(args, id)); err != nil {
				return err
			}
			did = true
			break
		}
	}
	if rows := args.String("verify"); rows != "" || args.String("check") != "" || args.String("batch-file") != "" {
		verifyArgs := withPositionals(args, "add", id)
		if rows != "" {
			verifyArgs["rows"] = rows
		}
		if err := verifyV7AddCmd(verifyArgs); err != nil {
			return err
		}
		did = true
	}
	if index := args.String("verify-remove"); index != "" {
		if err := verifyV7RemoveCmd(withPositionals(args, "remove", id).with("index", index)); err != nil {
			return err
		}
		did = true
	}
	if mode := args.String("proof-mode"); mode != "" {
		if err := proofV7SetModeCmd(withPositionals(args, "set-mode", id).with("mode", mode)); err != nil {
			return err
		}
		did = true
	}
	if status := args.String("status"); status != "" {
		if err := statusCmd(withPositionals(args).with("status", status)); err != nil {
			return err
		}
		did = true
	}
	if !did {
		return tuskerError(errorMissingArg, "tusker edit "+id+" changed nothing", withHint("pass contract flags (--title, --body-file, ...), --verify <row>, --verify-remove <n>, --proof-mode <mode>, or --status <status>"))
	}
	return nil
}

func editWave(args Args, id string) error {
	did := false
	if add := args.String("add"); add != "" {
		if err := waveV7AddCmd(withPositionals(args, append([]string{id}, splitCSV(add)...)...)); err != nil {
			return err
		}
		did = true
	}
	if remove := args.String("remove"); remove != "" {
		if err := waveV7RemoveCmd(withPositionals(args, append([]string{id}, splitCSV(remove)...)...)); err != nil {
			return err
		}
		did = true
	}
	if outcome := args.String("outcome"); outcome != "" {
		if err := waveV7OutcomeCmd(withPositionals(args, id).with("summary", outcome)); err != nil {
			return err
		}
		did = true
	}
	if !did {
		return tuskerError(errorMissingArg, "tusker edit "+id+" changed nothing", withHint("pass --add <TASK-IDs>, --remove <TASK-IDs>, or --outcome <text>"))
	}
	return nil
}

func editGate(args Args, id string) error {
	switch status := args.String("status"); status {
	case "satisfied", "waived", "obsolete":
		return gateV7Transition(args, status)
	default:
		return tuskerError(errorInvalidArg, "tusker edit "+id+" needs --status satisfied|waived|obsolete", withHint("satisfied takes --evidence <summary>; waived and obsolete take --reason <text>"))
	}
}

// withPositionals copies args with new positionals, dropping the caller's.
func withPositionals(args Args, positionals ...string) Args {
	out := Args{}
	for key, value := range args {
		if !strings.HasPrefix(key, "_pos") {
			out[key] = value
		}
	}
	if len(positionals) > 0 {
		out["_pos"] = strings.Join(positionals, "\n")
		for i, value := range positionals {
			out["_pos"+strconv.Itoa(i)] = value
		}
	}
	return out
}

func (a Args) with(key, value string) Args {
	a[key] = value
	return a
}
