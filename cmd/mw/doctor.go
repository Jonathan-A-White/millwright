package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"

	"github.com/spf13/cobra"
)

// newDoctorCmd builds `mw doctor`: a table of checks, each its own probe,
// cure, damper and way back, run on this host and logged here. It calls
// nothing but a check's own probe or cure, and never AI or mail itself: a
// check left needing a person is a beads note, one key per check, and
// `mw millhand tick` is what wakes the Millhand for it. The only pushes are
// the alarms to the Governor, mayor-stale's and boost-reach's, sent from their cures.
func newDoctorCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "doctor [check]",
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
			"journal shows it.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
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
			beadsBudget, err := config.BeadsBudgetBytes()
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

			posternChannel := doctor.NewPosternChannel(mwVault(vault, host), host)

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
				return seq, err
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
			tmpLeftovers := doctor.NewTmpLeftovers(os.TempDir())
			tmpLeftovers.Budget = tmpLeftoversBudget
			return runDoctor(cmd, application.Doctor{
				Checks: application.DoctorChecks{
					doctor.NewDaemonReload(units),
					doctor.NewWifi(reach, powershell, store),
					doctor.NewTunnel(tunnelHost, reach, tunnelUnit, tunnelProbe),
					wg,
					doctor.NewVaultDirty(vault, host, store),
					doctor.NewTimers(units),
					&doctor.BeadsSize{Dir: vault, Budget: beadsBudget},
					doctor.NewBeadsStores(vault),
					tmpLeftovers,
					mayorStale,
					mayorGone,
					posternTranscribe,
					beadsServerCheck,
					posternChannel,
					battery,
					boostReach,
				},
				State: store,
				Log:   store,
				Notes: mwGateway(vault, host),
				Host:  host,
				Out:   cmd.OutOrStdout(),
			}, name, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what each faulty check would do; change nothing")
	return cmd
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
