package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/codex"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
	"github.com/Jonathan-A-White/millwright/infrastructure/reaper"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/shellcmd"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newSeatCmd builds `mw seat`: the commands about a seat's own session. It has
// no behaviour of its own; each subcommand is one thing to do with it.
func newSeatCmd() *cobra.Command {
	seat := &cobra.Command{
		Use:   "seat",
		Short: "Work with a seat's own session",
		Long:  "seat holds the commands about a seat's live session, as opposed to the story it is working.",
		Args:  cobra.NoArgs,
	}
	seat.AddCommand(newSeatContextCmd())
	seat.AddCommand(newSeatUpCmd())
	seat.AddCommand(newSeatReapCmd())
	seat.AddCommand(newSeatHandoverCmd())
	seat.AddCommand(newSeatTakeCmd())
	seat.AddCommand(newSeatBootCmd())
	seat.AddCommand(newSeatReadCmd())
	return seat
}

// TmuxSocketEnv names the tmux server the seat commands work on, tmux's -L. Unset,
// they work on tmux's default server: the one the person's windows are in. It
// exists so that a test can run them against a server of its own.
const TmuxSocketEnv = "MW_TMUX_SOCKET"

// seatWindows is the terminal the seat commands work on.
func seatWindows() *tmux.Windows {
	return tmux.NewWindows(tmux.WithSocket(os.Getenv(TmuxSocketEnv)))
}

// newSeatUpCmd builds `mw seat up`: the seat's next session, started in a
// window of its own. It is the one command here that starts anything.
func newSeatUpCmd() *cobra.Command {
	var model, effort, reason string
	var reapWhenIdle, attended bool
	cmd := &cobra.Command{
		Use:   "up <seat>",
		Short: "Start a seat's next session in a window of its own",
		Long: "up starts one interactive Claude Code session for the seat, in a new window of the tmux\n" +
			"session mw is running in — or of a detached session named " + tmux.SeatsSession + ", when mw is running\n" +
			"outside tmux. The window is named <seat>-<UTC date>-<nn>, with nn one past the highest\n" +
			"number the seat has used for a handoff or a window.\n\n" +
			"The session is primed with the seat's charter and told the seat's own kickoff text\n" +
			"(seats/<seat>/kickoff.md), or a default when it keeps none, ending with the newest handoff\n" +
			"to boot from and the --reason it was started for. Nothing else the vault holds is read or\n" +
			"passed: no ledger, no memory of a rig, no prime from the tracker.\n\n" +
			"Handoffs are read from seats/<seat>/hosts/<host>/handoffs/ for a seat that keeps a directory\n" +
			"for this host, and from seats/<seat>/handoffs/ for one that does not.\n\n" +
			"It refuses and starts nothing when the seat has no charter, has written no handoff, or is\n" +
			"already acting in a window that is still open and has written no handoff since it opened.\n" +
			"mw never writes the acting file: the new session writes it at boot, and that is how the\n" +
			"seat is handed over.\n\n" +
			"Run from a tmux window of the seat's (named <seat>-...) that its acting file names, it also arms\n" +
			"`mw seat reap` on that window, so that the outgoing session's window closes itself once its\n" +
			"successor has the seat; from any other window it says it is not the seat's and arms nothing.\n" +
			"--reap-when-idle arms one in idle mode on the window it opens, for a session that hands over to\n" +
			"nobody.\n\n" +
			"Nobody is assumed to be watching: the session is given a PermissionRequest hook that denies a\n" +
			"permission it would otherwise ask for, so it never hangs on a prompt (the denial is in its\n" +
			"transcript). --attended, for a seat brought up by hand to talk to, leaves the hook out and the\n" +
			"session asks as Claude Code does.\n\n" +
			"On a host whose config.toml says [next] harness = \"codex\" the session is an interactive Codex\n" +
			"instead, in the vault, which reaches the charter by the AGENTS.md at its root (mw member init\n" +
			"--harness codex lays it): codex -m <model> -c model_reasoning_effort=<effort> --sandbox\n" +
			"workspace-write -c sandbox_workspace_write.network_access=true -a never, or -a on-request with\n" +
			"--attended. With no --model or --effort it runs " + string(application.MemberCodexModel) + " at " + string(application.MemberCodexEffort) + ".",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}

			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("finding the mw that is running, to arm a reaper with: %w", err)
			}

			harness, model, effort := seatHarness(dir, host, domain.Model(model), domain.Effort(effort))
			windows := seatWindows()
			_, err = application.SeatUp{
				Seats:   vault.New(dir),
				Windows: windows,
				Harness: harness,
				Seat:    args[0],
				Host:    host,
				Model:   model,
				Effort:  effort,
				Reason:  reason,
				Out:     cmd.OutOrStdout(),

				Attended:     attended,
				Terminal:     windows,
				Armer:        reaper.New(exe),
				ReapWhenIdle: reapWhenIdle,
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&reapWhenIdle, "reap-when-idle", false, "close the new window once a handoff newer than it exists and its pane is idle")
	cmd.Flags().BoolVar(&attended, "attended", false, "a person is at the keyboard: the session asks about permissions instead of denying them")
	cmd.Flags().StringVar(&model, "model", "", "the model the session runs on (default: the harness's own)")
	cmd.Flags().StringVar(&effort, "effort", "", "how hard the session is asked to think (default: the harness's own)")
	cmd.Flags().StringVar(&reason, "reason", "", "why the session is being started, told to it after the kickoff")
	return cmd
}

// seatHarness is the harness a seat's own session is started with: Codex on a
// host whose next.harness says codex, and Claude Code otherwise. A Codex seat
// with no model or effort given runs the member's own (application.MemberCodexModel,
// at MemberCodexEffort), since Codex has no alias of Claude's to leave to its
// default; a model given is checked by the harness.
func seatHarness(dir, host string, model domain.Model, effort domain.Effort) (application.SeatHarness, domain.Model, domain.Effort) {
	if hostHarnessName() != domain.HarnessCodex {
		return claudeHarness(dir, host, sessionTuning{}), model, effort
	}
	if model == "" {
		model = application.MemberCodexModel
	}
	if effort == "" {
		effort = application.MemberCodexEffort
	}
	opts := []codex.Option{codex.WithEnvFile(beadsEnvFile())}
	if server := sessionServerHost(dir, host); server != "" {
		opts = append(opts, codex.WithBeadsServerHost(server))
	}
	return codex.New(opts...), model, effort
}

// newSeatReapCmd builds `mw seat reap`: the watcher that closes a finished
// session's window. It is zero-token and meant to be started detached, by `mw
// seat up`; it is a command of its own so that it can also be run by hand.
func newSeatReapCmd() *cobra.Command {
	var window string
	var whenIdle bool
	var interval, limit time.Duration
	cmd := &cobra.Command{
		Use:   "reap <seat> --window <tmux window id>",
		Short: "Close a finished session's window once it is done",
		Long: "reap watches one tmux window and closes it when the session in it is finished: a session ends its\n" +
			"own window, and no session ever closes another's. It costs no tokens and runs until the window is\n" +
			"closed or the limit is up.\n\n" +
			"In successor mode, the default, it closes the window once the seat's acting file (.<seat>-acting in\n" +
			"the vault) names someone else than it did when the watch was armed, that someone's window is open,\n" +
			"and the window's pane is idle. With --when-idle, for a session with nobody to hand over to, it closes\n" +
			"the window once a handoff newer than the window exists and its pane is idle. Idle means an empty\n" +
			"input line with nothing running, or holding only unsent text with no turn running, on two looks in a\n" +
			"row, in either mode. The Deputy's window, with --when-idle, is not closed over unread mail in the\n" +
			"Deputy's box: the reaper types the mail nudge into the idle pane, logs it, and waits for a handoff\n" +
			"written after it; a mail count that fails closes nothing.\n\n" +
			"A pane that shows a background agent still running (\"Waiting for 1 background agent to finish\") is\n" +
			"kept for up to 30 minutes, the reaper saying that it waits, and closed after that with the line it\n" +
			"cut in the log.\n\n" +
			"It looks every --interval (30s) and gives up after --limit (3h), closing nothing and saying so. Arming,\n" +
			"closing and giving up each append one dated line to .<seat>-reaper.log in the vault. It works on\n" +
			"tmux's default server unless $" + TmuxSocketEnv + " names another.\n\n" +
			"It exits non-zero when it gave up or could not close the window.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}

			files := vault.New(dir)
			reap := application.SeatReap{
				Seats:    files,
				Terminal: seatWindows(),
				Log:      files,
				Events:   homeEventLog(),
				Seat:     args[0],
				Host:     host,
				Window:   window,
				WhenIdle: whenIdle,
				Settle:   application.TypeSettle,
				Interval: interval,
				Limit:    limit,
				Out:      cmd.OutOrStdout(),
			}
			// The Deputy's window is not closed over mail waiting in its box.
			if args[0] == application.DeputySeat {
				reap.Mail = mwGateway(dir, host)
				reap.Mailbox = application.DeputyMailbox
			}
			_, err = reap.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&window, "window", "", "the tmux window to close, by id (like @3)")
	cmd.Flags().BoolVar(&whenIdle, "when-idle", false, "close once a handoff newer than the window exists and its pane is idle, with no successor needed")
	cmd.Flags().DurationVar(&interval, "interval", application.DefaultReapInterval, "how long between looks")
	cmd.Flags().DurationVar(&limit, "limit", application.DefaultReapLimit, "how long to keep looking before giving up")
	return cmd
}

// thisWindowName is the name of the tmux window the calling process runs in,
// empty when it runs in none or tmux cannot say: how a wait knows whether a
// handover is to its own window.
func thisWindowName(ctx context.Context) string {
	here, in, err := seatWindows().ThisWindow(ctx)
	if err != nil || !in {
		return ""
	}
	return here.Name
}

// newSeatHandoverCmd builds `mw seat handover`: the old Mayor's last command,
// which marks event N of the log and hands the seat to the successor already up.
func newSeatHandoverCmd() *cobra.Command {
	var at uint64
	var to string
	cmd := &cobra.Command{
		Use:   "handover [--at <seq>] [--to <window>]",
		Short: "Hand the Mayor's seat to the successor that is already up, at event N of the log",
		Long: "handover is what the old Mayor runs last, once the successor (bin/respawn-mayor) is up beside it.\n" +
			"It marks event N of the home's event log, --at (default: the log's head), the last event the old\n" +
			"Mayor answers. It emits a handover event, and writes the successor's window name and N into the\n" +
			"acting file (.mayor-acting in the vault), which is what the reaper bin/respawn-mayor armed on the\n" +
			"old window waits for before it closes it.\n\n" +
			"The successor is --to, a window name, or by default the newest other window of the Mayor. From\n" +
			"the event on, mw events wait --for mayor and mw talk wait in the old window end at once with\n" +
			"'handed over at N', and the old Mayor answers nothing after N; the successor's mw events wait,\n" +
			"begun before it, ends with 'You hold the mayor seat from event N', and its waits then read from\n" +
			"N (mw events wait --since N; mw talk wait, from the talk cursor), so nothing the Governor sends\n" +
			"falls between the two. It works on tmux's default server unless $" + TmuxSocketEnv + " names another.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			path, err := config.EventsLogPath()
			if err != nil {
				return err
			}
			req := application.SeatHandoverRequest{Successor: to}
			if cmd.Flags().Changed("at") {
				req.At = &at
			}
			_, err = application.SeatHandover{
				Seat:     application.MayorSeat,
				Log:      eventlog.New(path),
				Terminal: seatWindows(),
				Acting:   vault.New(dir),
				Now:      eventsClock,
				Out:      cmd.OutOrStdout(),
			}.Run(cmd.Context(), req)
			return err
		},
	}
	cmd.Flags().Uint64Var(&at, "at", 0, "the seq of the last event the old Mayor answers (default: the log's head)")
	cmd.Flags().StringVar(&to, "to", "", "the successor's window name (default: the newest other window of the Mayor)")
	return cmd
}

// newSeatTakeCmd builds `mw seat take`: the acting file for a seat taken
// without a handover, written by the session that took it.
func newSeatTakeCmd() *cobra.Command {
	var as, note string
	cmd := &cobra.Command{
		Use:   "take <seat> [--as <name>] [--note <text>]",
		Short: "Write the acting file for a seat taken without a handover, naming this window and session",
		Long: "take is what a session runs, in its own tmux pane, when it takes a seat with no handover from a\n" +
			"predecessor: after bin/mayor-up started it, or after the last Mayor's window was closed. It writes the\n" +
			"acting file (.<seat>-acting in the vault) naming the session, the id of the window it runs in (which mw\n" +
			"doctor mayor-gone finds the window by) and its own session id (from $" + application.SessionEnv + "). Writing the\n" +
			"file by hand left out the window and the session, and the doctor then reported the Mayor gone.\n\n" +
			"bin/mayor-up calls its window after the handoff it booted from, not after the Mayor in it. --as <name>,\n" +
			"which must begin with <seat>-, renames this window to the name the session is to have and writes that\n" +
			"name; without it the window keeps its name, which must already begin with <seat>-. --note adds free text\n" +
			"to the file (where the seat was taken from, and why). Where a predecessor is alive, wait for its handover\n" +
			"instead: two sessions of a seat never act at once. It works on tmux's default server unless $" + TmuxSocketEnv + "\n" +
			"names another.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			return application.SeatTake{
				Seat:     args[0],
				Terminal: seatWindows(),
				Acting:   vault.New(dir),
				Session:  os.Getenv(application.SessionEnv),
				Now:      eventsClock,
				Out:      cmd.OutOrStdout(),
			}.Run(cmd.Context(), application.SeatTakeRequest{Name: as, Note: note})
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "the name this session and its window are to have (default: the window's own)")
	cmd.Flags().StringVar(&note, "note", "", "free text kept in the acting file, such as where the seat was taken from")
	return cmd
}

// newSeatContextCmd builds `mw seat context`: how full a seat's live session
// is against its handoff limit. It is read-only and zero-token: it reads a
// transcript and starts and writes nothing.
func newSeatContextCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Print a seat session's live context against its handoff limit",
		Long: "context prints one line: `context=<n> handoff_at=<limit> ok|handoff session=<id>`. n is the\n" +
			"size of the context the last assistant turn of the newest Claude Code transcript for the\n" +
			"directory was given: its input, cache-read and cache-creation tokens together. The directory\n" +
			"is the vault from config unless --dir names another. The limit is config `handoff_at` (or\n" +
			"$MW_HANDOFF_AT), default 180000, and the line says handoff at or above it.\n\n" +
			"Both ok and handoff exit 0. A directory with no transcript, or a transcript with no\n" +
			"assistant turn yet, is an error that says where it looked. Nothing is started, written or\n" +
			"spent: context only reads.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				vault, err := config.Vault()
				if err != nil {
					return err
				}
				dir = vault
			}
			where, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			limit, err := config.HandoffAt()
			if err != nil {
				return err
			}
			root, err := claude.DefaultProjectsRoot()
			if err != nil {
				return err
			}

			_, err = application.SeatContext{
				Transcripts: claude.NewTranscripts(root),
				Dir:         where,
				HandoffAt:   limit,
				Out:         cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "the working directory of the session to read (default: the vault)")
	return cmd
}

// newSeatBootCmd builds `mw seat boot`: what a seat reads at boot, from its
// manifest, in one output (mw-gq6.445).
func newSeatBootCmd() *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "boot <seat>",
		Short: "Print what a seat reads at boot, from seats/<seat>/boot.toml, in one output",
		Long: "boot prints a seat's boot set: every part its manifest, seats/<seat>/boot.toml, names, in order,\n" +
			"each under a `==> <part> <==` line and within its byte cap, then an index of the headings the seat can read\n" +
			"by need with `mw seat read <seat> <topic>`. It is one tool call in place of a run of reads, and\n" +
			"nothing the manifest does not name is read.\n\n" +
			"A [[boot]] entry is one of `file` (a path from the vault's root, or from a rig's with `rig`),\n" +
			"`newest` (a directory of the vault: its newest .md by name, a seat's handoffs) or `command` (a shell\n" +
			"line run in the vault, what it prints being the part), with `section` (one heading of the file, by\n" +
			"text or slug) and `cap` (bytes; 0 none; default the manifest's own `cap`, else " + strconv.Itoa(application.DefaultBootCap) + "). A path\n" +
			"may hold {host} and {seat}. A [[topic]] entry names a file (and maybe a rig) whose headings are\n" +
			"the topics. A part past its cap is cut at a line end and says `[cut at <cap> of <size> bytes]`.\n\n" +
			"A part that cannot be read is said in its place and the rest is printed; the command then exits\n" +
			"non-zero counting them. A seat with no manifest is refused. See docs/seat-boot.md.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, rigs, host, err := seatBootPorts()
			if err != nil {
				return err
			}
			return application.SeatBootSet{
				Files:    files,
				Rigs:     rigs,
				Commands: shellcmd.New(timeout),
				Seat:     args[0],
				Host:     host,
				Out:      cmd.OutOrStdout(),
			}.Run(cmd.Context())
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", shellcmd.DefaultTimeout, "how long one of the manifest's commands may run")
	return cmd
}

// newSeatReadCmd builds `mw seat read`: one of a seat's by-need topics, whole.
func newSeatReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read <seat> [topic]",
		Short: "Print one heading's section of a seat's by-need files, or the index of them",
		Long: "read prints the section under one heading of the files a seat's manifest (seats/<seat>/boot.toml)\n" +
			"names as [[topic]] entries: the heading line through the line before the next heading of its\n" +
			"level or higher. The topic is the heading's slug — its first five words before any aside in\n" +
			"parentheses or after a colon, lower-case and hyphenated, as `mw seat boot` lists them — or a\n" +
			"prefix no other slug shares. An unknown or ambiguous topic is refused, naming what there is.\n" +
			"With no topic it prints the index alone. It reads files only.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, rigs, host, err := seatBootPorts()
			if err != nil {
				return err
			}
			topic := ""
			if len(args) > 1 {
				topic = args[1]
			}
			return application.SeatRead{
				Files: files,
				Rigs:  rigs,
				Seat:  args[0],
				Host:  host,
				Out:   cmd.OutOrStdout(),
			}.Run(cmd.Context(), topic)
		},
	}
}

// seatBootPorts reads the config the seat boot commands need: the vault, the
// rigs checked out here and this host's name.
func seatBootPorts() (*vault.Vault, rig.Docs, string, error) {
	dir, err := config.Vault()
	if err != nil {
		return nil, rig.Docs{}, "", err
	}
	host, err := config.Host()
	if err != nil {
		return nil, rig.Docs{}, "", err
	}
	rigs, err := config.Rigs()
	if err != nil {
		return nil, rig.Docs{}, "", err
	}
	return vault.New(dir), rig.Docs{Rigs: rigs}, host, nil
}
