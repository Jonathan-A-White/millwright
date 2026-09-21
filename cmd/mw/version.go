package main

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
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

// harnessVersionTimeout is how long mw waits for the harness to say its version.
const harnessVersionTimeout = 10 * time.Second

// harnessVersionLine renders "claude <version>". A harness that is not
// installed, or that will not say its version, is reported plainly, never as an
// error: `mw version` is where a person looks first, and it must still answer.
func harnessVersionLine(ctx context.Context, harness *claude.Harness) string {
	printed, err := harness.Version(ctx)
	switch {
	case errors.Is(err, claude.ErrNotInstalled):
		return claude.Program + ": not installed"
	case err != nil:
		return claude.Program + ": version unknown (" + err.Error() + ")"
	}
	return claude.Program + " " + printed
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of mw and of the harness it starts sessions with",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info, ok := debug.ReadBuildInfo()
			if !ok {
				info = nil
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), harnessVersionTimeout)
			defer cancel()

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, versionLine(version, info))
			fmt.Fprintln(out, harnessVersionLine(ctx, claude.New()))
			return nil
		},
	}
}
