package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/giveway"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/pause"
	"github.com/Jonathan-A-White/millwright/infrastructure/procs"

	"github.com/spf13/cobra"
)

// giveWayGated are the entry points of the command line that start heavy work on
// a host, and so say "gives way since <time>" and do nothing while the host's
// give-way marker stands. mw dispatch is not among them: it reads the marker
// itself (Dispatch.GiveWay), and lifts a stale one. The sweep of what starts work,
// and why each of the rest is left out, is in the comment of mw-2891blf.1.
var giveWayGated = []string{
	"millhand",      // the Millhand's wake and tick: a self-update build, a harness session
	"millhand tick", // the timer's tick
	"suite",         // a rig's whole suite, nightly or by hand
	"land",          // a batch landing: the rig's tests under one run
	"after-landing", // a rig's after_landing build
	"grist grind",   // the mill's pass: harness calls and the grind's tools
	"sleep pass",    // one fresh agent
	"deputy",        // the Deputy's session
}

// giveWayGatedJobs are the follower's jobs (mw events follow) that run in the
// follower's own process, not through a command, and are held while the marker
// stands. dispatch is not among them (it lifts a stale marker), nor are the jobs
// that only read or send.
var giveWayGatedJobs = map[string]bool{
	application.GristJobName: true, application.GristLevelJobName: true,
	application.NightlySuiteJobName: true, application.NightlyTimingJobName: true,
	application.BatchLandJobName: true, application.SleepJobName: true,
}

// giveWayStaleSetting is giveway_stale as the readers of the hosts' give-way notes
// judge by: the setting, and the default when it cannot be read.
func giveWayStaleSetting() time.Duration {
	stale, err := config.GiveWayStale()
	if err != nil {
		return config.DefaultGiveWayStale
	}
	return stale
}

// hostGiveWay is this host's give-way, over the host's own state directory. It
// is false when the host has no state directory to keep one in.
func hostGiveWay(host string) (application.GiveWay, bool) {
	dir, err := config.GiveWayDir()
	if err != nil {
		return application.GiveWay{}, false
	}
	store := giveway.Store{Dir: dir}
	stale, err := config.GiveWayStale()
	if err != nil {
		stale = config.DefaultGiveWayStale
	}
	// A host whose factory runs its sessions on a tmux server of its own (tmux -L)
	// is seen as well as the default server. A config that cannot be read leaves
	// the sockets the environment names, and the default server.
	sockets, _ := config.GiveWayTmuxSockets()
	way := application.GiveWay{
		Processes: procs.Freezer{}, Panes: giveway.ServersFor(sockets).WithLog(store), Marker: store, Log: store,
		Load: hostload.Proc{}, Host: host, Stale: stale, Self: os.Getpid(), By: "mw@" + host,
	}
	// The note the home reads is kept in the tracker's notes, after the freeze and
	// with a short timeout; a host with no vault keeps none (mw-2891blf.3).
	if vault, err := config.Vault(); err == nil {
		way.Notes = mwGateway(vault, host)
	}
	if self, err := os.Executable(); err == nil {
		way.Retry = pause.Retry{Mw: self}
	}
	return way, true
}

// hostGiveWayGate is this host's give-way marker as the gate every place mw starts a
// Builder session, or a part of a gate, reads right before it starts
// (application.GivesWayNow, mw-2891blf.19): mw next's sends and a batch landing's
// fixer and gate parts. nil, a gate that gives way to nobody, when the host has no
// state directory to keep a marker in.
func hostGiveWayGate(host string) application.GiveWayGate {
	if way, ok := hostGiveWay(host); ok {
		return way
	}
	return nil
}

// giveWayStanding reads this host's marker, a file of its own: it lifts nothing,
// and a marker that cannot be read gives way to nobody.
func giveWayStanding(ctx context.Context) (application.GiveWayStanding, bool) {
	dir, err := config.GiveWayDir()
	if err != nil {
		return application.GiveWayStanding{}, false
	}
	marker, there, err := giveway.Store{Dir: dir}.Read(ctx)
	if err != nil || !there {
		return application.GiveWayStanding{}, false
	}
	return application.GiveWayStanding{Since: marker.Since, By: marker.By}, true
}

func givesWayLine(what string, standing application.GiveWayStanding) string {
	return fmt.Sprintf("%s: gives way since %s (%s); nothing done\n", what, standing.Since.UTC().Format(time.RFC3339), standing.By)
}

// gateByGiveWay wraps each command of giveWayGated so that, while the marker
// stands, it prints that the host gives way and does nothing (exit 0), before
// its arguments, flags and config are looked at.
func gateByGiveWay(root *cobra.Command) {
	for _, path := range giveWayGated {
		cmd, _, err := root.Find(strings.Fields(path))
		if err != nil || cmd == root || cmd.CommandPath() != "mw "+path {
			panic("giveWayGated names a command mw does not have: " + path)
		}
		run, plain := cmd.RunE, cmd.Run
		if run == nil && plain == nil {
			continue
		}
		args := cmd.Args
		cmd.Args = func(c *cobra.Command, a []string) error {
			if _, giving := giveWayStanding(commandContext(c)); giving {
				return nil
			}
			if args == nil {
				return cobra.ArbitraryArgs(c, a)
			}
			return args(c, a)
		}
		what := "mw " + path
		cmd.Run = nil
		cmd.RunE = func(c *cobra.Command, a []string) error {
			if standing, giving := giveWayStanding(commandContext(c)); giving {
				fmt.Fprint(c.OutOrStdout(), givesWayLine(what, standing))
				return nil
			}
			if run != nil {
				return run(c, a)
			}
			plain(c, a)
			return nil
		}
	}
}

// gateJobsByGiveWay holds the follower's heavy jobs while the marker stands.
func gateJobsByGiveWay(jobs []application.SpringJob, out interface{ Write([]byte) (int, error) }) {
	for i := range jobs {
		if !giveWayGatedJobs[jobs[i].Name] {
			continue
		}
		name, run := jobs[i].Name, jobs[i].Run
		jobs[i].Run = func(ctx context.Context) error {
			if standing, giving := giveWayStanding(ctx); giving {
				fmt.Fprint(out, givesWayLine("mw events follow: the "+name+" job", standing))
				return nil
			}
			return run(ctx)
		}
	}
}

// noteFlags reads --note k=v pairs.
func noteFlags(notes []string) (map[string]string, error) {
	if len(notes) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, n := range notes {
		k, v, ok := strings.Cut(n, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("--note %q is not k=v", n)
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}

// failingHome stands for a vault whose home file cannot be read: giving way
// without --session is then refused, as on the home.
type failingHome struct{ err error }

func (f failingHome) ReadHome(context.Context) (string, error) { return "", f.err }

// newGiveWayCmd builds `mw giveway`.
func newGiveWayCmd() *cobra.Command {
	var sessions, notes []string
	var by string
	cmd := &cobra.Command{
		Use:   "giveway on|off|status|mark",
		Short: "A host gives way to the person at its screen: its Builder processes stop within a second, and it takes nothing new",
		Long: "giveway is run on a host that a person also sits at (the desktop, when Grace unlocks it).\n\n" +
			"  mw giveway on      stops (SIGSTOP) every process below every Builder session's pane (not the pane's own, which tmux resumes) and every process of\n" +
			"                     the factory's apart scopes (landings, suites, mw-heavy) on this host, except each\n" +
			"                     session's `mw next --heartbeat` (it keeps the story's lease), the tmux server and\n" +
			"                     itself, and passes over the table again until nothing new is found. It lists the panes of\n" +
			"                     every tmux server the host names (the default, $MW_TMUX_SOCKET, giveway_tmux_sockets in the config)\n" +
			"                     and also stops a Builder's process left behind in a session of its own (a worktree for cwd, or\n" +
			"                     MW_STORY in its environment). A mw dispatch or mw millhand tick already running, and what it\n" +
			"                     started, is only LOWERED (nice 19, io idle), never stopped (a stop mid-sync could wedge the sync\n" +
			"                     lock); status names each until it ends. It writes the\n" +
			"                     marker (giveway.json) first and one JSON line to giveway.log last, with took_ms, the\n" +
			"                     sessions and processes stopped, MemAvailable and load1 and every --note k=v. Before the\n" +
			"                     last stop it makes no beads call, no ssh and no network call. A second `on` only renews\n" +
			"                     the marker's seen time and stops what is new (event renew).\n" +
			"  mw giveway off     continues (SIGCONT) every process the marker names, matched by pid and start time, and any\n" +
			"                     Builder-session process still stopped; removes the marker; logs event off with took_ms and\n" +
			"                     how long the host gave way. Then, not timed, a session whose pane died is retried (mw retry)\n" +
			"                     and said so in the log. With no marker it still continues what is stopped and says so.\n" +
			"  mw giveway status  whether this host gives way, since when, the sessions and processes stopped, and the last\n" +
			"                     5 lines of the log. Exit 0 either way.\n" +
			"  mw giveway mark    only appends a line of event mark with the --note k=v given: the Windows side puts its own\n" +
			"                     measurements in the same log.\n\n" +
			"After its last stop `on` writes a note for the home, host.<host>.gives_way = '<since> <by> <seen>', to the\n" +
			"tracker's notes (a short timeout; a failure is logged in giveway.log as note and changes nothing else). A\n" +
			"renew rewrites it no oftener than every 10 minutes; `off` and the expired lift clear it. While it stands, the\n" +
			"home's quiet, stuck, stranded and starved alarms pass the host's frozen stories over, and mw status says so.\n\n" +
			"While the marker stands, mw dispatch on this host does nothing at all and says \"gives way since <time>\", and so\n" +
			"does every other entry that starts heavy work (millhand, suite, land, after-landing, grist grind, sleep pass,\n" +
			"deputy, and the follower's suite, timing, grist, batch-land and sleep jobs). A marker not renewed for giveway_stale\n" +
			"(config, default 15m) is lifted by the next dispatch tick, which logs event expired.\n\n" +
			"--session <name> (repeatable) on `on` and `off` limits them to those tmux sessions and leaves the marker and the\n" +
			"gate alone: how to try it on a busy host. On the HOME host (the one that serves the Mayor and the beads) `on`\n" +
			"without --session is REFUSED with one line: the home never gives way.\n\n" +
			"Every verb first gives mw the environment of a login shell it may not have been started with (the Windows tasks run\n" +
			"wsl.exe --exec): ~/.local/bin and the standard directories on PATH, after what PATH names, and the names in\n" +
			"~/.config/mw/beads.env that the environment does not set.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The Windows tasks run this with no login environment (wsl.exe --exec):
			// bd, tmux and beads.env first, before anything is read or stopped.
			loginEnvironment()
			verb := args[0]
			if verb != "on" && verb != "off" && verb != "status" && verb != "mark" {
				return fmt.Errorf("giveway %q is not a verb: on, off, status or mark", verb)
			}
			if verb != "on" && verb != "off" && len(sessions) > 0 {
				return fmt.Errorf("--session is for on and off")
			}
			noted, err := noteFlags(notes)
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			way, ok := hostGiveWay(host)
			if !ok {
				return fmt.Errorf("giveway needs a state directory: no home directory, and MW_GIVEWAY_DIR is not set")
			}
			way.Out = cmd.OutOrStdout()
			if by != "" {
				way.By = by
			}
			req := application.GiveWayRequest{Sessions: sessions, Notes: noted}
			switch verb {
			case "on":
				if len(sessions) == 0 {
					dir, err := config.Vault()
					if err != nil {
						way.Home = failingHome{err}
					} else {
						way.Home = mwVault(dir, host)
					}
				}
				return way.On(cmd.Context(), req)
			case "off":
				return way.Off(cmd.Context(), req)
			case "mark":
				return way.Mark(cmd.Context(), noted)
			}
			return way.Status(cmd.Context())
		},
	}
	cmd.Flags().StringArrayVar(&sessions, "session", nil, "limit on and off to this tmux session (repeatable); leaves the marker and the gate alone")
	cmd.Flags().StringArrayVar(&notes, "note", nil, "k=v put in the log line (repeatable)")
	cmd.Flags().StringVar(&by, "by", "", "who asks, for the marker and the log (default mw@<host>)")
	return cmd
}

// commandContext is the command's context, or a plain one for a command run
// without one.
func commandContext(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
