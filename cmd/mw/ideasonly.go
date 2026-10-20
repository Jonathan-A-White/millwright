package main

import (
	"fmt"
	"io"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/ideasonly"

	"github.com/spf13/cobra"
)

// ideasOnlyMode is the Governor's Ideas only as this host knows it: the state
// and held files under ~/.local/state/mw/postern/ideas-only, and, when a
// postern backend and key are configured, the backend to ask. With asked
// false it is the files alone, for a command that must not wait on the
// network.
func ideasOnlyMode(asked bool) (application.IdeasOnlyMode, error) {
	dir, err := config.IdeasOnlyDir()
	if err != nil {
		return application.IdeasOnlyMode{}, err
	}
	governorKey, err := config.PosternGovernorKey()
	if err != nil {
		return application.IdeasOnlyMode{}, err
	}
	mode := application.IdeasOnlyMode{Files: ideasonly.New(dir), GovernorKey: governorKey}
	if !asked {
		return mode, nil
	}
	keys, err := posternKeys()
	if err != nil {
		return application.IdeasOnlyMode{}, err
	}
	backend, err := posternBackend(keys)
	if err != nil {
		return application.IdeasOnlyMode{}, err
	}
	mode.Reader = backend
	return mode, nil
}

// bootIdeasOnly is the mode for mw status and mw brief: the backend is asked
// when it can be, and the files are read when it cannot. Nil when this host
// keeps no files to read.
func bootIdeasOnly() *application.IdeasOnlyMode {
	if mode, err := ideasOnlyMode(true); err == nil {
		return &mode
	}
	if mode, err := ideasOnlyMode(false); err == nil {
		return &mode
	}
	return nil
}

// ideasOnlyReleaser is what gives the Governor's ideas back, wired as the
// postern inbox's hook is, so that a held tap is applied as the hook would have
// applied it. Nil when this host cannot be wired for it: the wait that asks
// goes on without, and the ideas stay held for mw ideas-only release.
func ideasOnlyReleaser(errOut io.Writer) *application.IdeasOnlyRelease {
	inbox, wired, err := newPosternInbox(nil)
	if err == nil {
		err = answerTheGovernor(&inbox, wired.backend, wired.keys, wired.governorKey)
	}
	if err != nil || inbox.Cards == nil {
		if err == nil {
			err = fmt.Errorf("no card can be sent")
		}
		fmt.Fprintf(errOut, "mw: Ideas only will not be given back by this command: %v\n", err)
		return nil
	}
	return &application.IdeasOnlyRelease{Inbox: inbox}
}

// newIdeasOnlyCmd builds `mw ideas-only`: whether the Governor is in Ideas
// only, until when, and the records mw is keeping for him meanwhile.
func newIdeasOnlyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ideas-only",
		Short: "Say whether the Governor is in Ideas only, until when, and what is held for him",
		Long: "ideas-only asks the postern backend (GET /api/ideas-only/<postern_governor_key>, with the\n" +
			"Mayor's key) whether the Governor has put his key in Ideas only, and prints 'off' or 'on until\n" +
			"21:00 UTC' (with his own zone beside it when his weekly schedule names one), then how many\n" +
			"ideas are held and one line for each: txid, channel, class, when, and the first 80 runes of\n" +
			"his words.\n\n" +
			"While the mode is on, mw postern inbox --apply and mw talk wait keep every record of his\n" +
			"(words, voice notes, Talk turns, call requests and taps) in\n" +
			"~/.local/state/mw/postern/ideas-only/held.jsonl instead of acting on it, and answer each in\n" +
			"its thread with 'Recorded for after 21:00 UTC', at zero tokens. mw keeps its own note of the\n" +
			"mode in state.json beside it: a backend out of reach shows what was last known, and a note\n" +
			"that says on is not believed past its end. mw status and mw brief print one heading line,\n" +
			"'Governor: Ideas only until 21:00 UTC (2 ideas held); to-dos still work', while it is on.\n\n" +
			"It changes nothing on the backend and releases nothing; mw ideas-only release does that.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode, err := ideasOnlyMode(true)
			if err != nil {
				return err
			}
			_, err = application.IdeasOnlyShow{Mode: mode, Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}.Run(cmd.Context())
			return err
		},
	}
	cmd.AddCommand(newIdeasOnlyReleaseCmd())
	return cmd
}

// newIdeasOnlyReleaseCmd builds `mw ideas-only release`: give the ideas back by
// hand.
func newIdeasOnlyReleaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "release",
		Short: "Give the Governor's ideas back, once, now that Ideas only has ended",
		Long: "release does what the end of Ideas only does by itself, for when no wait or hook was there to see\n" +
			"it. When the mode is not on and ideas are held, it sends the Governor one live card, 'Your ideas\n" +
			"from Ideas only (N)', with an item for each idea (its first 80 runes, where he sent it and when;\n" +
			"the Mayor adds links with mw card update as he files them), applies the taps he made meanwhile in\n" +
			"the order he made them, lets go of the held marks, puts the postern inbox's cursor back to the\n" +
			"earliest held record so that mw postern inbox shows the ideas as unread, and moves held.jsonl\n" +
			"to released/<time>.jsonl. It prints 'Ideas only ended 21:00: N ideas' and a line for each. It\n" +
			"runs once: a second finds nothing held and says 'Ideas only ended: nothing held', sending no\n" +
			"card. While the mode is still on it releases nothing and says so.\n\n" +
			"mw talk wait and mw postern inbox --apply run the same release when they see the end first.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			release := ideasOnlyReleaser(cmd.ErrOrStderr())
			if release == nil {
				return fmt.Errorf("mw ideas-only release: this host is not wired to send the card")
			}
			release.Inbox.Out = cmd.OutOrStdout()
			report, err := release.Run(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), report.String())
			return nil
		},
	}
}
