//go:build !darwin && !linux

package main

import "runtime"

func serveDeliveryPlanSnapshotAt(_ RegisteredProject, _ string, _ []string) (*serveDeliveryPlanSnapshot, error) {
	return nil, tuskerError(
		errorInvalidTransition,
		"Serve delivery plan snapshots are unavailable on "+runtime.GOOS+"; refusing an unbound plan read",
		withHint("run Tusker Serve on macOS or Linux to review or start delivery plans"),
		withContext(map[string]any{"goos": runtime.GOOS, "supported": []string{"darwin", "linux"}}),
	)
}
