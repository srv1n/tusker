package main

func retryNowCmd(args Args) error {
	identity, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	ok, err := store.ForceRetryNow(identity)
	if err != nil {
		return err
	}
	if !ok {
		return tuskerError(errorNotFound, "Run not found for retry: "+identity, withContext(map[string]any{"id": identity}))
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "id": identity, "queued": true})
		return nil
	}
	println("Queued retry for " + identity)
	return nil
}
