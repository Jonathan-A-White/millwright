package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/bdprogram"
)

// bdRealEnv names the bd that mw bd runs. The bd a Builder session finds first
// on its PATH is a script that runs mw bd, so the real one is named for it.
const bdRealEnv = "MW_BD_REAL"

// newBdCmd builds `mw bd <bd arguments>`: the real bd, run again when it meets a
// passing fault of the beads server. The Builder's session finds it as `bd`.
func newBdCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bd <bd arguments>",
		Short: "Run bd, again when it meets a passing fault of the beads server",
		Long: "bd runs the real bd with the arguments it is given, and when bd fails on a passing fault of the\n" +
			"beads server (dolt server unreachable, connection refused, i/o timeout, write commit result\n" +
			"indeterminate) it waits and runs it again, for up to two minutes. A close or a comment that bd\n" +
			"said was indeterminate is checked on the bead first, so that it is never written twice; a create is\n" +
			"never run again. A command that still fails leaves with bd's status and says which fault it met.\n\n" +
			"A Builder's session runs it as `bd`, through a script ahead of the real bd on its PATH. The real bd\n" +
			"is named in " + bdRealEnv + ", or is the first bd on PATH.",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			real := os.Getenv(bdRealEnv)
			if real == "" {
				found, err := exec.LookPath("bd")
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "mw bd: there is no bd to run: %v\n", err)
					return &handedOffExit{code: 1}
				}
				real = found
			}
			exit, err := application.BdRetry{
				Program: bdprogram.Program{Path: real},
				Out:     cmd.OutOrStdout(),
				Err:     cmd.ErrOrStderr(),
			}.Run(cmd.Context(), args)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "mw bd: %v\n", err)
				return &handedOffExit{code: 1}
			}
			if exit != 0 {
				return &handedOffExit{code: exit}
			}
			return nil
		},
	}
}
