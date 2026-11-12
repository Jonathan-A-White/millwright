package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/bdprogram"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
	"github.com/Jonathan-A-White/millwright/infrastructure/procs"
)

// bdRealEnv names the bd that mw bd runs. The bd a Builder session finds first
// on its PATH is a script that runs mw bd, so the real one is named for it.
const bdRealEnv = "MW_BD_REAL"

// bdWaitFlag and bdWaitEnv set how long mw bd waits for its turn and for quiet
// (mw-gq6.568). The flag counts only as mw bd's first argument, so that no flag
// of bd's own is taken from it; every other argument goes to bd as given.
const (
	bdWaitFlag = "--mw-wait"
	bdWaitEnv  = "MW_BD_WAIT"
)

// bdWaitFrom reads the wait limit from a leading --mw-wait flag, or the
// environment, and returns the arguments left for bd. A zero duration given is
// no waiting (negative to BdRetry); no duration given is its default.
func bdWaitFrom(args []string, env string) (time.Duration, []string, error) {
	text, left := env, args
	if len(args) > 0 {
		switch {
		case args[0] == bdWaitFlag && len(args) > 1:
			text, left = args[1], args[2:]
		case strings.HasPrefix(args[0], bdWaitFlag+"="):
			text, left = strings.TrimPrefix(args[0], bdWaitFlag+"="), args[1:]
		case args[0] == bdWaitFlag:
			return 0, nil, fmt.Errorf("%s needs a duration, such as 30s", bdWaitFlag)
		}
	}
	if text == "" {
		return 0, left, nil
	}
	d, err := time.ParseDuration(text)
	if err != nil || d < 0 {
		return 0, nil, fmt.Errorf("%s %q is not a duration such as 30s (0 waits for nothing)", bdWaitFlag, text)
	}
	if d == 0 {
		d = -1
	}
	return d, left, nil
}

// newBdQuiet is the table of processes mw bd looks at for another bd or a sync;
// a test replaces it so that no bd of the machine it runs on is waited for.
var newBdQuiet = func() application.UserProcesses { return procs.Table{} }

// bdOwnDir is the beads directory this mw bd works on: the -C of its arguments,
// else the directory it runs in; "" when that cannot be told.
func bdOwnDir(args []string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return application.BdBeadsDir(args, cwd)
}

// bdTurn is this host's turn to run a bd in, kept beside the sync lock. A host
// with no home directory keeps none, and bd waits only for quiet.
func bdTurn(args []string) application.BdTurn {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return hostlock.NewBdTurn(filepath.Join(home, SyncLockStateDir), "mw bd "+strings.Join(args, " "))
}

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
			"A hold (bd update <id> --status deferred or blocked) of a story a Builder has claimed (in progress, or\n" +
			"holding a live claim lease) is refused, naming the claimer, and nothing is written: it would leave that run\n" +
			"held mid-work. " + application.BdOverClaimFlag + " \"<why>\" lets it land and writes the why on the bead as an " + application.OverClaimPrefix + "\n" +
			"comment; bd never sees the flag. A bead that cannot be read is held as before.\n\n" +
			"A Builder's session runs it as `bd`, through a script ahead of the real bd on its PATH. The real bd\n" +
			"is named in " + bdRealEnv + ", or is the first bd on PATH.\n\n" +
			"Before it runs bd it waits for this host's turn (one mw bd at a time) and for quiet: no other bd\n" +
			"and no mw sync running (a bd of a test suite's throwaway vault in the temporary directory is not\n" +
			"counted, nor is a bd of another beads directory, by its -C or where it runs). It waits for up to two minutes; a leading " + bdWaitFlag + " <duration> (or " + bdWaitEnv + ")\n" +
			"changes that, 0 waits for nothing. When the limit passes it runs no bd, exits 1 and names the process\n" +
			"it waited for and the mw bd command to run again. bd's own environment and exit status are the caller's.",
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
			wait, args, err := bdWaitFrom(args, os.Getenv(bdWaitEnv))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "mw bd: %v\n", err)
				return &handedOffExit{code: 1}
			}
			exit, err := application.BdRetry{
				Program:   bdprogram.Program{Path: real},
				Turn:      bdTurn(args),
				Quiet:     newBdQuiet(),
				QuietWait: wait,
				Dir:       bdOwnDir(args),
				Out:       cmd.OutOrStdout(),
				Err:       cmd.ErrOrStderr(),
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
