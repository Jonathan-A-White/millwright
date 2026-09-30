package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
)

// newHandsCmd builds `mw hands`: the steps only the Governor's hands could
// take, written down for him to approve and the factory to run (postern's
// docs/protocol.md §17).
func newHandsCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "hands",
		Short: "Write down the steps only the Governor's hands can take, for him to approve in Postern",
		Args:  cobra.NoArgs,
	}
	root.AddCommand(newHandsAddCmd())
	root.AddCommand(newHandsListCmd())
	return root
}

// handsClock stamps a step mw hands add writes down. A test fixes it.
var handsClock = time.Now

// newHandsPush is the sender mw hands add pushes through: the very
// application.PosternSend mw postern send builds, set up when a push is sent
// so that a rig without a postern key still adds its step. A test swaps it.
var newHandsPush = func(gateway *beads.Gateway) application.PosternSender {
	return handsPush{gateway: gateway}
}

type handsPush struct{ gateway *beads.Gateway }

func (p handsPush) Run(ctx context.Context, req application.PosternSendRequest) (string, error) {
	keys, err := posternKeys()
	if err != nil {
		return "", err
	}
	governorKey, err := config.PosternGovernorKey()
	if err != nil {
		return "", err
	}
	floatSats, err := config.PosternFloatSats()
	if err != nil {
		return "", err
	}
	channel, err := config.PosternChannel()
	if err != nil {
		return "", err
	}
	backend, err := posternBackend(keys)
	if err != nil {
		return "", err
	}
	return application.PosternSend{
		Postern:     backend,
		Cipher:      posternCipher(keys),
		Keys:        keys,
		Tracker:     p.gateway,
		Notes:       p.gateway,
		GovernorKey: governorKey,
		FloatSats:   int64(floatSats),
		Channel:     channel,
		Now:         posternClock,
	}.Run(ctx, req)
}

// handsViewLockDir and handsViewLockFile are the mail notifier's lock, under
// the home directory: the file its tick flocks for as long as it runs, and so
// the one mw hands add takes, without waiting, around its own view.
const (
	handsViewLockFile = "lock"
)

var handsViewLockDir = filepath.Join(".local", "state", "mw-mail-notify")

// newHandsViewLock is the notifier's lock, nil on a host with no home
// directory. A test swaps it.
var newHandsViewLock = func() application.ViewLock {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return hostlock.NewTry(filepath.Join(home, handsViewLockDir), handsViewLockFile)
}

// newHandsView is the publisher mw hands add refreshes the live view through:
// the very view mw postern view writes, set up when it is published so that a
// rig with no postern key still adds its step. A test swaps it.
var newHandsView = func(gateway *beads.Gateway, host string) application.PosternViewPublisher {
	return handsView{gateway: gateway, host: host}
}

type handsView struct {
	gateway *beads.Gateway
	host    string
}

func (v handsView) Run(ctx context.Context) (application.PosternViewDoc, error) {
	view, err := sealedPosternView(application.PosternView{
		Tracker: v.gateway,
		Notes:   v.gateway,
		Host:    v.host,
		Now:     posternViewClock,
	})
	if err != nil {
		return application.PosternViewDoc{}, err
	}
	return view.Run(ctx)
}

// newHandsAddCmd builds `mw hands add`.
func newHandsAddCmd() *cobra.Command {
	var id, host, as, wayBack string
	var after []string
	var replace, noPush, noView bool

	cmd := &cobra.Command{
		Use:   "add <bead> --id <id> --host <host> --as user|root [--way-back '<commands>'] [--replace] [--after <bead>]... -- '<commands>'",
		Short: "Add a step for the Governor's hands to a bead",
		Long: "add writes down one step only the Governor's hands could take — what a `!` line used to\n" +
			"ask him to type — on <bead>: kept in the bead's hands note (hands.<bead>), commented on\n" +
			"the bead exactly as it will run, and the bead labelled hitl, so Postern shows it under\n" +
			"his hands. Nothing runs until he approves it there with his key; then this factory's\n" +
			"host runs it on --host, as the host's own user or, --as root, through mw-hands-root.\n\n" +
			"The commands are one argument after --, quoted. --way-back says how to undo them. An id\n" +
			"is used once per bead: --replace changes a step already there, which changes its hash\n" +
			"and so voids any approval of the old one.\n\n" +
			"Once the step is kept, one Postern message is sent to the Governor on the bead's thread,\n" +
			"whose tap opens Needs you. A push that fails is said on stderr and on the bead, and the\n" +
			"step stays: mw hands add still exits 0. --no-push sends none.\n\n" +
			"Then the live view is published, the same as mw postern view, so his phone shows the\n" +
			"step, or a replaced step's new hash, at once. It takes the mail notifier's lock\n" +
			"(~/.local/state/mw-mail-notify/lock) without waiting: while the notifier's tick holds it,\n" +
			"the view is skipped with a warning on stderr, and its next tick shows the step. A view\n" +
			"that fails is said on stderr too, and the step stays. --no-view publishes none.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("mw hands add takes the bead and then, after --, the commands as one quoted argument; got %d argument(s)", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, viewHost, err := posternGateway()
			if err != nil {
				return err
			}
			_, err = application.HandsAdd{
				Tracker: gateway,
				Notes:   gateway,
				Now:     handsClock,
				Out:     cmd.OutOrStdout(),
				Push:    newHandsPush(gateway),
				NoPush:  noPush,
				Err:     cmd.ErrOrStderr(),

				View:     newHandsView(gateway, viewHost),
				ViewLock: newHandsViewLock(),
				NoView:   noView,
			}.Run(cmd.Context(), application.HandsAddRequest{
				Bead:    args[0],
				Step:    domain.HandsStep{ID: id, Host: host, As: as, Run: args[1], WayBack: wayBack},
				Replace: replace,
				After:   after,
			})
			return err
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "the step's id, unique on the bead (required)")
	cmd.Flags().StringVar(&host, "host", "", "the host the step runs on (required)")
	cmd.Flags().StringVar(&as, "as", "", "who the step runs as: user or root (required)")
	cmd.Flags().StringVar(&wayBack, "way-back", "", "the commands that undo the step")
	cmd.Flags().BoolVar(&replace, "replace", false, "replace the bead's step of this id, voiding any approval of it")
	cmd.Flags().BoolVar(&noPush, "no-push", false, "do not send the Governor a Postern push about the step")
	cmd.Flags().BoolVar(&noView, "no-view", false, "do not publish the live view once the step is kept")
	cmd.Flags().StringArrayVar(&after, "after", nil, "a bead that must finish first; it blocks this bead (repeatable)")
	for _, name := range []string{"id", "host", "as"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

// newHandsListCmd builds `mw hands list`.
func newHandsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <bead>",
		Short: "List a bead's steps for the Governor's hands",
		Long: "list prints every hands step of <bead>: its id, host and who it runs as, its §17\n" +
			"sha256 (what the Governor's approval binds), whether it has run and how, its commands\n" +
			"and its way back. It reads and writes nothing else.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			_, err = application.HandsList{Notes: gateway, Out: cmd.OutOrStdout()}.Run(cmd.Context(), args[0])
			return err
		},
	}
}
