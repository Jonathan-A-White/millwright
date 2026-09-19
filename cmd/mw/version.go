package main

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version is the factory's version. Release builds override it with
// -ldflags "-X main.version=<version>".
var version = "0.1.0-dev"

// shortRevisionLen is how many characters of the commit hash are printed.
const shortRevisionLen = 7

// versionLine renders "mw <version>", followed by the short commit the binary
// was built from and, for a build from a dirty tree, ", modified". info is what
// the Go toolchain embedded (nil when it embedded nothing); a build with no
// VCS information prints the version alone.
func versionLine(version string, info *debug.BuildInfo) string {
	line := "mw " + version
	if info == nil {
		return line
	}

	var revision string
	var modified bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return line
	}

	if len(revision) > shortRevisionLen {
		revision = revision[:shortRevisionLen]
	}
	if modified {
		return line + " (" + revision + ", modified)"
	}
	return line + " (" + revision + ")"
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of mw",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info, ok := debug.ReadBuildInfo()
			if !ok {
				info = nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), versionLine(version, info))
			return nil
		},
	}
}
