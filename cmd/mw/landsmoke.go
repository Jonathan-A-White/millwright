package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/spf13/cobra"
)

// smokeSlotSuffix names the lock the grist smokes of one rig follow each other
// by: its own, beside the merge slot and the after-landing lock.
const smokeSlotSuffix = ".grist-smoke-slot"

// landSmokeLogDir is where the grist smokes mw land hands apart keep their
// output, one file for each rig, beside the landings' own state.
func landSmokeLogDir(home string) string {
	return filepath.Join(landStateDir(home), "smoke")
}

// landSmokeApart is how mw land hands the grist smoke of a landed batch to a
// process of its own: `self land-smoke <rig> ...`, in a session and a systemd
// scope of its own, its output appended to logDir/<rig>.log. mw land ends
// without it, which is why the follower can land the rig's next batch while it
// goes on (mw-apvyg8j).
type landSmokeApart struct {
	self, logDir string
	// noScope keeps the process out of a systemd scope.
	noScope bool
}

var _ application.SmokeApart = landSmokeApart{}

// Begin implements application.SmokeApart.
func (l landSmokeApart) Begin(ctx context.Context, s application.LandedSmoke) error {
	args := []string{
		"land-smoke", s.Rig,
		"--batch", strconv.Itoa(s.Number),
		"--target", s.Target,
		"--commit", s.Commit,
		"--story", s.Story,
		"--apps", strings.Join(s.Apps, ","),
	}
	return doctor.ApartProcess{Self: l.self, Args: args, Log: filepath.Join(l.logDir, s.Rig+".log"), NoScope: l.noScope}.Start(ctx)
}

// newLandSmokeCmd builds `mw land-smoke <rig>`, the process mw land begins the
// grist smoke of a landed batch in: the smoke as mw land would have made it,
// with the mail to the Mayor when it fails. Nobody runs it by hand.
func newLandSmokeCmd() *cobra.Command {
	var number int
	var target, commit, story, apps string
	cmd := &cobra.Command{
		Use:    "land-smoke <rig>",
		Short:  "Make the grist smoke of a batch mw land landed, apart from mw land",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			if err := os.Chdir(dir); err != nil {
				return fmt.Errorf("the grist smoke of a batch of %s: the vault %s cannot be worked from: %w", args[0], dir, err)
			}
			gateway := mwGateway(dir, host)
			files := mwVault(dir, host)
			next := application.Next{
				Tracker:   gateway,
				Mailbox:   gateway,
				Smoke:     hostGristSmoke(gateway, host, rigs, rig.New(), cmd.OutOrStdout(), application.BatchLogPrefix+"grist smoke: "),
				SmokeSlot: rig.NewSlots(rig.WithSlotSuffix(smokeSlotSuffix)),
				Host:      host,
				Rigs:      rigs,
				Out:       cmd.OutOrStdout(),
				Err:       cmd.ErrOrStderr(),
			}
			smoke := application.LandedSmoke{Rig: args[0], Number: number, Target: target, Commit: commit, Story: story}
			for _, app := range strings.Split(apps, ",") {
				if app = strings.TrimSpace(app); app != "" {
					smoke.Apps = append(smoke.Apps, app)
				}
			}
			if len(smoke.Apps) == 0 {
				return fmt.Errorf("no app to smoke: --apps names none")
			}
			return application.BatchLand{Next: withVaultPorts(next, files)}.SmokeLanded(cmd.Context(), smoke)
		},
	}
	cmd.Flags().IntVar(&number, "batch", 0, "the number of the batch that landed")
	cmd.Flags().StringVar(&target, "target", "", "the branch the batch landed on")
	cmd.Flags().StringVar(&commit, "commit", "", "the commit the batch landed at")
	cmd.Flags().StringVar(&story, "story", "", "the batch's newest story, on which a failure is written")
	cmd.Flags().StringVar(&apps, "apps", "", "the apps whose grist the batch changed, comma-separated")
	return cmd
}
