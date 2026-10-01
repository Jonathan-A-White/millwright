package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
)

// eventsClock stamps an emitted event. A test fixes it.
var eventsClock = time.Now

// newEventsCmd builds `mw events`: the home's sequenced, append-only event
// log, its follower, and the two ways to read and add to it.
func newEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "The home's event log: follow the beads into it, emit a job's event, tail it",
		Long: "events is the home's sequenced, append-only log of the factory's events (docs/events.md):\n" +
			"one JSON event per line in events_log_path (default ~/.local/state/mw/events/log.jsonl),\n" +
			"its head in log.seq beside it. `mw events follow` writes the beads' events into it,\n" +
			"`mw events emit` adds a job's own, and `mw events tail` reads it.",
	}
	cmd.AddCommand(newEventsFollowCmd(), newEventsEmitCmd(), newEventsTailCmd())
	return cmd
}

func newEventsFollowCmd() *cobra.Command {
	var every time.Duration
	cmd := &cobra.Command{
		Use:   "follow",
		Short: "Turn every change to the beads into events, and republish the live view",
		Long: "follow does not exit: every --every (default 1s) it reads the beads' head, Dolt's hash of the\n" +
			"whole database (one `bd sql` call, tokenless). When it has moved, it reads bd's own audit of\n" +
			"the beads and their comments since its cursor (two `bd sql` calls), appends an event for each\n" +
			"change by kind — a status move is bead_changed with from and to, a comment beginning QUESTION\n" +
			"card_asked, ANSWER card_answered, RAN hands_ran, 'The Governor by postern' message, a new mail\n" +
			"bead mail with its box as the detail — saves its cursor (follow.json beside the log), and\n" +
			"writes the sealed live view again, as `mw postern view` does, so a tap of the Governor's\n" +
			"shows within two seconds. Its first run only reads where every bead stands: it appends\n" +
			"nothing. A failure is logged and the loop goes on; SIGTERM or SIGINT stops it. It is what\n" +
			"contrib/systemd/mw-view-follow.service runs; `mw postern view --follow` is the same loop.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEventsFollow(cmd, every)
		},
	}
	cmd.Flags().DurationVar(&every, "every", application.DefaultEventFollowEvery, "how often to read the beads' head")
	return cmd
}

// runEventsFollow runs the follower until SIGTERM or SIGINT: what `mw events
// follow` and `mw postern view --follow` both are.
func runEventsFollow(cmd *cobra.Command, every time.Duration) error {
	if every <= 0 {
		return fmt.Errorf("--every must be a positive duration, like 1s")
	}
	gateway, host, err := posternGateway()
	if err != nil {
		return err
	}
	path, err := config.EventsLogPath()
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
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	return application.EventFollow{
		Head:    gateway,
		Feed:    gateway,
		Log:     eventlog.New(path),
		Cursors: eventlog.NewCursors(path),
		Publish: func(ctx context.Context) error {
			sealed, err := sealedPosternView(view)
			if err != nil {
				return err
			}
			sealed.Out = cmd.OutOrStdout()
			_, err = sealed.Run(ctx)
			return err
		},
		Aside: true,
		Every: every,
		Err:   cmd.ErrOrStderr(),
	}.Run(ctx)
}

func newEventsEmitCmd() *cobra.Command {
	var e events.Event
	cmd := &cobra.Command{
		Use:   "emit",
		Short: "Append one event of a job's own to the home's event log",
		Long: "emit appends one event, stamped now, in the normal lane, and prints the seq it was given.\n" +
			"It is how a scheduled job says it was scheduled, started, finished or failed:\n\n" +
			"  mw events emit --kind job --actor dispatch@laptop --from scheduled --to running\n\n" +
			"--kind is one of docs/events.md's kinds; --from and --to are its machine's states, and an\n" +
			"event its machine forbids is refused before anything is written. --actor defaults to\n" +
			"mw@<host>.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if e.Kind == "" {
				return fmt.Errorf("--kind is needed: one of %v", events.Kinds())
			}
			if e.Actor == "" {
				actor, err := beads.ActorFromConfig()
				if err != nil {
					return err
				}
				e.Actor = actor
			}
			path, err := config.EventsLogPath()
			if err != nil {
				return err
			}
			got, err := application.EventEmit{Log: eventlog.New(path), Now: eventsClock, Event: e}.Run(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), got.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&e.Kind, "kind", "", "the event's kind, e.g. job")
	cmd.Flags().StringVar(&e.Bead, "bead", "", "the bead it is about, if any")
	cmd.Flags().StringVar(&e.From, "from", "", "the state before (empty for the machine's first)")
	cmd.Flags().StringVar(&e.To, "to", "", "the state after")
	cmd.Flags().StringVar(&e.Detail, "detail", "", "what happened: a job's outcome on done or failed")
	cmd.Flags().StringVar(&e.Actor, "actor", "", "who: <job>@<host> for a job (default mw@<host>)")
	return cmd
}

func newEventsTailCmd() *cobra.Command {
	var since uint64
	var follow bool
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Print the home's events, one per line",
		Long: "tail prints every event after --since (default 0, the whole log), one per line: seq, time,\n" +
			"kind, bead, actor, from->to, detail, with \"-\" for an empty bead or actor or a kind with no\n" +
			"machine and \"(start)\" for a machine's first state. --follow keeps printing what is\n" +
			"appended, until SIGTERM or SIGINT.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.EventsLogPath()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, os.Interrupt)
			defer stop()
			return application.EventTail{Log: eventlog.New(path), Since: since, Follow: follow, Out: cmd.OutOrStdout()}.Run(ctx)
		},
	}
	cmd.Flags().Uint64Var(&since, "since", 0, "print only the events after this seq")
	cmd.Flags().BoolVar(&follow, "follow", false, "keep printing what is appended")
	return cmd
}
