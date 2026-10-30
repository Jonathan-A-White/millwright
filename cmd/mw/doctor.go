package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/configfile"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
	"github.com/Jonathan-A-White/millwright/infrastructure/flushlog"
	vaultpkg "github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newDoctorCmd builds `mw doctor`: a table of checks, each its own probe,
// cure, damper and way back, run on this host and logged here. It calls
// nothing but a check's own probe or cure, and never AI or mail itself: a
// check left needing a person is a beads note, one key per check, and
// `mw millhand tick` is what wakes the Millhand for it. The only pushes are
// the alarms to the Governor, mayor-stale's, mayor-stuck's and boost-reach's, sent from their cures,
// and starved-rig's one mail to the Mayor an episode. Its subcommand unit-failed
// is not a check: it is what a failed unit's OnFailure= runs.
func newDoctorCmd() *cobra.Command {
	var dryRun bool
	var envFile, posternBin string
	cmd := &cobra.Command{
		Use:   "doctor [check | licences | capacity <rig>]",
		Short: "Cure this host's known faults offline, on a table of checks",
		Long: "doctor works a table of checks, each with a probe (never changes anything), a cure (run\n" +
			"only when the probe says faulty and the damper allows it), a damper (a minimum wait\n" +
			"between cures and a cap on how many one fault episode may spend before it gives up and\n" +
			"waits for a person), and a way back. With no check named it works the whole table; named,\n" +
			"only that one. Every run appends one dated line per check to the doctor's log.\n\n" +
			"--dry-run prints, for each faulty check, the reason and the way back, and changes\n" +
			"nothing: no cure runs, no state is written, no log line is appended.\n\n" +
			"It leaves with 0 when every check is ok or cured, and " + fmt.Sprint(application.DoctorFaultExit) +
			" when any check is left faulty and uncured (damped, or its cure failed), so a timer's\n" +
			"journal shows it. A check that says so (tmp-leftovers) is not left faulty by a damped tick\n" +
			"held only by the wait after a cure that worked, and its cure that freed something ends the\n" +
			"fault's episode, so a cache regrown by normal work is cured again rather than damped.\n\n" +
			"mw doctor licences is not a check but a read of the licence map the postern binary prints\n" +
			"(--postern, default postern on the PATH) for `rights --json` on the backend's environment file\n" +
			"(--env-file, default ~/.config/postern/env): one line per licence collection, naming the\n" +
			"app it maps to or the cockpit and the routes a key holding it opens, and a warning for a\n" +
			"cockpit collection mapped to an app (it makes the cockpit key an app key) or one collection\n" +
			"mapped to two apps. The rules are postern's, none is kept here: with postern missing or too\n" +
			"old to know `rights`, it says so in one line and fails. It spends nothing, changes nothing,\n" +
			"and leaves with " + fmt.Sprint(application.DoctorFaultExit) + " on a warning.\n\n" +
			"mw doctor capacity <rig> is not a check either but a read of the capacity block in the rig's\n" +
			"file in the vault (capacity_viewers, capacity_sse_cap, capacity_nginx_worker_connections,\n" +
			"capacity_memory_free_mb): one line per fact, its value or MISSING, so that a deployed rig's\n" +
			"limits are written down before an event. It changes nothing and leaves with " + fmt.Sprint(application.DoctorFaultExit) + " when a fact is missing.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			if name == capacityArg {
				return runDoctorCapacity(cmd, args[1:])
			}
			if len(args) > 1 {
				return fmt.Errorf("mw doctor takes one check name, got %d arguments", len(args))
			}
			if name == licencesArg {
				return runDoctorLicences(cmd, posternBin, envFile)
			}

			units, err := config.DoctorUnits()
			if err != nil {
				return err
			}
			dir, err := config.DoctorStateDir()
			if err != nil {
				return err
			}
			reach, err := config.DoctorReach()
			if err != nil {
				return err
			}
			powershell, err := config.DoctorPowershell()
			if err != nil {
				return err
			}
			tunnelUnit, err := config.DoctorTunnelUnit()
			if err != nil {
				return err
			}
			tunnelHost, err := config.DoctorTunnelHost()
			if err != nil {
				return err
			}
			tunnelProbe, err := config.DoctorTunnelProbe()
			if err != nil {
				return err
			}
			wgHub, err := config.DoctorWgHub()
			if err != nil {
				return err
			}
			wgUnit, err := config.DoctorWgUnit()
			if err != nil {
				return err
			}
			tmpLeftoversBudget, err := config.DoctorTmpLeftoversBudgetBytes()
			if err != nil {
				return err
			}
			goBuildBudget, err := config.DoctorGoBuildBudgetBytes()
			if err != nil {
				return err
			}
			rootDiskBudget, err := config.DoctorRootDiskBudgetBytes()
			if err != nil {
				return err
			}
			rootDiskMargin, err := config.DoctorRootDiskMarginBytes()
			if err != nil {
				return err
			}
			beadsBudget, err := config.BeadsBudgetBytes()
			if err != nil {
				return err
			}
			beadsSquash, err := config.SyncBeadsSquash()
			if err != nil {
				return err
			}
			vault, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			// Every run clears or reads a note for each check, and a bd started for
			// each was most of a host's new connections to the beads server on a
			// five-minute clock (mw-gq6.608): they are read over one connection.
			defer keepBeadsConnection(vault, cmd.ErrOrStderr())()

			// Read here but judged by the check itself: a beads_sync or a
			// BEADS_DOLT_SERVER_PORT this host cannot read is beads-server's
			// to say, never a reason every other check does not run.
			setting, beadsSyncErr := hostBeads(cmd.Context(), mwVault(vault, host), host)
			if beadsSyncErr == nil && setting.Unknown != nil {
				beadsSyncErr = fmt.Errorf("beads_sync is auto but %w: nothing to judge", setting.Unknown)
			}
			beadsServer, beadsServerErr := config.BeadsServerAddress()
			beadsServerCheck := doctor.NewBeadsServer(string(setting.Mode()), beadsServer, beadsSyncErr, beadsServerErr)
			beadsServerCheck.Why = setting.Resolved.Why

			mayorGone := doctor.NewMayorGone(vault)
			mayorGone.Home, mayorGone.Host = mwVault(vault, host), host

			transcribeCmd, transcribeErr := config.PosternTranscribeCmd()
			posternTranscribe := doctor.NewPosternTranscribe(transcribeCmd, transcribeErr)
			posternTranscribe.Home, posternTranscribe.Host = mwVault(vault, host), host

			dispatchWindows := doctor.NewDispatchWindows(config.DispatchWindowProblems())

			// Read here, judged by the check: a config this host cannot read is
			// rig-tests's to say.
			rigsAt, rigsErr := config.Rigs()
			rigTestsRead, testsErr := config.Tests()
			rigTests := doctor.NewRigTests(rigsAt, rigTestsRead, errors.Join(rigsErr, testsErr))
			rigTests.Home, rigTests.Host = mwVault(vault, host), host

			posternChannel := doctor.NewPosternChannel(mwVault(vault, host), host)

			// The home's config against the vault's hosts/boost-config.toml: a
			// config this host cannot find is boost-config's to say.
			configPath, configPathErr := config.FilePath()
			boostConfig := doctor.NewBoostConfig(mwVault(vault, host), host,
				configfile.New(configPath), configfile.New(filepath.Join(vault, application.BoostConfigFile)))
			boostConfig.ConfigErr = configPathErr

			staleMinutes, err := config.DoctorMayorStaleMinutes()
			if err != nil {
				return err
			}

			store := doctor.New(dir)
			mayorStale := doctor.NewMayorStale(vault, store)
			mayorStale.Home, mayorStale.Host = mwVault(vault, host), host
			mayorStale.Limit = time.Duration(staleMinutes) * time.Minute
			// One alarm, shared by every check that tells the Governor in its own
			// words: a Postern push of class alarm, and the emergency event. It
			// gives the seq of the emergency event, 0 where none was written.
			alarm := func(ctx context.Context, text string) (uint64, error) {
				_, err := handsPush{gateway: mwGateway(vault, host)}.Run(ctx, application.PosternSendRequest{Class: "alarm", Text: text})
				// The alarm also rides the emergency lane of the event log, so the
				// Governor's app hears it at once; the push is the alarm proper.
				var seq uint64
				if logPath, pathErr := config.EventsLogPath(); pathErr == nil {
					seq, _ = emitDoctorEmergency(ctx, eventlog.New(logPath), eventsClock, host, text, cmd.ErrOrStderr())
				}
				return alarmSeq(seq, err, cmd.ErrOrStderr())
			}
			// The way back of an alarm: one event in the normal lane, naming the
			// emergency it ends, so the Governor's app takes the banner down.
			clear := func(ctx context.Context, text string, clears uint64) error {
				logPath, err := config.EventsLogPath()
				if err != nil {
					return err
				}
				return emitDoctorClear(ctx, eventlog.New(logPath), eventsClock, host, text, clears, cmd.ErrOrStderr())
			}
			mayorStale.Alarm, mayorStale.Clear = alarm, clear
			// A Mayor whose every reply lately was an API error: its transcript
			// is read from where Claude Code keeps it. Where that is unknown the
			// check says it cannot tell.
			mayorStuck := doctor.NewMayorStuck(vault, store)
			mayorStuck.Home, mayorStuck.Host = mwVault(vault, host), host
			if projects, projectsErr := claude.DefaultProjectsRoot(); projectsErr == nil {
				mayorStuck.Replies = claude.NewTranscripts(projects)
			}
			mayorStuck.Alarm, mayorStuck.Clear = alarm, clear
			mayorStuck.Events = homeEventLog()
			batteryLow, batteryCritical, err := config.DoctorBatteryThresholds()
			if err != nil {
				return err
			}
			battery := doctor.NewBattery(doctor.DefaultBatteryDir, store)
			battery.Low, battery.Critical = batteryLow, batteryCritical
			battery.Alarm = func(ctx context.Context, text string) (uint64, error) {
				logPath, err := config.EventsLogPath()
				if err != nil {
					return 0, err
				}
				return emitDoctorEmergency(ctx, eventlog.New(logPath), eventsClock, host, text, cmd.ErrOrStderr())
			}
			battery.Clear = clear
			handsHosts, err := config.HandsHosts()
			if err != nil {
				return err
			}
			wg := doctor.NewWg(wgHub, reach, wgUnit, store)
			boostReach := doctor.NewBoostReach(mwVault(vault, host), host, handsHosts, store)
			boostReach.Alarm = alarm
			// The Boost answering again is still pushed to the phone, as its
			// alarm was; it is the event that rides the normal lane.
			boostReach.Clear = func(ctx context.Context, text string, clears uint64) error {
				_, pushErr := handsPush{gateway: mwGateway(vault, host)}.Run(ctx, application.PosternSendRequest{Class: "alarm", Text: text})
				if err := clear(ctx, text, clears); err != nil {
					return err
				}
				return pushErr
			}
			boostReach.WgFaulty = func(ctx context.Context) bool {
				verdict, _ := wg.Probe(ctx)
				return verdict == application.DoctorFaulty
			}
			// The threshold is mw status's own, read here but judged by the check:
			// a host_silent_hours this host cannot read is its to say.
			boostAsleep := doctor.NewBoostAsleep(mwVault(vault, host), host, mwGateway(vault, host))
			boostAsleep.Work = mwGateway(vault, host)
			boostAsleep.Signs = mwVault(vault, host)
			if hours, hoursErr := config.HostSilentHours(); hoursErr != nil {
				boostAsleep.LimitErr = hoursErr
			} else {
				boostAsleep.Limit = time.Duration(hours) * time.Hour
			}
			// Judged by the check itself, as beads_sync is: an age_key_file this
			// host cannot read is age-key's to say.
			ageKeyFile, ageKeyErr := config.AgeKeyFile()
			ageKey := doctor.NewAgeKey(mwVault(vault, host), host, vault, ageKeyFile)
			ageKey.PathErr = ageKeyErr
			// The token in claude.env ageing out, or the login gone (mw-gq6.830):
			// the alarm to the Governor is the same push the other alarms use.
			claudeLogin := doctor.NewClaudeLogin(claudeEnvFile())
			claudeLogin.Alarm = alarm
			claudeLogin.Screens = seatWindows().NotLoggedInWindows
			// A user unit that runs mw with no beads.env cannot reach the served
			// beads (mw-gq6.849); the alarm is the same push.
			unitEnv := doctor.NewUnitEnv()
			unitEnv.Alarm = alarm
			checks := application.DoctorChecks{}
			// The factory checkout the vault's formulas are judged against; none
			// where this host checks the rig out nowhere.
			factoryDir := ""
			if rigs, rigsErr := config.Rigs(); rigsErr == nil {
				factoryDir = rigs[application.FactoryRig]
			}
			formulasCheck := doctor.NewFormulas(vaultpkg.NewFormulas(mwVault(vault, host)), factoryDir, mwVault(vault, host), host, store)
			vpsNginx, err := hostVPSNginx(mwVault(vault, host), false)
			if err != nil {
				return err
			}
			if vpsNginx != nil {
				vpsCheck := doctor.NewVPSNginx(mwVault(vault, host), host, vpsNginx, store)
				vpsCheck.Alarm = alarm
				// The upstream put right is pushed to the phone, as its alarm was;
				// the event rides the normal lane.
				vpsCheck.Clear = func(ctx context.Context, text string, clears uint64) error {
					_, pushErr := handsPush{gateway: mwGateway(vault, host)}.Run(ctx, application.PosternSendRequest{Class: "alarm", Text: text})
					if err := clear(ctx, text, clears); err != nil {
						return err
					}
					return pushErr
				}
				checks = append(checks, vpsCheck)
			}
			// The names pointed at the VPS, looked at for a site behind each: the
			// hosts of the rigs' site_url and healthz_url, and [doctor] vps_names.
			vpsNames, err := hostVPSNames(cmd.Context(), mwVault(vault, host))
			if err != nil {
				return err
			}
			if vpsNames != nil {
				vpsNames.Note = func(ctx context.Context, text string) error {
					return mwGateway(vault, host).SetNote(ctx, application.DoctorNoteKey(host, doctor.VPSNamesName), text)
				}
				checks = append(checks, vpsNames)
			}
			// How full each seat window's session is, read as the follower's
			// watcher reads it: where Claude Code keeps the transcripts is
			// unknown on a host with no home, and the check says it cannot tell.
			var seatContext *doctor.SeatContext
			if projects, projectsErr := claude.DefaultProjectsRoot(); projectsErr != nil {
				seatContext = doctor.NewSeatContext(func(context.Context) ([]application.SeatWindowContext, error) {
					return nil, projectsErr
				})
			} else {
				seatContext = doctor.NewSeatContext((&application.SeatContextWatch{
					Limits:      config.SeatHandoffs,
					Dir:         vault,
					Transcripts: claude.NewTranscripts(projects),
					Terminal:    seatWindows(),
				}).Readings)
			}
			// The home's last flush is read from the file its follower job writes;
			// a path this host cannot work out is the check's to say.
			flushFile, flushFileErr := config.BeadsFlushFile()
			beadsFlushCheck := doctor.NewBeadsFlush(vault, func(ctx context.Context) (bool, error) {
				return application.IsHome(ctx, mwVault(vault, host), host)
			}, flushlog.New(flushFile))
			if flushFileErr != nil {
				beadsFlushCheck.Log = brokenFlushLog{flushFileErr}
			}
			tmpLeftovers := doctor.NewTmpLeftovers(os.TempDir())
			tmpLeftovers.Budget, tmpLeftovers.GoBuildBudget = tmpLeftoversBudget, goBuildBudget
			rootDisk := doctor.NewRootDiskBudget()
			rootDisk.Budget, rootDisk.Margin = rootDiskBudget, rootDiskMargin
			rootDisk.Note = func(ctx context.Context, text string) error {
				return mwGateway(vault, host).SetNote(ctx, application.DoctorNoteKey(host, doctor.RootDiskBudgetName), text)
			}
			dampers, err := damperSurvey(mwGateway(vault, host))
			if err != nil {
				return err
			}
			// A rig with ready stories and none running for over an hour, named with
			// what holds it; the cure is the one mail to the Mayor of its episode.
			starvedRigs := &application.StarvedRigCheck{
				Host:   host,
				Alarms: func(ctx context.Context) ([]application.DamperAlarm, error) { return dampers.Starved(ctx, time.Now()) },
				Mail: func(ctx context.Context, message application.NewMessage) error {
					_, err := mwGateway(vault, host).Send(ctx, message)
					return err
				},
			}
			// A re-run of a suite bug's spec that dispatch did not make in the last
			// day; the cure is one mail a day to the Mayor (mw-gq6.640).
			recheckSkips := &application.RecheckSkipCheck{
				Host: host,
				Log:  hostTickLogs().RecheckSkips,
				Mail: func(ctx context.Context, message application.NewMessage) error {
					_, err := mwGateway(vault, host).Send(ctx, message)
					return err
				},
			}
			// Landing directories a close-out could not take away, looked for in the
			// home directory and beside every rig, where a landing is made.
			landingDirs := []string{}
			if home, homeErr := os.UserHomeDir(); homeErr == nil {
				landingDirs = append(landingDirs, home)
			}
			for _, rigDir := range rigsAt {
				landingDirs = append(landingDirs, filepath.Dir(filepath.Clean(rigDir)))
			}
			staleLandings := &application.StaleLandingsCheck{Dirs: landingDirs}
			// The connection rate of a beads server this host runs; a host that
			// reaches another's, or has none, leaves it to the host that runs it.
			var churn application.DoctorChecks
			if beadsServedHere() {
				churnLimit, limitErr := config.DoctorBeadsChurnPerMinute()
				if limitErr != nil {
					return limitErr
				}
				beadsChurn := doctor.NewBeadsChurn(mwGateway(vault, host).ServerConnections, store)
				beadsChurn.Limit = float64(churnLimit)
				beadsChurn.Clients = (&doctor.BdParents{}).Sample
				churn = application.DoctorChecks{beadsChurn}
			}
			harness, err := config.NextHarness()
			if err != nil {
				return err
			}
			table := append(append(application.DoctorChecks{
				doctor.NewDaemonReload(units),
				doctor.NewWifi(reach, powershell, store),
				doctor.NewTunnel(tunnelHost, reach, tunnelUnit, tunnelProbe),
				wg,
				doctor.NewVaultDirty(vault, host, store),
				formulasCheck,
				doctor.NewTimers(units),
				&doctor.BeadsSize{Dir: vault, Budget: beadsBudget, Home: beadsServedHere(), Squash: beadsSquash},
				doctor.NewBeadsStores(vault),
				doctor.NewBeadsBatch(vault, mwGateway(vault, host).AutoCommit),
				beadsFlushCheck,
				tmpLeftovers,
				rootDisk,
				mayorStuck,
				mayorStale,
				mayorGone,
				seatContext,
				posternTranscribe,
				dispatchWindows,
				rigTests,
				boostConfig,
				beadsServerCheck,
				posternChannel,
				battery,
				boostReach,
				boostAsleep,
				starvedRigs,
				recheckSkips,
				staleLandings,
				ageKey,
				claudeLogin,
				unitEnv,
			}, churn...), checks...)
			// Where Claude Code keeps its transcripts is the only place these
			// two checks read; a host on codex has none, and says so once rather
			// than twice as "cannot tell" (mw-9rp4hu.14).
			if harness == domain.HarnessCodex {
				table = withoutChecks(table, mayorStuck.Name(), seatContext.Name())
				fmt.Fprintf(cmd.OutOrStdout(), "harness %s: skipping the transcript checks %s and %s, which read Claude Code's transcripts\n", harness, mayorStuck.Name(), seatContext.Name())
			}
			return runDoctor(cmd, application.Doctor{
				Dampers: dampers,
				Checks:  table,
				State:   store,
				Log:     store,
				Notes:   mwGateway(vault, host),
				// The verdict rides the same notes, so the home reads it with the
				// rest of this host's (mw-gq6.705).
				Verdicts: mwGateway(vault, host),
				Host:     host,
				Out:      cmd.OutOrStdout(),
			}, name, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what each faulty check would do; change nothing")
	cmd.Flags().StringVar(&envFile, "env-file", "", "for licences: the postern backend's environment file (default ~/.config/postern/env)")
	cmd.Flags().StringVar(&posternBin, "postern", "postern", "for licences: the postern binary the backend runs, asked for `rights --json`")
	cmd.AddCommand(newDoctorUnitFailedCmd())
	return cmd
}

// capacityArg is the word that makes mw doctor read a rig's capacity facts
// instead of working the table.
const capacityArg = "capacity"

// runDoctorCapacity prints the capacity facts the named rig's file in the vault
// records and leaves faulty, as a doctor does, when any is missing.
func runDoctorCapacity(cmd *cobra.Command, rest []string) error {
	if len(rest) != 1 {
		return fmt.Errorf("mw doctor capacity needs the rig to read, as in: mw doctor capacity leaderboard")
	}
	vault, err := config.Vault()
	if err != nil {
		return err
	}
	host, err := config.Host()
	if err != nil {
		return err
	}
	requirements, err := mwVault(vault, host).EpicRequirements(cmd.Context(), rest[0])
	if err != nil {
		return err
	}
	report := application.ReportCapacity(rest[0], requirements.Capacity)
	fmt.Fprint(cmd.OutOrStdout(), report)
	if report.Faulty() {
		cmd.SilenceErrors = true
		return &application.DoctorFault{Line: capacityArg}
	}
	return nil
}

// licencesArg is the word that makes mw doctor read the backend's licences
// instead of working the table.
const licencesArg = "licences"

// posternRightsWait bounds `postern rights --json`: an older postern has no
// such subcommand and starts its server instead, which would never answer.
const posternRightsWait = 20 * time.Second

// runDoctorLicences prints the licence map the postern binary gives for its
// environment file and leaves faulty, as a doctor does, when it warns of
// anything. The rules are postern's: when postern cannot give the map, this
// says so in one line and fails; no rule of mw's stands in for it.
func runDoctorLicences(cmd *cobra.Command, posternBin, envFile string) error {
	if envFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("there is no home directory to find the backend's environment file in: %w", err)
		}
		envFile = filepath.Join(home, ".config", "postern", "env")
	}
	entries, err := readPosternRights(cmd.Context(), posternBin, envFile)
	if err != nil {
		return err
	}
	report := application.ReportLicences(entries)
	fmt.Fprint(cmd.OutOrStdout(), report)
	if report.Faulty() {
		cmd.SilenceErrors = true
		return &application.DoctorFault{Line: licencesArg}
	}
	return nil
}

// readPosternRights runs `<postern> rights --json --env-file <envFile>` and
// reads its map. Every failure is one line that names the command.
func readPosternRights(ctx context.Context, posternBin, envFile string) ([]application.LicenceEntry, error) {
	const command = "postern rights --json"
	path, err := exec.LookPath(posternBin)
	if err != nil {
		return nil, fmt.Errorf("mw doctor licences needs the postern binary the backend runs, to ask it for `%s`, and cannot find %q on this host", command, posternBin)
	}
	ctx, cancel := context.WithTimeout(ctx, posternRightsWait)
	defer cancel()
	var stdout, stderr bytes.Buffer
	run := exec.CommandContext(ctx, path, "rights", "--json", "--env-file", envFile)
	run.Stdout, run.Stderr = &stdout, &stderr
	if err := run.Run(); err != nil {
		reason := oneLine(stderr.String())
		if ctx.Err() != nil {
			reason = fmt.Sprintf("no answer in %s; this postern is probably older and has no rights subcommand", posternRightsWait)
		} else if reason == "" {
			reason = oneLine(err.Error())
		}
		return nil, fmt.Errorf("%s: `%s` failed: %s", path, command, reason)
	}
	entries, err := application.ParseRightsJSON(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("%s: `%s` gave no licence map (is this postern older, without the rights subcommand?): %s", path, command, oneLine(err.Error()))
	}
	return entries, nil
}

// oneLine folds text onto a single line.
func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

// withoutChecks is the table less the checks named.
func withoutChecks(table application.DoctorChecks, names ...string) application.DoctorChecks {
	kept := make(application.DoctorChecks, 0, len(table))
	for _, check := range table {
		if !slices.Contains(names, check.Name()) {
			kept = append(kept, check)
		}
	}
	return kept
}

// runDoctor runs the doctor. A check left faulty and uncured is an outcome
// and not a failure: the report naming it has already been printed, so cobra
// is not to print the error too, and only the status it leaves with says so.
func runDoctor(cmd *cobra.Command, doc application.Doctor, name string, dryRun bool) error {
	_, err := doc.Run(cmd.Context(), name, dryRun)
	if _, fault := application.DoctorFaults(err); fault {
		cmd.SilenceErrors = true
	}
	return err
}

// emitDoctorEmergency puts an alarm's text in the emergency lane of log, cut
// to what an emergency event may carry, and gives the seq it was written at.
// A write that is refused or fails is said on errOut and returned, with seq 0:
// mayor-stale's push is its alarm proper, so it drops the error, while the
// battery check, whose alarm this is, retries.
func emitDoctorEmergency(ctx context.Context, log application.EventLog, now func() time.Time, host, text string, errOut io.Writer) (uint64, error) {
	ev, err := application.EventEmit{
		Log: log, Now: now, Emergency: true,
		Event: events.Event{Kind: events.KindJob, Actor: "doctor@" + host, From: events.JobRunning, To: events.JobFailed, Detail: events.CutDetail(text)},
	}.Run(ctx)
	if err != nil {
		fmt.Fprintf(errOut, "mw doctor: emergency event: not written: %v\n", err)
		return 0, err
	}
	return ev.Seq, nil
}

// alarmSeq is what a doctor alarm gives its check: the seq of the emergency
// event and the push's error. A push that failed after the emergency event was
// written is only said, with the seq returned and no error: the check would
// otherwise try the alarm again at its next pass, writing a second emergency,
// and the first, whose seq it never kept, could never be cleared.
func alarmSeq(seq uint64, pushErr error, errOut io.Writer) (uint64, error) {
	if seq != 0 && pushErr != nil {
		fmt.Fprintf(errOut, "mw doctor: alarm push: not sent, the emergency event (seq %d) was: %v\n", seq, pushErr)
		return seq, nil
	}
	return seq, pushErr
}

// emitDoctorClear puts the end of an alarm in the normal lane of log: a job
// going running to done, not the running to failed of the alarm, whose clears
// names the seq of the emergency it ends (0 where the alarm kept none). A
// write that is refused or fails is said on errOut and returned.
func emitDoctorClear(ctx context.Context, log application.EventLog, now func() time.Time, host, text string, clears uint64, errOut io.Writer) error {
	_, err := application.EventEmit{
		Log: log, Now: now,
		Event: events.Event{Kind: events.KindJob, Actor: "doctor@" + host, From: events.JobRunning, To: events.JobDone, Detail: events.CutDetail(text), Clears: clears},
	}.Run(ctx)
	if err != nil {
		fmt.Fprintf(errOut, "mw doctor: clearing event: not written: %v\n", err)
	}
	return err
}

// brokenFlushLog is the beads flush log of a host whose file path cannot be
// worked out: every read says why.
type brokenFlushLog struct{ err error }

func (b brokenFlushLog) Last(context.Context) (application.BeadsFlushRecord, error) {
	return application.BeadsFlushRecord{}, b.err
}

func (b brokenFlushLog) Record(context.Context, application.BeadsFlushRecord) error { return b.err }

// beadsServedHere says whether this host's beads server runs on this host: the
// environment bd reads names a server at a loopback address. mw doctor's
// beads-churn check reads that server's connection count, and a host that only
// reaches another's would raise the same alarm twice.
func beadsServedHere() bool {
	switch strings.TrimSpace(os.Getenv(config.BeadsDoltServerHostEnv)) {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	return false
}
