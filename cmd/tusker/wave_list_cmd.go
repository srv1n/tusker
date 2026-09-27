package main

import (
	"fmt"
	"path/filepath"
)

func waveListCmd(args Args) error {
	if args.String("project") != "" && args.String("vault") != "" {
		return tuskerError(errorInvalidArg, "use either --project or --vault")
	}
	store, err := OpenRuntimeStoreReadOnly(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	server := newServeServer("", "", "", store, nil)
	if args.String("project") == "" {
		vault, err := resolveVaultPath(args, false)
		if err != nil {
			return err
		}
		server.vaultPath, server.repoRoot = vault, filepath.Dir(vault)
	}
	project, err := server.projectForSnapshot(args.String("project"))
	if err != nil {
		return err
	}
	snap, err := server.buildSnapshotForProject(project, false)
	if err != nil {
		return err
	}
	waves := serveWaveList(snap)
	if args.Bool("json") {
		emitJSON(waves)
		return nil
	}
	for _, wave := range waves {
		fmt.Printf("%s  %s  status=%s  authorization=%s  tasks=%d done=%d\n", wave.ID, wave.Title, wave.Status, wave.Authorization, wave.MemberCount, wave.DoneCount)
	}
	return nil
}
