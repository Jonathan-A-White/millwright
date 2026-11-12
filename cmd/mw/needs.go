package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// newNeedsCmd builds `mw needs`: the Needs you view's own upkeep.
func newNeedsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "needs",
		Short: "Keep the Needs you view small: sweep the landings the factory can verify itself",
	}
	cmd.AddCommand(newNeedsSweepCmd())
	return cmd
}

func newNeedsSweepCmd() *cobra.Command {
	var dryRun, write, staleLive bool
	cmd := &cobra.Command{
		Use:   "sweep [--dry-run|--write] [--stale-live]",
		Short: "Check the unverified landings and comment VERIFIED on the internal ones the factory can see in main",
		Long: "sweep reads the landings the Needs you view makes a verify need of (closed as landed within 30 days, no\n" +
			"comment opening VERIFIED or NOT MET) and tells each apart, one line each: bead, class, what it would write.\n" +
			"  internal    its closing step said 'Internal: nothing for the Governor to look at'; the only class written\n" +
			"  reverted    a later commit on the rig's main says 'This reverts commit <its commit>' and no later merge of\n" +
			"              its branch follows: no comment, one line in one mail to the Mayor (once per landing)\n" +
			"  follow-up-open  an open bug names it        his-words  a message of his is on the bead after it closed\n" +
			"  no-deploy   his line, and this host keeps no deployed mark for the rig\n" +
			"  not-deployed  its commit is not in the deployed commit   fresh  closed under [needs] min_age ago\n" +
			"  his-line    his to walk (stale-live is its old, seen-live part)   unproven  the commit is not shown in main\n" +
			"  no-check    its closing step gives neither a HOW TO CHECK IT nor an Internal line\n\n" +
			"--dry-run (the default) prints and writes nothing. --write comments on an internal landing whose commit is\n" +
			"an ancestor of the rig's main and, where this host keeps built-<rig>.deployed, of the deployed commit,\n" +
			"closed at least [needs] min_age (default 24h) ago. The comment opens 'VERIFIED by the factory's own check, not\n" +
			"by his use' and names the commit, where it was seen and the time. Never on a landing with a message of his on\n" +
			"its bead after it closed, an open bug naming it, or a revert. One tracker write at a time; a failed one stops\n" +
			"the run naming the bead. A --write run is recorded in ~/.local/state/mw/needs/<run>.json.\n\n" +
			"--stale-live (off unless given; PROVISIONAL) also writes on his line's landing closed 14 days or more whose\n" +
			"deployed build the rig's site_url page states; the comment ends 'His own walk stays open to him.'",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dryRun && write {
				return fmt.Errorf("mw needs sweep: --dry-run and --write are opposites; give one")
			}
			sweep, err := newNeedsSweep(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			_, err = sweep.Run(cmd.Context(), application.NeedsSweepOptions{Write: write, StaleLive: staleLive})
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be written and write nothing (the default)")
	cmd.Flags().BoolVar(&write, "write", false, "write the VERIFIED comments and the one mail, and keep the run's record")
	cmd.Flags().BoolVar(&staleLive, "stale-live", false, "also write on his line's landings of 14 days or more whose deployed build the live page states")
	return cmd
}

// newNeedsSweep is the mw needs sweep the config describes, saying what it did on out.
func newNeedsSweep(out io.Writer) (application.NeedsSweep, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.NeedsSweep{}, err
	}
	host, err := config.Host()
	if err != nil {
		return application.NeedsSweep{}, err
	}
	settings, err := config.Needs()
	if err != nil {
		return application.NeedsSweep{}, err
	}
	runs, err := config.NeedsRunDir()
	if err != nil {
		return application.NeedsSweep{}, err
	}
	rigs, err := config.Rigs()
	if err != nil {
		return application.NeedsSweep{}, err
	}
	gateway := mwGateway(dir, host)
	return application.NeedsSweep{
		Beads: gateway, Git: rig.New(), Marks: hostDeployedMarks(), Rules: vault.New(dir), Web: verifyWeb{},
		Mailbox: gateway, Dirs: rigs, Settings: application.NeedsSettings{MinAge: settings.MinAge},
		RunDir: runs, Host: host, Out: out,
	}, nil
}
