package main

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
)

// cliCommand is one row of the command table in commands_table.go. That table
// is the only list of commands: dispatch, help, unknown-command suggestions,
// capabilities, and daemon notification all read it. Add a command there and
// nowhere else.
type cliCommand struct {
	Name    string // full path as typed, e.g. "wave start"
	Group   string // one of cliGroups
	Summary string // what it does, one line
	Use     string // "Use when ..." — the situation that calls for it
	Avoid   string // "Not for ..." — the confusable neighbour and its command
	Usage   string // usage lines, when no Ref printer covers this command
	Ref     func() // longer reference: flags, behavior, examples
	Run     func(Args) (int, error)
	Mutates bool // nudge the daemon after a successful run
	Hidden  bool // plumbing or retiring: dispatched, never listed
}

type cliGroup struct{ Name, About string }

var cliGroups = []cliGroup{
	{"setup", "install Tusker, create the repo vault, check and repair the machine"},
	{"find", "read, search, and diagnose tasks, waves, and the vault"},
	{"author", "create and edit task contracts, proof rows, gates, and waves"},
	{"start", "authorize and queue work for background agents"},
	{"work", "your own session on a task: claim, progress, submit, review"},
	{"runs", "inspect and steer background runs, executions, and messages"},
	{"deliver", "close, accept, land, and hand work back to a human"},
	{"docs", "project documentation, specs, and skills"},
	{"models", "runner harnesses, model profiles, and tier routing"},
	{"daemon", "the resident daemon, registered projects, and automation"},
	{"maintain", "cleanup, migration, audit, and the demo project"},
	{"internal", "plumbing and retiring commands; not for normal use"},
}

var (
	cliCommands []cliCommand
	cliIndex    map[string]*cliCommand
)

func init() {
	cliCommands = commandRows()
	cliIndex = make(map[string]*cliCommand, len(cliCommands))
	for i := range cliCommands {
		cliIndex[cliCommands[i].Name] = &cliCommands[i]
	}
}

func lookupCommand(name string) *cliCommand { return cliIndex[name] }

func commandChildren(family string) []*cliCommand {
	var out []*cliCommand
	for i := range cliCommands {
		if strings.HasPrefix(cliCommands[i].Name, family+" ") {
			out = append(out, &cliCommands[i])
		}
	}
	return out
}

// run0 adapts the common handler shape that reports only an error.
func run0(f func(Args) error) func(Args) (int, error) {
	return func(args Args) (int, error) { return 0, f(args) }
}

// runID adapts a handler that takes its record id as the first positional.
func runID(f func(Args) error) func(Args) (int, error) {
	return func(args Args) (int, error) {
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, f(args)
	}
}

func cliCommandMutatesVault(command string) bool {
	c := lookupCommand(command)
	return c != nil && c.Mutates
}

func runInner(command string, args Args) (int, error) {
	switch command {
	case "", "help", "--help", "-h":
		target := strings.TrimPrefix(strings.Join(strings.Fields(strings.ReplaceAll(args.String("_pos"), "\n", " ")), " "), "tusker ")
		if target == "" && args.Bool("full") {
			for i := range cliCommands {
				if args.Bool("all") || !cliCommands[i].Hidden {
					printCommandBlock(&cliCommands[i], true)
				}
			}
			return 0, nil
		}
		if target == "" {
			printHelp(args.Bool("all"))
			return 0, nil
		}
		if printCommandHelp(target) {
			return 0, nil
		}
		return unknownCommand(target, args)
	case "--version":
		command = "version"
	}
	if args.Bool("help") {
		if printCommandHelp(command) {
			return 0, nil
		}
		return unknownCommand(command, args)
	}
	c := lookupCommand(command)
	if c == nil {
		if len(commandChildren(command)) > 0 {
			if sub := args.String("_pos0"); sub != "" {
				return unknownCommand(command+" "+sub, args)
			}
			printCommandHelp(command)
			return 0, nil
		}
		return unknownCommand(command, args)
	}
	if c.Run == nil {
		printCommandHelp(command)
		return 0, nil
	}
	return c.Run(args)
}

// removedCommands maps retired command names to what replaces them, so an
// agent working from stale instructions gets a direct answer.
var removedCommands = map[string]string{
	"claim":              "tusker work start <TASK-ID> --by <agent>",
	"heartbeat":          "tusker work heartbeat <TASK-ID> --by <agent>",
	"release":            "tusker work release <TASK-ID> --by <agent> --reason <text>",
	"handoff":            "tusker finish <TASK-ID> --request-review",
	"attempt":            "tusker work start <TASK-ID> --by <agent>, then tusker finish <TASK-ID> --request-review",
	"relaunch":           "tusker reset",
	"propose":            "tusker proposal",
	"runner conformance": "tusker runner test",
	"models catalog":     "tusker runner catalog",
	"runs redrive":       "tusker redrive",
	"runs claim":         "tusker work start (your own session); the daemon claims background runs itself",
	"runs start":         "tusker work start",
	"runs heartbeat":     "tusker work heartbeat",
	"runs submit":        "tusker work submit",
	"runs fail":          "tusker work fail",
	"runs reclaim":       "tusker runs release or tusker redrive",
	"streams":            "tusker runs list --active",
	"factory":            "tusker digest, or the Serve UI operations view",
	"context":            "nothing; the Codex transcript audit was removed",
	"work cancel":        "tusker work release <TASK-ID> --by <agent> --reason <text>",
	"update":             "tusker install (same flags; refreshes skills and, with --bin, the PATH link)",
	"worker":             "nothing; external worker coordination commands were removed",
	"dashboard":          "tusker reindex (rebuilds the generated dashboards)",
	"state":              "nothing; the state-branch lease sync was removed",
	"skill pack":         "tusker packet <TASK-ID> --for agent",
}

func unknownCommand(command string, args Args) (int, error) {
	replacement, removed := removedCommands[command]
	if !removed {
		replacement, removed = removedCommands[strings.Fields(command + " x")[0]]
	}
	if removed {
		return 1, tuskerError(errorInvalidArg, "tusker "+command+" was removed", withHint("use "+replacement))
	}
	matches := suggestCommands(command)
	hint := "run `tusker help` for the grouped list, or `tusker help | grep <word>`"
	if len(matches) > 0 {
		hint = "did you mean: tusker " + strings.Join(matches, ", tusker ") + "; " + hint
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": false, "error": errorToIssue(tuskerError(errorInvalidArg, "unknown command: "+command, withHint(hint)))})
		return 1, nil
	}
	fmt.Fprintf(os.Stderr, "Unknown command: tusker %s\n", command)
	for _, m := range matches {
		if c := lookupCommand(m); c != nil {
			fmt.Fprintf(os.Stderr, "  did you mean: tusker %-24s %s\n", m, c.Summary)
		} else {
			fmt.Fprintf(os.Stderr, "  did you mean: tusker %s ...\n", m)
		}
	}
	fmt.Fprintln(os.Stderr, "List every command: tusker help    Search: tusker help | grep <word>")
	return 1, nil
}

// suggestCommands ranks visible commands that share a word with the typed
// command or sit within a small edit distance of it.
func suggestCommands(typed string) []string {
	words := strings.Fields(strings.ToLower(typed))
	type scored struct {
		name  string
		score int
	}
	var hits []scored
	for _, c := range cliCommands {
		if c.Hidden {
			continue
		}
		score := 0
		if d := editDistance(strings.ToLower(typed), c.Name); d <= 2 {
			score += 10 - d
		}
		for _, w := range words {
			if len(w) < 3 {
				continue
			}
			for _, part := range strings.Fields(c.Name) {
				if part == w {
					score += 4
				} else if strings.Contains(part, w) || (len(w) >= 4 && editDistance(part, w) <= 1) {
					score += 2
				}
			}
		}
		if score > 0 {
			hits = append(hits, scored{c.Name, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	var out []string
	for i := 0; i < len(hits) && i < 6; i++ {
		out = append(out, hits[i].name)
	}
	return out
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func printHelp(all bool) {
	fmt.Println(`Tusker - repo-local work tracking: task contracts, proof, review, and background agent runs.

Find a command:   tusker help | grep <word>
Search guidance:  tusker help --full | grep -B3 <word>   (every use/avoid line)
Command detail:   tusker help <command>      (same as: tusker <command> --help)
Every line below is "<group>  tusker <command>  <summary>", so grep output is
runnable. Grep a group name (e.g. "^work ") to list that group.

Groups:`)
	for _, g := range cliGroups {
		if g.Name == "internal" && !all {
			continue
		}
		fmt.Printf("  %-9s %s\n", g.Name, g.About)
	}
	fmt.Println()
	for _, g := range cliGroups {
		if g.Name == "internal" && !all {
			continue
		}
		for _, c := range cliCommands {
			if c.Group == g.Name && (all || !c.Hidden) {
				fmt.Printf("%-9s tusker %-26s %s\n", g.Name, c.Name, c.Summary)
			}
		}
	}
	fmt.Println(`
Vault: tusker walks up from the working directory to the nearest .tusker/
vault; pass --vault <path> to choose one, or --use-project-vault from a linked
worktree. Global flag: --json emits machine-readable success and error output.`)
	if !all {
		fmt.Println("Internal and retiring commands: tusker help --all")
	}
}

// printCommandHelp renders help for one command or a command family from the
// table. It never opens a vault or writes state.
func printCommandHelp(name string) bool {
	name = strings.TrimSpace(strings.TrimPrefix(name, "tusker "))
	c, children := lookupCommand(name), commandChildren(name)
	if c == nil && len(children) == 0 {
		return false
	}
	var all []*cliCommand
	if c != nil {
		printCommandBlock(c, true)
		all = append(all, c)
	}
	if len(children) > 0 {
		if c != nil {
			fmt.Println()
		}
		fmt.Printf("tusker %s has %d subcommands. Detail: tusker help %s <subcommand>\n\n", name, len(children), name)
		for _, child := range children {
			printCommandBlock(child, false)
			fmt.Println()
		}
		all = append(all, children...)
	}
	printRefs(all)
	return true
}

func printCommandBlock(c *cliCommand, full bool) {
	fmt.Printf("tusker %s — %s\n", c.Name, c.Summary)
	if full {
		fmt.Printf("  group: %s\n", c.Group)
	}
	if c.Use != "" {
		fmt.Printf("  use:   %s\n", c.Use)
	}
	if c.Avoid != "" {
		fmt.Printf("  avoid: %s\n", c.Avoid)
	}
	if c.Usage != "" {
		fmt.Println("  usage:")
		for _, line := range strings.Split(c.Usage, "\n") {
			fmt.Printf("    %s\n", line)
		}
	}
}

// printRefs prints each distinct reference printer once; most families share
// one printer across their subcommands.
func printRefs(cmds []*cliCommand) {
	seen := map[uintptr]bool{}
	for _, c := range cmds {
		if c.Ref == nil {
			continue
		}
		ptr := reflect.ValueOf(c.Ref).Pointer()
		if seen[ptr] {
			continue
		}
		seen[ptr] = true
		fmt.Println("\nReference:")
		c.Ref()
	}
}
