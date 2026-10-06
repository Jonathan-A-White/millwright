package main

import (
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
	"github.com/Jonathan-A-White/millwright/infrastructure/procs"

	"github.com/spf13/cobra"
)

// newStatusCmd builds `mw status`: what this host is doing right now, for a
// phone. It is read-only and zero-token — it starts no session, and it writes
// nothing to beads, the ledger, git or tmux.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what is running, ready and blocked on this host",
		Long: "status reads this host's running stories (with the tmux session name to attach to), what\n" +
			"is ready to be taken, what is blocked on an unfinished dependency, and today's fuel summed\n" +
			"from the Builder's ledger. A story filed with only its path overrides is shown with the\n" +
			"epic's defaults filled in. A claimed story whose close-out is blocked by an open formula\n" +
			"step says so, and a story recorded run=stopped or run=stuck is shown as that, not running.\n\n" +
			"It then shows every other host a story is pathed to: when that host last recorded itself\n" +
			"level, and what it holds. A host silent for longer than the threshold (config\n" +
			"`host_silent_hours`, default 2), or that never synced at all, is marked asleep and its work\n" +
			"is listed as stranded, with the one line that re-paths a story here. Re-pathing is a\n" +
			"person's act: status only says which stories are waiting for one.\n\n" +
			"A WAITING ON THE MAYOR section lists each hands bead (hitl) with no step filed yet, which\n" +
			"the postern view says waits on the Mayor, with its bead, its age and what it waits on. It\n" +
			"costs one read of the notes, and builds and keeps nothing of the view.\n\n" +
			"A TICKS section says how this host's timers are doing, counted from the logs mw dispatch and\n" +
			"mw millhand tick keep: the time of the last good run of each and how many runs since have\n" +
			"failed, with local network faults counted apart. It is left out on a host that keeps no log.\n" +
			"The same counts reach the other host with every sync, and are shown under its block in OTHER\n" +
			"HOSTS.\n\n" +
			"When a rig's memory in the Builder's seat is larger than the budget (config\n" +
			"`rig_memory_bytes`, default 8000), a RIG MEMORY section says which and by how much: every\n" +
			"session pays for that file at boot, so the Mayor is due to prune it. It is left out when\n" +
			"none is over.\n\n" +
			"The BEADS line warns when this host's beads database is past its budget (config\n" +
			"`beads_budget_bytes`, default 1500000000).\n\n" +
			"An IDLE line, 'IDLE since HH:MM', says the home's event log has held no event but the jobs' own\n" +
			"(dispatch, tick and sync passes) since then, for longer than [events] idle_after (default 10m), and\n" +
			"that no job is in flight, with the count of harness processes alive: none when the factory is idle.\n" +
			"Otherwise a HARNESS line gives the count. Both are left out on a host with no event log.\n\n" +
			"A BEADS line says how large this host's own beads database is on disk — its auto-commit\n" +
			"history and auto-backups included, since both have grown unbounded before — and warns once\n" +
			"it passes 1 GB, so the Mayor sees it without asking a Clerk to run du. A BEADS SYNC line\n" +
			"under it says how this host's beads are synced (config `beads_sync`: remote, backup, shared or\n" +
			"auto, which says whether this host is the home or a boost, and why) and, on the host that\n" +
			"keeps the one database, when its last backup got through.\n\n" +
			"When a rig's file in the vault (rigs/<rig>.toml) requires something of its epics, an EPICS\n" +
			"MISSING REQUIREMENTS section names each open epic that lacks it, and an EPICS WAIVED section\n" +
			"each epic the Governor waived it for. Both are left out when there are none.\n\n" +
			"A DONE, STILL OPEN section lists each open bead that looks finished, so the Mayor can close it:\n" +
			"an epic or map whose children are all closed, and a grilling or research ticket linked to\n" +
			"epics that are all closed. It costs one read of every bead, and closes nothing.\n\n" +
			"An EVENTS line says where the event follower stands: the log's head seq, the last seq sent,\n" +
			"the batches that went direct as fallback and wait to go on chain, and today's records on\n" +
			"chain of the daily cap ([events] chain_daily_cap).\n\n" +
			"A NETWORK line says whether the network is metered: 'NETWORK metered (Windows: <profile>, cost\n" +
			"<type>)' or 'NETWORK unmetered', as Windows' own setting says it on WSL (config `metered`\n" +
			"overrides). While metered, no beads backup runs and no story on a heavy_net rig starts.\n\n" +
			"Every line fits a phone-width terminal, at most 60 columns. Nothing is claimed, nothing is\n" +
			"written and no session is started: status only reads.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			hours, err := config.HostSilentHours()
			if err != nil {
				return err
			}

			budget, err := config.RigMemoryBytes()
			if err != nil {
				return err
			}
			beadsBudget, err := config.BeadsBudgetBytes()
			if err != nil {
				return err
			}
			files := mwVault(dir, host)
			setting, err := hostBeads(cmd.Context(), files, host)
			if err != nil {
				return err
			}

			tracker := mwGateway(dir, host)
			events, err := eventsShipping(host)
			if err != nil {
				return err
			}
			idleLog, idleAfter, err := idleSource()
			if err != nil {
				return err
			}
			net, err := hostNetwork(true)
			if err != nil {
				return err
			}
			_, err = application.Status{
				Tracker:          tracker,
				Notes:            tracker,
				Vault:            mwVault(dir, host),
				Rules:            files,
				Graph:            tracker,
				SyncHalt:         hostSyncHalt(),
				Mayor:            application.MayorReader{Tracker: tracker, Notes: tracker},
				Host:             host,
				Seat:             BuilderSeat,
				Ticks:            hostTickLogs(),
				Events:           events,
				Log:              idleLog,
				IdleAfter:        idleAfter,
				Harness:          procs.Harness{},
				Control:          homeEventLog(),
				HostSilence:      time.Duration(hours) * time.Hour,
				RigMemoryBytes:   budget,
				BeadsBudgetBytes: beadsBudget,
				SyncMode:         setting.Configured,
				Home:             files,
				Network:          net,
				Out:              cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
}

// eventsShipping is where mw status reads the event follower's sending from,
// or nil, with no error, when the host has no event log path to read.
func eventsShipping(host string) (application.EventsShipping, error) {
	path, err := config.EventsLogPath()
	if err != nil {
		return nil, nil
	}
	ship, err := eventShip(path, host)
	if err != nil {
		return nil, err
	}
	return ship, nil
}

// idleSource is the event log mw status reads the IDLE line from, with how
// long it may hold no event but the jobs' before it is idle: nil, with no
// error, when the host has no event log path to read.
func idleSource() (application.EventLog, time.Duration, error) {
	path, err := config.EventsLogPath()
	if err != nil {
		return nil, 0, nil
	}
	knobs, err := config.Events()
	if err != nil {
		return nil, 0, err
	}
	return eventlog.New(path), knobs.IdleAfter, nil
}
