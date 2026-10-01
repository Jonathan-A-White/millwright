package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
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
			"`mw events emit` adds a job's own, `mw events tail` reads it, and `mw events wait` blocks on it\n" +
			"until a seat's subscribed event comes.",
	}
	cmd.AddCommand(newEventsFollowCmd(), newEventsEmitCmd(), newEventsTailCmd(), newEventsWaitCmd())
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
			"nothing. Each pass also seals the events not yet sent as one `events` record to the Governor's\n" +
			"key (docs/events.md, \"The batch\"), at most one batch every 2 s or 50 events at once, and puts\n" +
			"it on chain and delivers it direct in the same pass. With the chain unreachable, off ([events]\n" +
			"chain = false) or past [events] chain_daily_cap records today, it goes direct only, in the\n" +
			"fallback lane, and is put on chain later, oldest first. A failure is logged and the loop goes\n" +
			"on; SIGTERM or SIGINT stops it. Each pass also tells the seats of the events they subscribed to\n" +
			"(seats/<seat>/subscribe.toml, see `mw events wait --help`): a seat whose window is up and idle at an empty\n" +
			"input line is typed \"New events for <seat>: N. Run mw events tail --since <seq>.\" (and the mail line when mail\n" +
			"is among them), a busy pane is left alone and told on a later pass, and a seat whose window is down and\n" +
			"marked spring = true is brought up (mw deputy, mw millhand). It is what\n" +
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
	ship, err := eventShip(path, host)
	if err != nil {
		return err
	}
	var shipper application.EventShipper
	if keys, err := posternKeys(); err != nil {
		return err
	} else if exists, err := keys.Exists(); err != nil {
		return err
	} else if !exists {
		fmt.Fprintf(cmd.ErrOrStderr(), "mw events follow: no postern key at %s, so no event is sent (mw postern key init)\n", keys.Path())
	} else {
		backend, err := posternBackend(keys)
		if err != nil {
			return err
		}
		if ship.GovernorKey, err = config.PosternGovernorKey(); err != nil {
			return err
		}
		ship.Postern, ship.Cipher, ship.Keys, ship.Err = backend, posternCipher(keys), keys, cmd.ErrOrStderr()
		shipper = ship
	}
	vaultDir, err := config.Vault()
	if err != nil {
		return err
	}
	vlt := vault.New(vaultDir)
	terminal := seatWindows()
	nudger := &application.EventNudge{
		Log:      eventlog.New(path),
		Subs:     vlt,
		Cursors:  eventlog.NewNudgeCursors(path),
		Terminal: terminal,
		Seats:    vlt,
		Spring:   springSeat,
		Host:     host,
		Err:      cmd.ErrOrStderr(),
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	return application.EventFollow{
		Shipper: shipper,
		Nudger:  nudger,
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

// eventShip is the shipper for the log in the file path, with the log, its
// state and the [events] knobs but no road to the backend: enough to read
// where sending stands, and what runEventsFollow fills in to send.
func eventShip(path, host string) (*application.EventShip, error) {
	knobs, err := config.Events()
	if err != nil {
		return nil, err
	}
	return &application.EventShip{
		Log:      eventlog.New(path),
		State:    eventlog.NewShipStates(path),
		Chain:    knobs.Chain,
		DailyCap: knobs.ChainDailyCap,
		Host:     host,
	}, nil
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

// springSeat brings up a seat whose window is down, as its own command does,
// for the reason given: the follower's spring. A seat whose window turned out
// to be up already is not a failure.
func springSeat(ctx context.Context, seat, reason string) error {
	var err error
	switch seat {
	case application.DeputySeat:
		_, err = bringUpDeputy(ctx, reason, io.Discard)
	case application.MillhandSeat:
		_, err = bringUpMillhand(ctx, application.WakeHand, reason, io.Discard)
	default:
		return fmt.Errorf("the %s seat has no up command to spring it with", seat)
	}
	if _, up := application.DeputyIsUp(err); up {
		return nil
	}
	if _, up := application.MillhandIsUp(err); up {
		return nil
	}
	return err
}

func newEventsWaitCmd() *cobra.Command {
	var seat string
	var kinds []string
	var since uint64
	var limit time.Duration
	cmd := &cobra.Command{
		Use:   "wait --for <seat> [--kinds k1,k2] [--since N] [--limit 50m]",
		Short: "Block until an event the seat subscribed to is in the log, print it and exit",
		Long: "wait blocks, at zero tokens, until the home's log holds an event of one of the --kinds, then prints\n" +
			"\"New events for <seat>: N. Run mw events tail --since <seq>.\" and one line per matching event (the\n" +
			"lines mw events tail prints) and exits 0, so a seat's harness, running it in the background, wakes\n" +
			"on its exit. While it waits it looks at the log's head once a second and calls no bd. Only events\n" +
			"after the log's head when it began count unless --since names a seq, after which they do; a mail\n" +
			"event is the seat's only when its box is the seat. With no event by --limit (default 50m) it says\n" +
			"so and exits 0: arm it again.\n\n" +
			"--kinds is a list of " + strings.Join(application.SubscribableKinds(), ", ") + " (hyphens may stand for underscores;\n" +
			"landing is a bead_changed to landed). Without it, the kinds are those of seats/<seat>/subscribe.toml\n" +
			"in the vault, or mail alone when the seat has none. That file is also what mw events follow reads to tell\n" +
			"the seat in its pane, and to spring it:\n\n" +
			"  kinds = [\"mail\", \"landing\", \"card_answered\", \"message\"]\n" +
			"  spring = true   # deputy and millhand only: bring the seat up when its window is down\n\n" +
			"A kind that is none of these is refused, with the kinds listed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if seat == "" {
				return fmt.Errorf("--for is needed: the seat that waits, like mayor or deputy")
			}
			sub := application.Subscription{Seat: seat}
			var err error
			if len(kinds) > 0 {
				if sub.Kinds, err = application.ParseKinds(kinds); err != nil {
					return err
				}
			} else {
				dir, err := config.Vault()
				if err != nil {
					return err
				}
				file, found, err := application.ReadSubscription(cmd.Context(), vault.New(dir), seat)
				if err != nil {
					return err
				}
				sub.Kinds = []string{events.KindMail}
				if found {
					sub = file
				}
			}
			path, err := config.EventsLogPath()
			if err != nil {
				return err
			}
			wait := application.EventWait{Log: eventlog.New(path), Subscription: sub, Limit: limit, Out: cmd.OutOrStdout()}
			if cmd.Flags().Changed("since") {
				wait.Since = &since
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, os.Interrupt)
			defer stop()
			_, err = wait.Run(ctx)
			return err
		},
	}
	cmd.Flags().StringVar(&seat, "for", "", "the seat that waits")
	cmd.Flags().StringSliceVar(&kinds, "kinds", nil, "the kinds that end the wait, comma separated (default: the seat's subscribe.toml)")
	cmd.Flags().Uint64Var(&since, "since", 0, "count the events after this seq, so those already in the log end the wait at once")
	cmd.Flags().DurationVar(&limit, "limit", application.DefaultEventWaitLimit, "how long to wait before saying nothing came")
	return cmd
}
