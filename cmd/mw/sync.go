package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newSyncCmd builds `mw sync`: the one command that brings this host level with
// the other one. It is safe to run by hand, from the dispatcher, or on a timer,
// and it says in one line what it did.
func newSyncCmd() *cobra.Command {
	var noBackup bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Bring this host level with the other one: the vault, then beads",
		Long: "sync pulls the other host's vault commits and pushes this host's, then runs one beads\n" +
			"synchronisation cycle, then records when this host was last level. It never migrates and\n" +
			"never forces: what it cannot settle stops it, with the reason in plain words. A merge\n" +
			"conflict (bd exit 2) is given one retry after a short wait, since it sometimes clears on\n" +
			"its own within seconds; nothing else is retried. A sync stopped by beads exits with beads'\n" +
			"own exit code, so that a timer can branch on it: 2 is a merge conflict and 4 a stuck\n" +
			"working set, and both wait for a person.\n" +
			"Work nobody committed in the vault is not a failure: the vault half is skipped, beads are\n" +
			"synced anyway, and sync exits 5 with one line naming the files. mw commits nobody's edits.\n\n" +
			"With the note of when this host was level it leaves the counts of its timers' logs, for the\n" +
			"other host's mw status to show: the last good run of mw dispatch and of the Millhand's tick, and\n" +
			"how many runs since have failed.\n\n" +
			"Once level, it also asks the tracker to reclaim the disk space its own auto-commit history\n" +
			"piles up — never more than once a day per host, since a full collection can take real time.\n" +
			"Nothing is ever deleted by it: that stays a person's call.\n\n" +
			"All of that is config `beads_sync = \"remote\"`, the default. On the host that keeps the one\n" +
			"database (`backup`) the beads cycle is only a backup, run once the last one is\n" +
			"`beads_backup_minutes` old (default 30); one that halts is said and marked but stops nothing.\n" +
			"On a host whose bd reaches another host's database (`shared`) there is no beads cycle and no\n" +
			"collection. In both the note of when this host was level is written on every sync, straight\n" +
			"into the one database. `auto` is backup on the home and shared on a boost, read off the\n" +
			"vault's home file (backup every 5 minutes unless beads_backup_minutes says; the boost's\n" +
			"server is beads_server_host or <home>.mw); with no home file it refuses and does nothing.\n\n" +
			"While the network is metered (config `metered`, or Windows' own setting read from WSL), a\n" +
			"backup that is due is skipped, as --no-backup would, with 'backup skipped: metered network'.\n\n" +
			"--no-backup leaves that backup, and the collection after it, to the next sync that does not\n" +
			"say so, even when one is due: for a caller that must stay cheap, like the mail notifier,\n" +
			"whose run is killed at its unit's timeout. Nothing is recorded, so the backup stays due.",
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

			sync, err := hostSync(application.Sync{
				Vault:     mwVault(dir, host),
				Tracker:   mwGateway(dir, host),
				Host:      host,
				Ticks:     hostTickLogs(),
				Lock:      hostSyncLock(),
				SyncHalts: hostSyncHalt(),

				SkipBackup: noBackup,
			})
			if err != nil {
				return err
			}
			report, err := sync.Run(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), report)
			return nil
		},
	}
	cmd.Flags().BoolVar(&noBackup, "no-backup", false, "leave the backup of the beads database to another sync, even if one is due")
	return cmd
}

// hostBeadsSync is how this host's beads database is treated: config
// beads_sync, remote when it says nothing. It is as configured: auto stays
// auto, for hostBeads to read off the home.
func hostBeadsSync() (application.BeadsSyncMode, error) {
	said, err := config.BeadsSync()
	if err != nil {
		return "", err
	}
	return application.ParseBeadsSyncMode(said)
}

// hostBeadsSetting is what this host's beads_sync comes to.
type hostBeadsSetting struct {
	// Configured is the mode as config says it, auto included.
	Configured application.BeadsSyncMode
	// Resolved is the mode this host acts on. For auto whose home cannot be
	// told it is still auto, and Unknown says why: nothing may be done on it.
	Resolved application.ResolvedBeadsSync
	Unknown  error
}

// Mode is the mode this host acts on: Configured, or the one auto came to.
func (h hostBeadsSetting) Mode() application.BeadsSyncMode {
	if h.Resolved.Mode == "" {
		return h.Configured
	}
	return h.Resolved.Mode
}

// hostBeads reads this host's beads_sync and, when it is auto, the vault's
// home file with it. On a boost it also points bd at the home's Dolt server —
// beads_server_host or <home>.mw — in this process's environment, where every
// bd this command starts reads it; the rest of the server's settings stay in
// the environment as they were. Nothing is guessed: a home that cannot be told
// leaves the environment alone and comes back as Unknown.
func hostBeads(ctx context.Context, files application.HomeFile, host string) (hostBeadsSetting, error) {
	configured, err := hostBeadsSync()
	if err != nil {
		return hostBeadsSetting{}, err
	}
	setting := hostBeadsSetting{Configured: configured}
	resolved, err := application.ResolveBeadsSync(ctx, files, host, configured)
	if _, unknown := application.HomeUnknownIn(err); unknown {
		setting.Unknown = err
		return setting, nil
	}
	if err != nil {
		return hostBeadsSetting{}, err
	}
	setting.Resolved = resolved
	if configured == application.BeadsSyncAuto && resolved.Mode == application.BeadsSyncShared {
		override, err := config.BeadsServerHost()
		if err != nil {
			return hostBeadsSetting{}, err
		}
		if err := os.Setenv(config.BeadsDoltServerHostEnv, application.BoostServerHost(resolved.Home, override)); err != nil {
			return hostBeadsSetting{}, err
		}
	}
	return setting, nil
}

// sessionHarness is the Claude Code harness for the sessions this host starts:
// reading beads.env first, then pointing bd at the server host the home says
// (application.SessionServerHost), so that a move of the home is followed by
// every session without anyone rewriting the file. That host is also set in
// this process's environment, where the bd that mw runs itself reads it. A host
// that cannot be told — no beads_sync auto, no home file, a config error that
// the commands around this one report on their own — leaves both as they were.
func sessionHarness(vaultDir, host string, opts ...claude.Option) *claude.Harness {
	opts = append(opts, claude.WithEnvFile(beadsEnvFile()))
	configured, err := hostBeadsSync()
	if err != nil {
		return claude.New(opts...)
	}
	override, err := config.BeadsServerHost()
	if err != nil {
		return claude.New(opts...)
	}
	info, statErr := os.Stat(filepath.Join(vaultDir, ".beads", "dolt"))
	server := application.SessionServerHost(context.Background(), mwVault(vaultDir, host), host, configured, override, statErr == nil && info.IsDir())
	if server != "" {
		_ = os.Setenv(config.BeadsDoltServerHostEnv, server)
		opts = append(opts, claude.WithBeadsServerHost(server))
	}
	return claude.New(opts...)
}

// hostSync is sync with this host's beads_sync mode and backup interval set
// from config, so that every command that brings this host level — mw sync,
// dispatch, next and the Millhand's tick — treats the beads database alike.
// A beads_backup_minutes that is not said is the mode's own default: 30, and
// 5 for auto, which the sync applies once it knows it is on the home. It also
// gives the sync the host's network, so that a backup waits out a metered one.
func hostSync(sync application.Sync) (application.Sync, error) {
	files, _ := sync.Vault.(application.HomeFile)
	setting, err := hostBeads(context.Background(), files, sync.Host)
	if err != nil {
		return sync, err
	}
	minutes, said, err := config.BeadsBackupMinutesSaid()
	if err != nil {
		return sync, err
	}
	sync.Mode = setting.Configured
	sync.Home = files
	if sync.Network == nil {
		if sync.Network, err = hostNetwork(false); err != nil {
			return sync, err
		}
	}
	if sync.Stamps == nil {
		// A stamp queue that cannot be placed queues nothing: no sync waits on a
		// chain stamp.
		if queue, err := stampQueue(); err == nil {
			sync.Stamps = queue
		}
	}
	switch {
	case said:
		sync.BackupInterval = time.Duration(minutes) * time.Minute
	case setting.Configured != application.BeadsSyncAuto:
		sync.BackupInterval = time.Duration(config.DefaultBeadsBackupMinutes) * time.Minute
	}
	return sync, nil
}

// exitCode is the status mw leaves with after a command reported err. A sync
// that beads stopped leaves with beads' own code, so that whoever ran mw reads
// the same number bd would have given them; a sync only somebody's uncommitted
// vault work stood in the way of leaves with a status of its own; anything else
// is a plain 1.
func exitCode(err error) int { return application.ExitStatus(err) }
