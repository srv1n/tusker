package main

import "strings"

func taskUpdateCLICmd(args Args) error {
	if strings.TrimSpace(args.String("if-revision")) != "" {
		return updateV7TaskCmd(args)
	}
	id := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if id == "" {
		return tuskerError(errorMissingArg, "task update requires a task ID")
	}
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		return err
	}
	data, _, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	args = copyArgsForInternalMutation(args)
	args["if-revision"] = stringField(data, "state_rev")
	return updateV7TaskCmd(args)
}
