package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// posternViewClock stamps the written_at a view is built with. A test fixes
// it.
var posternViewClock = time.Now

// newPosternViewCmd builds `mw postern view`: the live view of the factory,
// sealed to the Governor's key and written where the postern backend serves
// it as GET /api/view.
func newPosternViewCmd() *cobra.Command {
	var jsonOut, follow bool
	var every time.Duration

	cmd := &cobra.Command{
		Use:   "view",
		Short: "Write the sealed live view of the factory for the Governor's app",
		Long: "view builds postern's docs/protocol.md section 11 view (v 2): every epic open or in\n" +
			"progress, every bead under one at any depth (a closed one only if it closed in the last 7\n" +
			"days; an epic's done_earlier counts the rest), every live bead's parent chain up to its\n" +
			"root, and the needs waiting on the Governor, most blocking first, then oldest: an open\n" +
			"postern question, a live epic with held stories (approve), a story closed in the last 24\n" +
			"hours with no VERIFIED comment (verify), an open bead labelled demo or hitl (demo, hands),\n" +
			"an approve need over 7 days old or a hands need over 3 days old, which becomes one stale\n" +
			"need (Keep or Close) unless a postern.keep note holds a time still ahead, a\n" +
			"story out of attempts or a host whose last sync is over 20 minutes old with work pathed\n" +
			"to it (alarm). It gzips that JSON, encrypts it with BRC-78 to postern_governor_key, and\n" +
			"writes it base64, atomically, to postern_view_path (default\n" +
			"~/.local/state/postern/view.b64).\n\n" +
			"It reads every bead in one bd list, every note at once, and comments only for a question\n" +
			"or landing that needs them — about a second on the live vault — so it can run every few\n" +
			"seconds.\n\n" +
			"--json prints the plaintext JSON instead of writing anything, for inspection.\n\n" +
			"--follow does not exit: every --every (default 1s) it reads the beads' head, Dolt's hash of\n" +
			"the whole database (one `bd sql` call, tokenless), and writes the view again only when it\n" +
			"has changed since the last write, so a tap of the Governor's shows within two seconds.\n" +
			"A failure is logged and the loop goes on; SIGTERM or SIGINT stops it. It is what\n" +
			"contrib/systemd/mw-view-follow.service runs.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if follow && jsonOut {
				return fmt.Errorf("--follow writes the sealed view, so it cannot be used with --json")
			}
			if !follow && cmd.Flags().Changed("every") {
				return fmt.Errorf("--every is how often --follow reads the beads' head: it needs --follow")
			}
			if every <= 0 {
				return fmt.Errorf("--every must be a positive duration, like 1s")
			}
			gateway, host, err := posternGateway()
			if err != nil {
				return err
			}
			view := application.PosternView{
				Tracker: gateway,
				Notes:   gateway,
				Host:    host,
				Now:     posternViewClock,
				Err:     cmd.ErrOrStderr(),
			}
			if follow {
				ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, os.Interrupt)
				defer stop()
				return application.PosternViewFollow{
					Head: gateway,
					Publish: func(ctx context.Context) error {
						sealed, err := sealedPosternView(view)
						if err != nil {
							return err
						}
						sealed.Out = cmd.OutOrStdout()
						_, err = sealed.Run(ctx)
						return err
					},
					Every: every,
					Err:   cmd.ErrOrStderr(),
				}.Run(ctx)
			}
			if jsonOut {
				doc, err := view.Build(cmd.Context())
				if err != nil {
					return err
				}
				encoded, err := json.Marshal(doc)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
				return nil
			}

			view, err = sealedPosternView(view)
			if err != nil {
				return err
			}
			view.Out = cmd.OutOrStdout()
			_, err = view.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&follow, "follow", false, "keep running: write the view again whenever the beads change")
	cmd.Flags().DurationVar(&every, "every", application.DefaultViewFollowEvery, "with --follow, how often to read the beads' head")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print the plaintext view JSON instead of writing the sealed file")
	return cmd
}

// sealedPosternView is view with what Run needs to write it: the postern key
// to seal with, the Governor's key to seal to, and the file the backend
// serves. It is what `mw postern view` and the view mw hands add publishes
// are both made of.
func sealedPosternView(view application.PosternView) (application.PosternView, error) {
	keys, err := posternKeys()
	if err != nil {
		return view, err
	}
	governorKey, err := config.PosternGovernorKey()
	if err != nil {
		return view, err
	}
	// Checked before Build: a key file not there yet would otherwise
	// surface only once the view is sealed, after every read.
	if exists, err := keys.Exists(); err != nil {
		return view, err
	} else if !exists {
		return view, fmt.Errorf("no postern key at %s: run mw postern key init first", keys.Path())
	}
	path, err := config.PosternViewPath()
	if err != nil {
		return view, err
	}
	view.Cipher = posternCipher(keys)
	view.File = postern.NewSnapshotFile(path)
	view.GovernorKey = governorKey
	return view, nil
}
