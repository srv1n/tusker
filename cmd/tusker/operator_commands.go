package main

import (
	"fmt"
	"strings"
	"time"
)

// Routers for the collapsed operator surface (daemon, projects, automation,
// models, runs, message). Each one only picks an existing handler; the
// handlers keep their logic and are still called directly by Serve.

// shiftPositionals drops the leading verb so the handler sees its own
// positionals from _pos0.
func shiftPositionals(args Args) Args {
	out := Args{}
	var rest []string
	for key, value := range args {
		if !strings.HasPrefix(key, "_pos") {
			out[key] = value
		}
	}
	for i := 1; ; i++ {
		value, ok := args[fmt.Sprintf("_pos%d", i)]
		if !ok {
			break
		}
		rest = append(rest, value)
	}
	if len(rest) > 0 {
		out["_pos"] = strings.Join(rest, "\n")
		for i, value := range rest {
			out[fmt.Sprintf("_pos%d", i)] = value
		}
	}
	return out
}

func unknownOperatorVerb(family, verb, valid string) error {
	return tuskerError(errorInvalidArg, "unknown "+family+" subcommand: "+verb, withHint("use `tusker "+family+" "+valid+"`; `tusker help "+family+"` lists them"))
}

// daemonCmd: bare or `status` reports; --limit/--disk-pressure-* change the
// global caps; --resume closes tripped circuits. `daemon service <action>`
// stays reachable for the macOS app installer scripts.
func daemonCmd(args Args) error {
	switch verb := strings.ToLower(args.String("_pos0")); verb {
	case "", "status":
	case "service":
		return daemonServiceCmd(shiftPositionals(args))
	default:
		return unknownOperatorVerb("daemon", verb, "status|run|stop|install")
	}
	if args.Bool("resume") {
		return daemonResumeCmd(args)
	}
	if limit := strings.TrimSpace(args.String("limit")); limit != "" {
		args["max-active-runs"] = limit
	}
	for _, key := range []string{"max-active-runs", "disk-pressure-enabled", "disk-pressure-min-free-bytes", "disk-pressure-min-free-percent"} {
		if _, ok := args[key]; ok {
			return daemonLimitsCmd(args)
		}
	}
	return daemonStatusCmd(args)
}

// daemonRunRoute: --once is one poll with no dispatch, and works while the
// resident daemon holds the guard.
func daemonRunRoute(args Args) error {
	if args.Bool("once") {
		return refreshCmd(args)
	}
	return daemonRunCmd(args)
}

func daemonInstallRoute(args Args) error {
	action := "install"
	if args.Bool("remove") {
		action = "uninstall"
	}
	args["_pos0"] = action
	return daemonServiceCmd(args)
}

func projectsCmd(args Args) error {
	switch verb := strings.ToLower(args.String("_pos0")); verb {
	case "", "list":
		return projectsListCmd(args)
	default:
		return unknownOperatorVerb("projects", verb, "list|add|set|remove")
	}
}

// projectsAddRoute: --rebind <project-id> points an existing disabled
// project at the --repo/--vault given instead of registering a new one.
func projectsAddRoute(args Args) error {
	rebind, ok := args["rebind"]
	if !ok {
		return projectsAddCmd(args)
	}
	if rebind != "true" {
		args["id"] = rebind
	}
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	return projectsRebindCmd(args)
}

// projectsSetCmd: --on|--off toggle background work, --limit N sets the
// per-project cap, --dry-run alone (or with --on/--off) previews the resume
// scope and toggle history without changing anything.
func projectsSetCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	on, off := args.Bool("on"), args.Bool("off")
	limit := strings.TrimSpace(args.String("limit"))
	if on && off {
		return tuskerError(errorInvalidArg, "projects set takes --on or --off, not both")
	}
	if args.Bool("dry-run") {
		return projectsAutomationScopeCmd(args)
	}
	if !on && !off && limit == "" {
		return tuskerError(errorMissingArg, "projects set needs --on, --off, --limit <n>, or --dry-run", withHint("tusker projects set <project-id> --on|--off|--limit <n> [--dry-run]"))
	}
	if limit != "" {
		args["max-active-runs"] = limit
		if err := projectsLimitsCmd(args); err != nil {
			return err
		}
	}
	if !on && !off {
		return nil
	}
	if err := setProjectEnabledCmd(args, on); err != nil {
		return err
	}
	// The command table cannot name this row in the registry-mutation list,
	// so wake registry reconciliation here like enable/disable did.
	_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_registry"}, 250*time.Millisecond)
	return nil
}

// projectsRemoveRoute: --missing previews dead registrations (--apply removes).
func projectsRemoveRoute(args Args) error {
	if args.Bool("missing") {
		return projectsPruneCmd(args)
	}
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	return projectsRemoveCmd(args)
}

func automationCmd(args Args) error {
	switch verb := strings.ToLower(args.String("_pos0")); verb {
	case "", "status":
		return automationStatusCmd(args)
	case "on":
		return automationOnCmd(args)
	case "off":
		return automationOffCmd(args)
	default:
		return unknownOperatorVerb("automation", verb, "[on|off]|plan|dispatch")
	}
}

func modelsRoute(args Args) error {
	switch verb := strings.ToLower(args.String("_pos0")); verb {
	case "", "show":
		return modelsCmd(args)
	default:
		return unknownOperatorVerb("models", verb, "[show]|set|profile")
	}
}

// modelsSetRoute: `models set <level> <lane> <profiles>` or `--clear` to drop
// the override; the --level/--lane/--profiles flags still work.
func modelsSetRoute(args Args) error {
	for i, key := range []string{"level", "lane", "profiles"} {
		if args.String(key) == "" {
			if value := args.String(fmt.Sprintf("_pos%d", i)); value != "" {
				args[key] = value
			}
		}
	}
	if args.Bool("clear") {
		return modelsResetCmd(args)
	}
	return modelsSetCmd(args)
}

// modelsProfileRoute: `models profile <name>` creates or updates a profile;
// --disable, --enable, --remove change its lifecycle; --defaults previews
// (or --write) generated default profiles.
func modelsProfileRoute(args Args) error {
	if args.Bool("defaults") {
		return runnerProfilesBootstrapCmd(args)
	}
	args["name"] = firstNonEmpty(args.String("name"), args.String("_pos0"))
	action := ""
	for _, flag := range []string{"disable", "enable", "remove"} {
		if args.Bool(flag) {
			if action != "" {
				return tuskerError(errorInvalidArg, "models profile takes one of --set, --disable, --enable, --remove")
			}
			action = "profile-" + flag
		}
	}
	if action == "" {
		return modelsProfileSetCmd(args)
	}
	if args.Bool("set") {
		return tuskerError(errorInvalidArg, "models profile takes one of --set, --disable, --enable, --remove")
	}
	return modelsProfileLifecycleCmd(args, action)
}

// runsRestartCmd: resume the run's native session (default), start a fresh
// session (--fresh), or requeue a parked/failed run with a reset budget
// window (--requeue).
func runsRestartCmd(args Args) error {
	if args.Bool("fresh") && args.Bool("requeue") {
		return tuskerError(errorInvalidArg, "runs restart takes --fresh or --requeue, not both")
	}
	switch {
	case args.Bool("fresh"):
		return runsFreshCmd(args)
	case args.Bool("requeue"):
		return redriveCmd(args)
	}
	return runsContinueCmd(args)
}

// runsStopRoute: --soft interrupts the current turn and keeps the session;
// the default records a lasting stop intent.
func runsStopRoute(args Args) error {
	if args.Bool("soft") {
		return runsInterruptCmd(args)
	}
	return runsStopCmd(args)
}

// runsRetireRoute: --release frees a dead run's lease so it can run again;
// the default retires the record for good.
func runsRetireRoute(args Args) error {
	if args.Bool("release") {
		return runsReleaseCmd(args)
	}
	return runsRetireCmd(args)
}

// messageAskCmd: a question by default; --notify sends a notice that needs no
// reply; --severity raises an operator escalation record (ESC-) instead.
func messageAskCmd(args Args) error {
	if severity := strings.TrimSpace(args.String("severity")); severity != "" {
		if to := strings.TrimSpace(firstNonEmpty(args.String("to"), args.String("recipient"))); to != "" && to != "operator" {
			return tuskerError(errorInvalidArg, "message ask --severity escalates to the operator only")
		}
		if args.String("description") == "" && args.String("body") != "" {
			args["description"] = args.String("body")
		}
		return escalationV7CreateCmd(args)
	}
	if args.String("recipient") == "" && args.String("to") != "" {
		args["recipient"] = args.String("to")
	}
	if args.Bool("notify") {
		return agentMessageCmd("message send", args)
	}
	return agentMessageCmd("message ask", args)
}

// messageMarkCmd: --consumed (read) or --applied (acted on). An escalation ID
// (ESC-) is acknowledged with --applied.
func messageMarkCmd(args Args) error {
	id := strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0")))
	consumed, applied := args.Bool("consumed"), args.Bool("applied")
	if consumed == applied {
		return tuskerError(errorInvalidArg, "message mark takes exactly one of --consumed or --applied", withHint("tusker message mark <ID> --consumed|--applied"))
	}
	if strings.HasPrefix(strings.ToUpper(id), "ESC-") {
		if !applied {
			return tuskerError(errorInvalidArg, "escalations are acknowledged with --applied")
		}
		args["id"] = id
		return escalationV7AckCmd(args)
	}
	if applied {
		return agentMessageCmd("message apply", args)
	}
	return agentMessageCmd("message consume", args)
}
