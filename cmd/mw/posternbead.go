package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// exitStatus is the status mw leaves with after a command reported err: a
// bead mw postern bead does not know leaves with 3, which the postern
// backend answers 404 (postern's docs/protocol.md §12); anything else is as
// exitCode says.
func exitStatus(err error) int {
	var handedOff *handedOffExit
	if errors.As(err, &handedOff) {
		return handedOff.code
	}
	if application.PosternBeadIsMissing(err) {
		return application.PosternBeadMissingExit
	}
	return exitCode(err)
}

// newPosternBeadCmd builds `mw postern bead <id>`: one bead in full, sealed
// to the Governor's key, for the postern backend's GET /api/beads/{id}.
func newPosternBeadCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "bead <id>",
		Short: "Print one bead in full, sealed to the Governor's key",
		Long: "bead prints postern's docs/protocol.md section 12 detail of one bead: its fields, the\n" +
			"beads it waits on and those that wait on it within its epic, its children if it is an\n" +
			"epic, its full description and acceptance, and every comment, oldest first, each cut at\n" +
			"16000 runes. The JSON is gzipped, encrypted with BRC-78 to postern_governor_key and\n" +
			"printed base64 on one line — the same encoding as mw postern view. It only reads.\n\n" +
			"The postern backend runs it per request (POSTERN_BEAD_CMD). A bead the tracker does not\n" +
			"know leaves with status 3, saying so on standard error.\n\n" +
			"--json prints the plaintext JSON instead.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			bead := application.PosternBead{Tracker: gateway}
			if jsonOut {
				detail, err := bead.Build(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				encoded, err := json.Marshal(detail)
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
			bead.Cipher = posternCipher(keys)
			bead.GovernorKey = governorKey
			bead.Out = cmd.OutOrStdout()
			_, err = bead.Run(cmd.Context(), args[0])
			return err
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print the plaintext detail JSON instead of the sealed text")
	return cmd
}
