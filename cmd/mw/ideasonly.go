package main

import (
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

// newIdeasOnlyCmd builds `mw ideas-only`: whether the Governor is in Ideas
// only, until when, and the records mw is keeping for him meanwhile.
func newIdeasOnlyCmd() *cobra.Command {
	return &cobra.Command{
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
			"'Governor: Ideas only until 21:00 UTC (2 ideas held)', while it is on.\n\n" +
			"It changes nothing on the backend and releases nothing.",
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
}
