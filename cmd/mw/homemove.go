package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/homemove"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// newHomeMoveCmd builds `mw home move <host>`: run on the host that becomes home,
// it takes the home over from an old home that is dead.
func newHomeMoveCmd() *cobra.Command {
	var oldHomeDead, dryRun bool

	cmd := &cobra.Command{
		Use:   "move <host>",
		Short: "Make this host home, taking over from an old home that is dead",
		Long: "move runs ON the host that becomes home (`mw home move laptop` on the Laptop) and takes\n" +
			"the home over from the other one, which must not answer ssh (its [hands_hosts] line).\n" +
			"The steps, each printed with its way back:\n\n" +
			"  1. asks whether the old home answers ssh (10 s). If it does, stops: old home is up: use\n" +
			"     --planned when it exists. If it does not, goes on only with --old-home-dead.\n" +
			"  2. beads: reads when GitHub's refs/dolt/data was written, sets the embedded database\n" +
			"     aside in a dated directory (never deleted), runs `bd bootstrap --yes` and restores\n" +
			"     .beads/config.yaml, and starts the dolt-beads user unit if this host has one.\n" +
			"  3. the vault: writes the home file, commits it and pushes. That is the fence: an old\n" +
			"     home that comes back reads it and stays quiet.\n" +
			"  4. Postern: starts the postern-backend user unit and waits for its /healthz to answer\n" +
			"     as home. Never `mw postern serve` here: it writes POSTERN_ISSUER_KEY back.\n" +
			"  5. the Mayor: mails the Mayor, then runs the vault's bin/mayor-up.\n" +
			"  6. prints what was lost: the age of GitHub's backup and of the Postern data here.\n\n" +
			"--dry-run prints every step and its way back and runs none of them. A step that fails\n" +
			"stops the move and prints the ways back of what was already done, last first. The design\n" +
			"is docs/home-move.md.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			reach, err := config.HandsHosts()
			if err != nil {
				return err
			}
			data, err := config.PosternData()
			if err != nil {
				return err
			}
			backend, err := config.PosternLocalURL(host)
			if err != nil {
				return err
			}
			beadsSync, err := hostBeadsSync()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("there is no home directory to set the old database aside in: %w", err)
			}

			vault := mwVault(dir, host)
			return application.HomeMove{
				Files:       vault,
				Writer:      vault,
				Vault:       vault,
				Machine:     homemove.Host{Vault: dir, Home: home},
				Mailbox:     mwGateway(dir, host),
				Index:       postern.Mirrorer{},
				Lock:        hostSyncLock(),
				Out:         cmd.OutOrStdout(),
				Host:        host,
				Target:      args[0],
				Reach:       reach,
				VaultDir:    dir,
				DataDir:     data,
				BackendURL:  backend,
				Actor:       application.SeatIdentity(application.MwSeat, host),
				BeadsSync:   beadsSync,
				OldHomeDead: oldHomeDead,
				DryRun:      dryRun,
			}.Run(cmd.Context())
		},
	}
	cmd.Flags().BoolVar(&oldHomeDead, "old-home-dead", false, "say that the old home is dead: go on when it does not answer ssh")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print every step and its way back, and run none of them")
	return cmd
}
