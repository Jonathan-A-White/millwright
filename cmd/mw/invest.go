package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newInvestCmd builds `mw invest`: the factory's investments, each with its aim,
// predicted gain, measure and review date, and the outcomes over time.
func newInvestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "invest",
		Short: "Keep the factory's investments and their outcomes: the aim, the prediction, what happened, the lesson",
		Long: "invest keeps an append-only ledger of the factory's investments in\n" +
			"plans/investments/ledger.jsonl in the vault. mw invest add records one: its aim, the gain it\n" +
			"predicts (tokens, speed or quality), how that is measured and the days until it is looked at\n" +
			"again. mw invest outcome records what happened: win, loss or mixed, with numbers and the\n" +
			"lesson, and the better approach if there is one. A line is never edited; a later outcome is\n" +
			"a new line and the newest is the one shown. mw invest due lists those whose review date has\n" +
			"passed with no outcome, mw invest list shows all of them, and mw report ends its window with\n" +
			"the wins celebrated and the losses told with their lesson.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newInvestAddCmd(), newInvestOutcomeCmd(), newInvestDueCmd(), newInvestListCmd())
	return cmd
}

// investFromConfig is the use case over this host's vault, acting as the mw seat.
func investFromConfig(cmd *cobra.Command) (application.Invest, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.Invest{}, err
	}
	actor, err := beads.ActorFromConfig()
	if err != nil {
		return application.Invest{}, err
	}
	host, err := config.Host()
	if err != nil {
		return application.Invest{}, err
	}
	return application.Invest{Book: mwVault(dir, host), Actor: actor, Out: cmd.OutOrStdout()}, nil
}

func newInvestAddCmd() *cobra.Command {
	var aim, predict, measure string
	var review int
	cmd := &cobra.Command{
		Use:   `add <bead> --aim "<what it tries to achieve>" --predict "<gain>" --measure "<how>" --review <days>`,
		Short: "Record an investment: its aim, predicted gain, measure and when to look again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			invest, err := investFromConfig(cmd)
			if err != nil {
				return err
			}
			return invest.Add(cmd.Context(), args[0], aim, predict, measure, review)
		},
	}
	cmd.Flags().StringVar(&aim, "aim", "", "what the investment tries to achieve")
	cmd.Flags().StringVar(&predict, "predict", "", "the gain predicted, in tokens, speed or quality")
	cmd.Flags().StringVar(&measure, "measure", "", "how the gain is measured")
	cmd.Flags().IntVar(&review, "review", 0, "days until it is looked at again")
	return cmd
}

func newInvestOutcomeCmd() *cobra.Command {
	var result, actual, lesson, next string
	cmd := &cobra.Command{
		Use:   `outcome <bead> --result win|loss|mixed --actual "<what happened, with numbers>" --lesson "<what was learned>" [--next "<better approach or bead>"]`,
		Short: "Record what happened to an investment: win, loss or mixed, and the lesson",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			invest, err := investFromConfig(cmd)
			if err != nil {
				return err
			}
			return invest.Outcome(cmd.Context(), args[0], result, actual, lesson, next)
		},
	}
	cmd.Flags().StringVar(&result, "result", "", "win, loss or mixed")
	cmd.Flags().StringVar(&actual, "actual", "", "what actually happened, with numbers")
	cmd.Flags().StringVar(&lesson, "lesson", "", "what was learned")
	cmd.Flags().StringVar(&next, "next", "", "the better approach, or the follow-up bead")
	return cmd
}

func newInvestDueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "due",
		Short: "List the investments whose review date has passed with no outcome",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			invest, err := investFromConfig(cmd)
			if err != nil {
				return err
			}
			return invest.Due(cmd.Context())
		},
	}
}

func newInvestListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show every investment: aim, prediction, measure, outcomes in order and the lesson",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			invest, err := investFromConfig(cmd)
			if err != nil {
				return err
			}
			return invest.List(cmd.Context())
		},
	}
}
