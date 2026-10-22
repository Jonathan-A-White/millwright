package main

import (
	"context"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/procs"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

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
			"`host_silent_hours`, default 2) and has shown no other sign of life in that time (a vault\n" +
			"commit, a claim, a renewed lease on a story it holds; the newest is shown), or that never\n" +
			"synced at all, is marked asleep and its work\n" +
			"is listed as stranded, with the one line that re-paths a story here. Re-pathing is a\n" +
			"person's act: status only says which stories are waiting for one.\n\n" +
			"A DAMPERS section line 'stuck: N live beads behind M held roots: <ids>' names the held beads that\n" +
			"live beads wait behind and that cannot move on their own (mw stuck lists them with what is behind).\n\n" +
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
			"session pays for that file at boot, so the Mayor is due to prune it (or, for a rig kept as facts, retire or supersede some). It is left out when\n" +
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
			"A HELD, WITH A HANDS STEP section lists each held bead that keeps a hands step not yet run: the\n" +
			"Governor is offered no Run on it until it is open. It is left out when there are none.\n\n" +
			"A DONE, STILL OPEN section lists each open bead that looks finished, so the Mayor can close it:\n" +
			"an epic or map whose children are all closed, and a grilling or research ticket linked to\n" +
			"epics that are all closed. It costs one read of every bead, and closes nothing.\n\n" +
			"An EVENTS line says where the event follower stands: the log's head seq, the last seq sent,\n" +
			"the batches that went direct as fallback and wait to go on chain, and today's records on\n" +
			"chain of the daily cap ([events] chain_daily_cap).\n\n" +
			"A NETWORK line says whether the network is metered: 'NETWORK metered (Windows: <profile>, cost\n" +
			"<type>)' or 'NETWORK unmetered', as Windows' own setting says it on WSL (config `metered`\n" +
			"overrides). While metered, no beads backup runs and no story on a heavy_net rig starts; a DAMPERS\n" +
			"line 'metered: pinned to <host>, metered, its rig installs dependencies: set host=auto' names each\n" +
			"ready story pinned to this host on such a rig, which only host=auto lets another host take.\n\n" +
			"A ready story every host has passed for a reason of the host's own (its rig not checked out there,\n" +
			"or metered on a heavy_net rig) says 'passed by every host, so none takes it' under its READY line,\n" +
			"naming each host's reason, as the hosts' dispatch notes it. Passed for over 20 minutes, a DAMPERS\n" +
			"line 'no host can take it: ...' names it with each host's fix.\n\n" +
			"A VPS NGINX line says whether the VPS's nginx postern_api upstream sends the phone to the home\n" +
			"first and every other backend as `backup`: 'VPS NGINX ok (home first, N backup)', 'VPS NGINX\n" +
			"FAULT: <what>', or 'VPS NGINX not checked (<why>)' when the VPS cannot be reached over ssh. It\n" +
			"adds 'bin/mw lacks <commit>' when the VPS's mw binary was built without the commit the [doctor]\n" +
			"table's vps_mw_needs names. The check is mw doctor's vps-nginx, which tells the Governor once.\n\n" +
			"A standby line, when a [backend.<rig>] table names a VPS standby (vps_host), compares the commit\n" +
			"the standby's /healthz says with the home's: 'standby behind: <standby commit|none> vs <home\n" +
			"commit>', 'standby level at <commit>', or 'standby not checked (<why>)'. A landing that changed\n" +
			"the backend stages the standby's swap as a hands step for the Governor's tap.\n\n" +
			"A BENCHMARKS section reads what every close-out recorded in its story's result file in the vault\n" +
			"(gate_seconds, load_at_gate, cores, mem_free_mb, running_count, swap_in_per_s, par_s and\n" +
			"more): each rig's usual gate time on each host against its latest ('lampas on laptop: gate\n" +
			"usual 5m10s, latest 9m02s', the median of the last [benchmark] usual_gates), what each host\n" +
			"landed per hour at each count of stories running at once, raw and in par-hours (a story's par\n" +
			"is the median wall time of the last par_window landings of its rig, bug-or-not and model, or\n" +
			"of its rig or all when fewer than par_min are of the kind), and each kind's par error over its\n" +
			"last calibration_window landings, flagged past error_flag_percent with the change it suggests.\n\n" +
			"A 'host: N of cap M' line says how many of the cap on sessions (config `cap`) are in use. When the\n" +
			"host has no room to start another story - its 1-minute load at or above its core count, or\n" +
			"less than 2 GB of memory available (the [dispatch] table's room_load_per_core and\n" +
			"room_min_free_mb) - a 'held back, no room' line gives each reason, and mw dispatch starts\n" +
			"nothing here until it clears.\n\n" +
			"On the host where the mill runs, a 'grist:' line says whether a tutor is in use ('active (3 in\n" +
			"10 min)', 'slow' or 'quiet'), the median of its last answers against their par, and the lowered cap\n" +
			"('grist first: N stories at most while it lasts'); slow grist is a 'held back, no room' reason.\n" +
			"While the follower's warm pool runs, a 'grist pool:' line gives each app and kind's sessions\n" +
			"waiting for a grist and answering one ('lampas/bible-talk 1 warm, 0 busy').\n\n" +
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
			atOnce, capWhy, err := config.CapInForce(time.Now())
			if err != nil {
				return err
			}
			room, err := config.Room()
			if err != nil {
				return err
			}
			bench, err := config.Benchmark()
			if err != nil {
				return err
			}
			files := mwVault(dir, host)
			setting, err := hostBeads(cmd.Context(), files, host)
			if err != nil {
				return err
			}

			tracker := statusTracker(dir, host)
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
			var vps application.VPSNginxReader
			if guard, err := hostVPSNginx(files, true); err != nil {
				return err
			} else if guard != nil {
				vps = guard
			}
			var standby application.StandbyReader
			if reader := hostStandby(files); reader != nil {
				standby = reader
			}
			// Apps that cannot be listed cost the report only its VISION lines.
			gristApps, _ := config.GristApps()
			mayor := application.MayorReader{Tracker: tracker, Notes: tracker}
			cloudBook, plan := cloudStatus(dir, host)
			// A rig table that cannot be read costs the report only what running
			// branches have changed: the files keys are still compared.
			rigs, err := config.Rigs()
			if err != nil {
				rigs = nil
			}
			// Main's commit is read as this host last fetched it; without a
			// checkout of the factory rig no host is said to be behind it.
			var mainCommit string
			if dir := rigs[application.FactoryRig]; dir != "" {
				if tip, err := rig.New().Tip(cmd.Context(), dir, "origin", application.FactoryBranch); err == nil && len(tip) >= 7 {
					mainCommit = tip[:7]
				}
			}
			// Without the harness's projects directory no session's turns are
			// read, and the report names none past the line (mw-1ljpo9.6).
			var turns application.TranscriptTurns
			if projects, err := claude.DefaultProjectsRoot(); err == nil {
				turns = claude.NewTranscripts(projects)
			}
			dampers, err := damperSurvey(tracker)
			if err != nil {
				return err
			}
			heavy, err := config.HeavyNet()
			if err != nil {
				return err
			}
			_, err = application.Status{
				HeavyNet:         heavy,
				Tracker:          tracker,
				Dampers:          dampers,
				TestersPerRig:    dampers.Limits.TestersPerRig,
				Turns:            turns,
				Ideas:            bootIdeasOnly(),
				Rigs:             rigs,
				Changes:          rig.New(),
				RigFiles:         rig.Files{},
				Notes:            tracker,
				MainCommit:       mainCommit,
				Signs:            mwVault(dir, host),
				Vault:            mwVault(dir, host),
				RigFacts:         mwVault(dir, host),
				Errors:           files,
				Rules:            files,
				Suites:           files,
				Batches:          files,
				Graph:            tracker,
				SyncHalt:         hostSyncHalt(),
				CloseOuts:        hostCloseOuts(),
				Mayor:            mayor,
				HeldHands:        mayor,
				Icebox:           application.IceboxReader{Beads: tracker, Notes: tracker},
				Host:             host,
				Seat:             BuilderSeat,
				Ticks:            hostTickLogs(),
				Panes:            seatWindows(),
				Events:           events,
				Log:              idleLog,
				IdleAfter:        idleAfter,
				Harness:          procs.Harness{},
				Control:          homeEventLog(),
				Smoke:            gristSmokeBook(tracker, host),
				Apps:             gristApps,
				Visions:          grist.NewVisions(),
				HostSilence:      time.Duration(hours) * time.Hour,
				RigMemoryBytes:   budget,
				BeadsBudgetBytes: beadsBudget,
				SyncMode:         setting.Configured,
				Home:             files,
				Network:          net,
				Cap:              atOnce,
				CapWhy:           capWhy,
				Load:             hostload.Proc{},
				Room:             application.RoomLimits{LoadPerCore: room.LoadPerCore, MinFreeMB: room.MinFreeMB},
				Grist:            hostGristRoom(room),
				VPSNginx:         vps,
				Standby:          standby,
				Cloud:            cloudBook,
				CloudPlan:        plan,
				Benchmarks:       vault.Benchmarks{Vault: files},
				Bench:            benchmarkLimits(bench),
				Causes:           tracker,
				Out:              cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
}

// statusTracker is the tracker every section of mw status reads from. It keeps
// what the run reads of bd (the list of every bead, the table of notes), so that
// the snapshot, the bead graph, the stuck check and the causes share one list
// and the notes are read once (mw-gq6.659); status writes nothing.
func statusTracker(dir, host string) *beads.Gateway {
	return mwGateway(dir, host, beads.WithRunCache())
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

// benchmarkLimits is the [benchmark] table as the use cases read it.
// damperSurvey is where mw status and mw doctor judge the DAMPERS alarms from:
// the [dampers] table's limits over the tracker.
func damperSurvey(tracker *beads.Gateway) (application.DamperSurvey, error) {
	limits, err := config.Dampers()
	if err != nil {
		return application.DamperSurvey{}, err
	}
	return application.DamperSurvey{
		Tracker: tracker, Notes: tracker, Graph: tracker,
		Stuck: application.StuckSurvey{Graph: tracker, Comments: tracker, Claims: tracker},
		Limits: application.DamperLimits{
			ReadyP1: limits.ReadyP1, P1UntouchedDays: limits.P1UntouchedDays, DemoWaitingDays: limits.DemoWaitingDays,
			GrillingDays: limits.GrillingDays, TestersPerRig: limits.TestersPerRig, ReviewQueue: limits.ReviewQueue,
		},
	}, nil
}

// testersPerRig is the [dampers] testers_per_rig key, 0 when the table leaves it
// out: mw dispatch holds a ready Tester while its rig runs that many (mw-gq6.546).
func testersPerRig() (int, error) {
	limits, err := config.Dampers()
	if err != nil {
		return 0, err
	}
	return limits.TestersPerRig, nil
}

func benchmarkLimits(s config.BenchmarkSettings) application.BenchmarkSettings {
	return application.BenchmarkSettings{
		UsualGates: s.UsualGates, ParWindow: s.ParWindow, ParMin: s.ParMin,
		CalibrationWindow: s.CalibrationWindow, ErrorFlagPercent: s.ErrorFlagPercent, OverParFactor: s.OverParFactor,
	}
}

// gateScaledSlotWait is how a close-out's wait on one holder of a rig's merge
// slot scales with the rig's own gate on this host (mw-gq6.350): twice the
// slower of its usual and latest gate, never under rig.SlotWait. rigs is where
// each rig is checked out, to name the rig a slot belongs to. A history that
// cannot be read, or a slot of a directory no rig is checked out in, waits
// rig.SlotWait.
func gateScaledSlotWait(book application.BenchmarkBook, rigs map[string]string, host string, s application.BenchmarkSettings) rig.SlotOption {
	return rig.WithSlotWaitFor(gateScaledWait(book, rigs, host, s))
}

func gateScaledWait(book application.BenchmarkBook, rigs map[string]string, host string, s application.BenchmarkSettings) func(ctx context.Context, rigDir string) time.Duration {
	return func(ctx context.Context, rigDir string) time.Duration {
		for name, checkout := range rigs {
			if filepath.Clean(checkout) != filepath.Clean(rigDir) {
				continue
			}
			history, err := book.Recent(ctx)
			if err != nil {
				return rig.SlotWait
			}
			return application.SlotWaitFor(history, name, host, s, rig.SlotWait)
		}
		return rig.SlotWait
	}
}
