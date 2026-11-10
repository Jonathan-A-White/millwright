package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// newGrillCmd builds `mw grill`: one question of a grilling, as a decision card
// with a running card kept current beside it.
func newGrillCmd() *cobra.Command {
	var ask, recommend, whyNow string
	var options []string

	cmd := &cobra.Command{
		Use:   "grill <ticket> --ask <question> --recommend <option> --option <option>...",
		Short: "Ask a grilling ticket's next question and keep its one running card current",
		Long: "grill sends the decision card mw postern send --class decision-needed --bead <ticket> sends\n" +
			"(--ask is its question; --recommend and --option as there), then keeps ONE running card on\n" +
			"the ticket's channel current: an item per question, 'open, waiting on card <txid>' until the\n" +
			"Governor answers, then 'answered <option> at <time>'. The card is sent with the first question\n" +
			"and updated with each new one; mw postern inbox --apply updates it on each answer it applies.\n" +
			"The card's txid is kept in the ticket's metadata (note postern.grill.<ticket>). The ticket must\n" +
			"carry the label grilling. It prints the decision card's txid, then the running card's.\n\n" +
			"While his review queue is over its limit (review_queue in [dampers]) the question is not sent:\n" +
			"it is written on the ticket as a QUESTION for Monday: comment, Monday's mw digest send carries\n" +
			"it, and one line names the ticket, the queue and the limit. --why-now \"<reason>\" sends it\n" +
			"anyway (use it when the answer unblocks work) and writes the reason on the ticket as\n" +
			"SENT OVER THE LIMIT:.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if cmd.Flags().Changed("why-now") && strings.TrimSpace(whyNow) == "" {
				return fmt.Errorf("mw grill: --why-now needs a reason: say why this question cannot wait for Monday")
			}
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			governorKey, err := config.PosternGovernorKey()
			if err != nil {
				return err
			}
			floatSats, err := config.PosternFloatSats()
			if err != nil {
				return err
			}
			channel, err := config.PosternChannel()
			if err != nil {
				return err
			}
			backend, err := posternBackend(keys)
			if err != nil {
				return err
			}
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			threads, err := posternThreads()
			if err != nil {
				return err
			}
			cards, err := posternCards(out)
			if err != nil {
				return err
			}
			grill := application.Grill{
				Send: application.PosternSend{
					Threads: threads, Postern: backend, Chain: newChain(backend, keys, out),
					Cipher: posternCipher(keys), Keys: keys, Tracker: gateway, Notes: gateway,
					GovernorKey: governorKey, FloatSats: int64(floatSats), Channel: channel,
					Now: posternClock, Out: out,
				},
				Cards: cards, Tracker: gateway, Notes: gateway,
				Hold: application.ReviewHold{Queue: reviewQueueReader(gateway), Tracker: gateway, Out: out, Err: cmd.ErrOrStderr()},
			}
			_, err = grill.Ask(cmd.Context(), application.GrillAsk{Bead: args[0], Question: ask, Recommend: recommend, Options: options, WhyNow: whyNow})
			return err
		},
	}
	cmd.Flags().StringVar(&ask, "ask", "", "the question (required)")
	cmd.Flags().StringVar(&recommend, "recommend", "", "the option the question recommends")
	cmd.Flags().StringArrayVar(&options, "option", nil, "an option the question offers (repeatable)")
	cmd.Flags().StringVar(&whyNow, "why-now", "", "send the question over his review-queue limit anyway, for this reason (written on the ticket as SENT OVER THE LIMIT:)")
	cmd.MarkFlagRequired("ask")
	return cmd
}
