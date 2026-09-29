package main

import (
	"encoding/json"
	"fmt"
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
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "view",
		Short: "Write the sealed live view of the factory for the Governor's app",
		Long: "view builds postern's docs/protocol.md section 11 view (v 2): every epic open or in\n" +
			"progress, every bead under one at any depth (a closed one only if it closed in the last 7\n" +
			"days; an epic's done_earlier counts the rest), every live bead's parent chain up to its\n" +
			"root, and the needs waiting on the Governor, most blocking first, then oldest: an open\n" +
			"postern question, a live epic with held stories (approve), a story closed in the last 24\n" +
			"hours with no VERIFIED comment (verify), an open bead labelled demo or hitl (demo, hands),\n" +
			"a story out of attempts or a host whose last sync is over 20 minutes old with work pathed\n" +
			"to it (alarm). It gzips that JSON, encrypts it with BRC-78 to postern_governor_key, and\n" +
			"writes it base64, atomically, to postern_view_path (default\n" +
			"~/.local/state/postern/view.b64).\n\n" +
			"It costs one bd call per live epic for its children and a handful besides — the live\n" +
			"epics, their own fields, every note at once, and comments only for a question or landing\n" +
			"that needs them — so it can run every half minute.\n\n" +
			"--json prints the plaintext JSON instead of writing anything, for inspection.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			keys, err := posternKeys()
			if err != nil {
				return err
			}
			governorKey, err := config.PosternGovernorKey()
			if err != nil {
				return err
			}
			// Checked before Build: a key file not there yet would otherwise
			// surface only once the view is sealed, after every read.
			if exists, err := keys.Exists(); err != nil {
				return err
			} else if !exists {
				return fmt.Errorf("no postern key at %s: run mw postern key init first", keys.Path())
			}
			path, err := config.PosternViewPath()
			if err != nil {
				return err
			}
			view.Cipher = posternCipher(keys)
			view.File = postern.NewSnapshotFile(path)
			view.GovernorKey = governorKey
			view.Out = cmd.OutOrStdout()
			_, err = view.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print the plaintext view JSON instead of writing the sealed file")
	return cmd
}
