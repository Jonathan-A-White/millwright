package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// newCardCmd builds `mw card`: the Mayor's live cards, sent to the Governor
// as records the app keeps current.
func newCardCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "card",
		Short: "Send the Governor a live card, or update one already sent",
		Args:  cobra.NoArgs,
	}
	root.AddCommand(newCardSendCmd())
	root.AddCommand(newCardUpdateCmd())
	return root
}

// cardItemHelp says how an item is given, for every command that takes one.
const cardItemHelp = "an item, '<text>|<links csv>|<bead>:<state>' (repeatable); the state is open, landed, verified, closed, answered or held, derived from the text's ask when left out"

// posternCards is the Cards config wires: the home's postern key sealing to
// postern_governor_key, sent through postern_backend by postern_channel.
func posternCards(out io.Writer) (application.Cards, error) {
	keys, err := posternKeys()
	if err != nil {
		return application.Cards{}, err
	}
	governorKey, err := config.PosternGovernorKey()
	if err != nil {
		return application.Cards{}, err
	}
	floatSats, err := config.PosternFloatSats()
	if err != nil {
		return application.Cards{}, err
	}
	channel, err := config.PosternChannel()
	if err != nil {
		return application.Cards{}, err
	}
	backend, err := posternBackend(keys)
	if err != nil {
		return application.Cards{}, err
	}
	return application.Cards{
		Postern: backend, Cipher: posternCipher(keys), Keys: keys,
		GovernorKey: governorKey, FloatSats: int64(floatSats), Channel: channel,
		Now: posternClock, Out: out,
	}, nil
}

// newCardSendCmd builds `mw card send`.
func newCardSendCmd() *cobra.Command {
	var title, beadChannel string
	var items []string

	cmd := &cobra.Command{
		Use:   "send --title <title> --item '<text>|<links csv>|<bead>:<state>'... [--bead-channel <id>]",
		Short: "Send the Governor a live card: numbered items with links and what each expects",
		Long: "send posts a card record (class card), sealed to the Governor as a message is and sent by\n" +
			"postern_channel, and prints its txid, then each item. Each --item is numbered in the order\n" +
			"given (or by a leading '<n>. '), links the beads in its csv, and expects <bead> to reach\n" +
			"<state>: open, landed, verified, closed, answered or held. An item that gives no expectation has\n" +
			"one derived from its ask: VERIFIED on X expects X verified, Looks good on X expects X closed,\n" +
			"Approve X and Answer X expect X answered, Release X expects X open. The card subscribes to\n" +
			"the beads its items name and the event kinds their expectations need, so the app ticks an\n" +
			"item off when its event arrives. --bead-channel posts it in that bead's channel. The record\n" +
			"carries no summary, so no word of it is pushed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cards, err := posternCards(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			_, _, err = cards.Send(cmd.Context(), application.CardSendRequest{Title: title, Items: items, BeadChannel: beadChannel})
			return err
		},
	}
	cardSendFlags(cmd, &title, &items, &beadChannel)
	return cmd
}

// cardSendFlags are the flags a card is sent with, by mw card send and by mw
// prompt run --card alike.
func cardSendFlags(cmd *cobra.Command, title *string, items *[]string, beadChannel *string) {
	cmd.Flags().StringVar(title, "title", "", "the card's title (required)")
	cmd.Flags().StringArrayVar(items, "item", nil, cardItemHelp)
	cmd.Flags().StringVar(beadChannel, "bead-channel", "", "the bead whose channel the card goes to (default Factory)")
}

// newCardUpdateCmd builds `mw card update`.
func newCardUpdateCmd() *cobra.Command {
	var items, links []string
	var tick []int

	cmd := &cobra.Command{
		Use:   "update <txid> [--item '<n>. <text>|<links csv>|<bead>:<state>']... [--link <n> <bead>]... [--tick <n>]...",
		Short: "Add or change items, links and ticks on a live card already sent",
		Long: "update posts a card-update record (class card-update) naming the card by the txid mw card\n" +
			"send printed, sealed and sent as the card was, and prints its txid. --item adds the item\n" +
			"numbered <n>, or replaces it; --link <n> <bead> (or --link <n>:<bead>) adds a link to item\n" +
			"<n>; --tick <n> ticks item <n> off. Each is repeatable.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paired, err := cardLinks(links, args[1:])
			if err != nil {
				return err
			}
			cards, err := posternCards(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			_, _, err = cards.Update(cmd.Context(), application.CardUpdateRequest{Re: args[0], Items: items, Links: paired, Tick: tick})
			return err
		},
	}
	cmd.Flags().StringArrayVar(&items, "item", nil, "an item added or replaced, '<n>. <text>|<links csv>|<bead>:<state>' (repeatable)")
	cmd.Flags().StringArrayVar(&links, "link", nil, "a link added to an item: --link <n> <bead>, or --link <n>:<bead> (repeatable)")
	cmd.Flags().IntSliceVar(&tick, "tick", nil, "the number of an item ticked off (repeatable)")
	return cmd
}

// cardLinks is each --link as <n>:<bead>. A link given as two words, --link
// <n> <bead>, reaches cobra as the flag's value <n> and a bead among the
// arguments after the txid: the beads pair with the bare numbers in order.
func cardLinks(links, beads []string) ([]string, error) {
	var paired []string
	for _, link := range links {
		if _, err := strconv.Atoi(strings.TrimSpace(link)); err != nil {
			paired = append(paired, link)
			continue
		}
		if len(beads) == 0 {
			return nil, fmt.Errorf("mw card update: --link %s names no bead: give --link <n> <bead> or --link <n>:<bead>", link)
		}
		paired, beads = append(paired, strings.TrimSpace(link)+":"+beads[0]), beads[1:]
	}
	if len(beads) > 0 {
		return nil, fmt.Errorf("mw card update: %q is not a link's bead: give the card's txid only, then --link <n> <bead>", beads[0])
	}
	return paired, nil
}

// promptRunCard is mw prompt run <name> --card ...: the card flags, read from
// the tokens after the name, since cobra parses none of a run's flags.
func promptRunCard(cmd *cobra.Command, name string, tokens []string) error {
	var title, beadChannel string
	var items []string
	var card bool
	flags := &cobra.Command{Use: "run --card"}
	flags.Flags().BoolVar(&card, "card", false, "")
	cardSendFlags(flags, &title, &items, &beadChannel)
	if err := flags.Flags().Parse(tokens); err != nil {
		return fmt.Errorf("mw prompt run --card: %w: a card takes --title, --item and --bead-channel", err)
	}
	if rest := flags.Flags().Args(); len(rest) > 0 {
		return fmt.Errorf("mw prompt run --card: %q is not a card flag: a card takes --title, --item and --bead-channel", rest[0])
	}
	backend, err := promptsBackend()
	if err != nil {
		return err
	}
	cards, err := posternCards(cmd.OutOrStdout())
	if err != nil {
		return err
	}
	_, _, err = application.PromptCard{Prompts: backend, Cards: cards}.Run(cmd.Context(), name,
		application.CardSendRequest{Title: title, Items: items, BeadChannel: beadChannel})
	return err
}
