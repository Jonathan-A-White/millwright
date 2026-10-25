package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
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
			"`mw events emit` adds a job's own, `mw events tail` reads it, `mw events wait` blocks on it\n" +
			"until a seat's subscribed event comes, and `mw events trim` moves the old events to the archive.",
	}
	cmd.AddCommand(newEventsFollowCmd(), newEventsEmitCmd(), newEventsTailCmd(), newEventsWaitCmd(), newEventsTrimCmd(), newEventsJobsCmd())
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
			"input line is typed one line, \"New events for <seat>: N. Run mw events tail --since <seq>.\" (the mail line\n" +
			"instead when every event is mail), a busy pane is left alone and told on a later pass, and a seat\n" +
			"whose window is down and marked spring = true is brought up (mw deputy, mw millhand). It also runs the jobs the events call for,\n" +
			"each as the unit its timer starts, at most one in flight: a bead opened or landed runs\n" +
			"mw-dispatch.service, an alarm (an emergency-lane event), mail for the Millhand or a failed doctor\n" +
			"mw-millhand-tick.service; each with an [events] heartbeat (default 1h) as fallback; and mw-mail-notify.service\n" +
			"on its own [events] clock (default 5m). Every pass writes job events: scheduled, running, done or failed.\n" +
			"At most every 5 s it also applies the Governor's one-tap actions (Done, release, hold, priority,\n" +
			"keep, verified, thaw, chase answers, To do edits) that came in, as mw postern inbox --apply would, once per\n" +
			"txid and at zero tokens; his typed words, answers and voice notes are left unread for the seat.\n" +
			"It is what\n" +
			"contrib/systemd/mw-view-follow.service runs; `mw postern view --follow` is the same loop.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEventsFollow(cmd, every)
		},
	}
	cmd.Flags().DurationVar(&every, "every", application.DefaultEventFollowEvery, "how often to read the beads' head")
	return cmd
}

// followerNoteWait is how long the follower keeps a note it read from bd before
// it reads it again: the notes it reads every pass (the month's review filed,
// the harness hold, the inbox cursor) change rarely, and a change another
// process makes is seen within this wait.
const followerNoteWait = time.Minute

// followerPublishEvery is the least time between the views of two heads: the
// Governor's phone is not held up by ten seconds, and the beads' server is
// spared the reads of a view for every write.
const followerPublishEvery = 10 * time.Second

// runEventsFollow runs the follower until SIGTERM or SIGINT: what `mw events
// follow` and `mw postern view --follow` both are.
func runEventsFollow(cmd *cobra.Command, every time.Duration) error {
	if every <= 0 {
		return fmt.Errorf("--every must be a positive duration, like 1s")
	}
	// The follower reads the beads' head every second and a few notes on every
	// pass; started as a bd each, those were 8.5 new connections a second to the
	// beads server (mw-gq6.596). The head, the view's reads of every bead, the
	// notes and the comments, and the feed are read over one connection kept
	// open (mw-gq6.600), and a note is read from bd once in followerNoteWait.
	if dir, err := config.Vault(); err == nil {
		defer keepBeadsConnection(dir, cmd.ErrOrStderr(), beads.WithNoteCache(followerNoteWait))()
	} else {
		followerGatewayOptions = []beads.Option{beads.WithNoteCache(followerNoteWait), beads.WithKeptSaid(cmd.ErrOrStderr())}
		defer func() { followerGatewayOptions = nil }()
	}
	gateway, host, err := posternGateway()
	if err != nil {
		return err
	}
	path, err := config.EventsLogPath()
	if err != nil {
		return err
	}
	verifyDays, err := config.PosternVerifyDays()
	if err != nil {
		return err
	}
	cloudBook, cloudPlan := posternCloud()
	view := application.PosternView{
		Tracker:    gateway,
		Notes:      gateway,
		Signs:      posternSigns(),
		Cloud:      cloudBook,
		CloudPlan:  cloudPlan,
		Host:       host,
		Reaches:    handsReaches(host),
		Now:        posternViewClock,
		Err:        cmd.ErrOrStderr(),
		VerifyDays: verifyDays,
		// Its seq: the head of the log this follower writes, and where a folded
		// verify need tells the Mayor.
		Events: eventlog.New(path),
		// Held across the passes, so a beads change that leaves the view as
		// it was does not give the Governor's phone a new file to download.
		Memo: &application.PosternViewMemo{},
	}
	ship, err := eventShip(path, host)
	if err != nil {
		return err
	}
	var shipper application.EventShipper
	var tapper application.EventTapper
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
		ship.ChainRoad = newChain(backend, keys, cmd.ErrOrStderr())
		shipper = ship
		// His one-tap actions are applied here, as they come, so none waits for a
		// seat to run mw postern inbox. Wiring that fails leaves the follower
		// without taps, said once, rather than stopping it.
		if taps, err := posternTaps(cmd.ErrOrStderr()); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "mw events follow: his one-tap actions are left for mw postern inbox: %v\n", err)
		} else {
			tapper = taps
		}
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
		Settle:   application.TypeSettle,
		Inbox:    gateway.Inbox,
	}
	projects, err := claude.DefaultProjectsRoot()
	if err != nil {
		return err
	}
	watcher := &application.SeatContextWatch{
		Limits:      config.SeatHandoffs,
		Dir:         vaultDir,
		Transcripts: claude.NewTranscripts(projects),
		Terminal:    terminal,
		Marks:       eventlog.NewContextMarks(path),
		Settle:      application.TypeSettle,
		Err:         cmd.ErrOrStderr(),
	}
	spring, err := homeSpring(path, host, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	defer spring.Wait()
	controller := &application.EventControl{
		Log:     eventlog.New(path),
		Cursors: eventlog.NewNudgeCursors(path),
		Tracker: gateway,
		Runner:  tmux.New(),
		Host:    host,
		Notes:   gateway,
		Err:     cmd.ErrOrStderr(),
	}
	return application.EventFollow{
		Shipper:    shipper,
		Nudger:     nudger,
		Springer:   spring,
		Watcher:    watcher,
		Tapper:     tapper,
		Controller: controller,
		Trimmer:    eventTrim(path),
		Head:       gateway,
		Feed:       gateway,
		Log:        eventlog.New(path),
		Cursors:    eventlog.NewCursors(path),
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
		// Each head's view is read from the beads over the kept connection, by bd
		// only when it fails; a busy beads moves its head every few seconds
		// (mw-gq6.596, mw-gq6.600).
		PublishEvery: followerPublishEvery,
		Every:        every,
		Err:          cmd.ErrOrStderr(),
	}.Run(ctx)
}

// eventTrim is the trim of the log in the file path: its readers' cursors and
// the shipper's state beside it, its archive in the directory beside it.
func eventTrim(path string) application.EventTrim {
	log := eventlog.New(path)
	return application.EventTrim{
		Log:     log,
		Archive: log,
		Nudges:  eventlog.NewNudgeCursors(path),
		Ship:    eventlog.NewShipStates(path),
	}
}

// homeEventLog is this host's event log for a command that only reads it (a
// pause for dispatch, the cancelled runs for status) or adds one event to it
// (a hold's cancel). A path that cannot be settled is no log: the command goes
// on without one, as it did before the log existed.
func homeEventLog() application.EventLog {
	path, err := config.EventsLogPath()
	if err != nil {
		return nil
	}
	return eventlog.New(path)
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
		Log:               eventlog.New(path),
		State:             eventlog.NewShipStates(path),
		Chain:             knobs.Chain,
		DailyCap:          knobs.ChainDailyCap,
		EmergencyDailyCap: knobs.EmergencyDailyCap,
		Host:              host,
	}, nil
}

func newEventsEmitCmd() *cobra.Command {
	var e events.Event
	var emergency bool
	cmd := &cobra.Command{
		Use:   "emit",
		Short: "Append one event of a job's own to the home's event log",
		Long: "emit appends one event, stamped now, in the normal lane, and prints the seq it was given.\n" +
			"It is how a scheduled job says it was scheduled, started, finished or failed:\n\n" +
			"  mw events emit --kind job --actor dispatch@laptop --from scheduled --to running\n\n" +
			"--kind is one of docs/events.md's kinds; --from and --to are its machine's states, and an\n" +
			"event its machine forbids is refused before anything is written. --actor defaults to\n" +
			"mw@<host>.\n\n" +
			"--kind control carries a word to the factory, in --detail (docs/events.md, \"Control\"): cancel (with --bead:\n" +
			"the follower ends that story's session and holds it), pause-host <host> and resume-host <host> (dispatch\n" +
			"passes on that host stop and start), cap <host> <n>, priority <n> (with --bead):\n\n" +
			"  mw events emit --kind control --detail \"pause-host laptop\"\n\n" +
			"--emergency puts the event in the emergency lane: the follower sends it alone, as a record of one\n" +
			"event, on chain and direct at once, ahead of the pending batches and of the 2 s window, and mw status\n" +
			"counts it among today's emergencies. An emergency ends with an event in the normal lane whose --clears is its\n" +
			"seq (the number emit printed): the Governor's app takes that banner down.\n\n" +
			"  mw events emit --kind job --from running --to done --clears 41",
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
			got, err := application.EventEmit{Log: eventlog.New(path), Now: eventsClock, Event: e, Emergency: emergency}.Run(cmd.Context())
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
	cmd.Flags().Uint64Var(&e.Clears, "clears", 0, "the seq of the emergency this event ends")
	cmd.Flags().BoolVar(&emergency, "emergency", false, "send it alone and at once, ahead of the batches (the emergency lane)")
	return cmd
}

// newEventsJobsCmd builds `mw events jobs`: the jobs the follower runs on this
// host, and what springs each.
func newEventsJobsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "List the jobs the follower runs on this host, and what springs each",
		Long: "jobs lists, one to a line, the jobs `mw events follow` runs here: dispatch, millhand-tick, mail-notify,\n" +
			"the grist jobs, chain-stamp, the To do jobs, the nightly suite, sleep, batch-land, contribute-back\n" +
			"and the cloud job, each with what springs it: events, a check each pass, a watch, or a clock.\n" +
			"A job whose unit is not installed here is not run, and is not listed; it is said on stderr. It\n" +
			"reads the config and starts nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			host, err := config.Host()
			if err != nil {
				return err
			}
			path, err := config.EventsLogPath()
			if err != nil {
				return err
			}
			spring, err := homeSpring(path, host, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			application.WriteEventJobs(cmd.OutOrStdout(), spring.Jobs)
			return nil
		},
	}
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
			"appended, until SIGTERM or SIGINT. A --since older than the log's first kept event reads the\n" +
			"older ones from the archive (mw events trim).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.EventsLogPath()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, os.Interrupt)
			defer stop()
			return application.EventTail{Log: eventlog.New(path), Archive: eventlog.New(path), Since: since, Follow: follow, Out: cmd.OutOrStdout()}.Run(ctx)
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
	var seat, window string
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
			"event is the seat's only when its box is the seat. With mail among the kinds and no --since,\n" +
			"it reads the seat's box once first and, if mail is unread there, prints \"New events for <seat>:\n" +
			"unread mail in its box (N). Run bd mail inbox.\" and exits 0 at once. With no event by --limit (default 50m) it says\n" +
			"so and exits 0: arm it again.\n\n" +
			"--kinds is a list of " + strings.Join(application.SubscribableKinds(), ", ") + " (hyphens may stand for underscores;\n" +
			"landing is a bead_changed to landed). Without it, the kinds are those of seats/<seat>/subscribe.toml\n" +
			"in the vault, or mail alone when the seat has none. That file is also what mw events follow reads to tell\n" +
			"the seat in its pane, and to spring it:\n\n" +
			"  kinds = [\"mail\", \"landing\", \"card_answered\", \"message\"]\n" +
			"  spring = true   # deputy and millhand only: bring the seat up when its window is down\n\n" +
			"A kind that is none of these is refused, with the kinds listed.\n\n" +
			"A handover of the seat (mw seat handover) ends the wait whatever the kinds. In the old session's\n" +
			"window it prints the events up to N, then 'handed over at N: answer nothing after event N'; in the\n" +
			"successor's window, whose name is --as (default: this tmux window's), 'You hold the seat from event N'.\n" +
			"A wait begun --since N or later ignores a handover that marked N. A wait with handover among its kinds\n" +
			"and no --since looks back first: when the seat's newest handover in the log is to its own window (--as),\n" +
			"it prints 'You hold the seat from event N' and exits 0 at once, the handover having landed before it\n" +
			"was armed, if the seat's acting file names that window at the handover's event (a handover the file\n" +
			"does not stand at, the old session still acting, is stale: mw-gq6.767); a newest handover to another\n" +
			"window, or none, leaves it to wait as above.",
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
			wait := application.EventWait{Log: eventlog.New(path), Subscription: sub, Self: window, Limit: limit, Out: cmd.OutOrStdout()}
			if !cmd.Flags().Changed("since") {
				// A vault or host that cannot be named leaves the box unread: the wait is as it was.
				if dir, err := config.Vault(); err == nil {
					if host, err := config.Host(); err == nil {
						wait.Box = mwGateway(dir, host)
					}
				}
			}
			if window == "" {
				wait.Self = thisWindowName(cmd.Context())
			}
			if cmd.Flags().Changed("since") {
				wait.Since = &since
			} else if dir, err := config.Vault(); err == nil {
				// The look-back before arming reads the acting file: a handover to this window
				// that the file does not stand at is stale. An unreadable file reads as none.
				text, _ := os.ReadFile(filepath.Join(dir, application.ActingFileName(seat)))
				acting := string(text)
				wait.Acting = &acting
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, os.Interrupt)
			defer stop()
			_, err = wait.Run(ctx)
			return err
		},
	}
	cmd.Flags().StringVar(&seat, "for", "", "the seat that waits")
	cmd.Flags().StringVar(&window, "as", "", "the name of the window this wait runs in, to tell a handover to it from one away from it (default: this tmux window's)")
	cmd.Flags().StringSliceVar(&kinds, "kinds", nil, "the kinds that end the wait, comma separated (default: the seat's subscribe.toml)")
	cmd.Flags().Uint64Var(&since, "since", 0, "count the events after this seq, so those already in the log end the wait at once")
	cmd.Flags().DurationVar(&limit, "limit", application.DefaultEventWaitLimit, "how long to wait before saying nothing came")
	return cmd
}

func newEventsTrimCmd() *cobra.Command {
	var keep uint64
	cmd := &cobra.Command{
		Use:   "trim [--keep N]",
		Short: "Move the log's old events into a dated archive file, so the log stays small",
		Long: "trim moves every event that every reader is past from log.jsonl to archive/log-<date>.jsonl beside it\n" +
			"(the UTC date of the trim), keeping the log's newest --keep events (default 5000) whatever the readers\n" +
			"say. An event is past when its seq is at or below the lowest of the seats' nudge cursors and the control\n" +
			"cursor (nudge.json), at or below the last seq the shipper has sent (ship.json), and before the first batch\n" +
			"still waiting for the chain. Seq numbering is unchanged. `mw events follow` does this itself, once a UTC day;\n" +
			"`mw events tail --since N` reads the archive when N is older than the log's first kept event.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.EventsLogPath()
			if err != nil {
				return err
			}
			trim := eventTrim(path)
			trim.Keep, trim.Out = keep, cmd.OutOrStdout()
			_, err = trim.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().Uint64Var(&keep, "keep", application.DefaultEventKeep, "how many of the newest events stay in the log")
	return cmd
}
