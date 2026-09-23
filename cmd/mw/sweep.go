package main

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"

	"github.com/spf13/cobra"
)

// newSweepCmd builds `mw sweep`: for this host, the claimed stories whose
// session is gone or has gone quiet, marked run=stuck. It is read-mostly and
// zero-token — it never kills or restarts a session, never gives a claim
// back, and never touches a worktree, git or the ledger.
func newSweepCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sweep",
		Short: "Find claimed stories on this host whose session is gone or silent",
		Long: "sweep reads this host's claimed stories and asks the runner about each one's session. A\n" +
			"session that is no longer there is reported stuck straight away. A session that is still\n" +
			"there but has printed nothing new for longer than the stale threshold (config `stale_hours`,\n" +
			"default 2) is reported stuck too. Either way the finding is commented on the bead once and\n" +
			"recorded run=stuck; a story mw next or an earlier sweep already recorded gone is left alone.\n" +
			"What sweep saw of each session is kept as a note in bd's key-value store, not as state, so\n" +
			"that sweeping often files no event beads.\n\n" +
			"sweep never kills or restarts a session, never gives a claim back, and never touches a\n" +
			"worktree, git or the ledger. Settling a stuck claim is a separate command.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			hours, err := config.StaleHours()
			if err != nil {
				return err
			}
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}

			gateway := mwGateway(dir, host)
			_, err = application.Sweep{
				Tracker:    gateway,
				Memory:     gateway,
				Runner:     tmux.New(),
				Host:       host,
				Rigs:       rigs,
				Activity:   worktreeActivity,
				StaleAfter: time.Duration(hours) * time.Hour,
				Out:        cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
}

// worktreeActivity is the application.Sweep.Activity a real mw sweep reads
// worktree writes through: the newest modification time of any file under
// dir, walked the way infrastructure/doctor's beads-size check sizes .beads.
// A worktree not there — the claim just made, or its worktree already
// removed — is not an error: it reports the zero time, so sweep falls back to
// judging that story's session by its pane alone.
func worktreeActivity(_ context.Context, dir string) (time.Time, error) {
	var newest time.Time
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, nil
	}
	return newest, err
}
