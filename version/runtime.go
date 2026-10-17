// Package version provides build version information from Go's embedded VCS metadata.
package version

import (
	"errors"
	"runtime/debug"
	"strings"
)

// Build metadata, populated by Set.
var (
	Repo       = ""
	Version    = "dev"
	Tag        = "dev"
	CommitDate = ""
)

// ErrBuildDataNotReadable is returned when debug.ReadBuildInfo fails.
var ErrBuildDataNotReadable = errors.New("not able to read build data")

// Set populates version variables from Go's embedded VCS build info.
func Set() error {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return ErrBuildDataNotReadable
	}
	setFrom(buildInfo)

	return nil
}

func setFrom(buildInfo *debug.BuildInfo) {
	Repo = buildInfo.Main.Path
	CommitDate = get(buildInfo, "vcs.time")
	Tag = strings.ReplaceAll(buildInfo.Main.Version, "(devel)", "dev")

	commit := get(buildInfo, "vcs.revision")
	if len(commit) > 8 {
		commit = commit[:8]
	}
	if commit == "" {
		// Module-proxy builds (go install pkg@version) have an exact tag but no VCS metadata.
		if buildInfo.Main.Version != "(devel)" && buildInfo.Main.Version != "" {
			Version = Tag
			return
		}
		commit = "unknown"
	}

	// Main.Version already carries +dirty for a modified tree.
	Version = Tag + "." + commit
}

func get(buildInfo *debug.BuildInfo, key string) string {
	for _, setting := range buildInfo.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}
