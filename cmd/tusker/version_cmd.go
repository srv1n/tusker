package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
)

const versionSchema = "tusker.version/v1"

var buildVersion = "dev"

type VersionProjection struct {
	Schema       string `json:"schema"`
	Version      string `json:"version"`
	Revision     string `json:"revision,omitempty"`
	Modified     bool   `json:"modified"`
	VCSTime      string `json:"vcs_time,omitempty"`
	GoVersion    string `json:"go_version"`
	GOOS         string `json:"goos"`
	GOARCH       string `json:"goarch"`
	BinarySHA256 string `json:"binary_sha256,omitempty"`
}

func versionCmd(args Args) error {
	info, _ := debug.ReadBuildInfo()
	executable, _ := os.Executable()
	projection := buildVersionProjection(info, executable)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "version": projection})
		return nil
	}
	revision := shortCommit(projection.Revision)
	if revision == "" {
		revision = "revision-unavailable"
	}
	if projection.Modified {
		revision += "+dirty"
	}
	fmt.Printf("tusker %s (%s) %s %s/%s\n", projection.Version, revision, projection.GoVersion, projection.GOOS, projection.GOARCH)
	if projection.BinarySHA256 != "" {
		fmt.Printf("binary sha256:%s\n", projection.BinarySHA256)
	}
	return nil
}

func buildVersionProjection(info *debug.BuildInfo, executable string) VersionProjection {
	projection := VersionProjection{
		Schema:    versionSchema,
		Version:   firstNonEmpty(strings.TrimSpace(buildVersion), "dev"),
		GoVersion: runtime.Version(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}
	if info != nil {
		if projection.Version == "dev" && strings.TrimSpace(info.Main.Version) != "" && info.Main.Version != "(devel)" {
			projection.Version = strings.TrimSpace(info.Main.Version)
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				projection.Revision = strings.TrimSpace(setting.Value)
			case "vcs.modified":
				projection.Modified = strings.EqualFold(strings.TrimSpace(setting.Value), "true")
			case "vcs.time":
				projection.VCSTime = strings.TrimSpace(setting.Value)
			}
		}
	}
	if resolved, err := filepath.EvalSymlinks(strings.TrimSpace(executable)); err == nil {
		executable = resolved
	}
	if raw, err := os.ReadFile(strings.TrimSpace(executable)); err == nil {
		sum := sha256.Sum256(raw)
		projection.BinarySHA256 = hex.EncodeToString(sum[:])
	}
	return projection
}
