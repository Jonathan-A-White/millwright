package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"

	"github.com/spf13/cobra"
)

// newUsageCmd builds `mw usage`: the quota used, as the Governor reports it,
// read against the reserve that trips mw pause (mw-gq6.707).
func newUsageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "The quota used, as the Governor reports it, and the reserve that pauses the factory",
		Long: "The reserve R(t) is 10% of the quota with a whole week left, 0% with an hour left, and a straight\n" +
			"line between. When the usage reported has reached 100% less R(t), mw pause runs by itself.\n" +
			"A floor (mw usage floor) keeps the reserve at no less than a percent until a time, then R(t) alone.\n" +
			"The usage is read from Claude's /usage screen by mw usage read, which a user timer runs every 30\n" +
			"minutes; mw usage set is the Governor's report by hand, for when the screen cannot be read.",
	}
	cmd.AddCommand(newUsageSetCmd(), newUsageReadCmd(), newUsageShowCmd(), newUsageFloorCmd())
	return cmd
}

func newUsageSetCmd() *cobra.Command {
	var resets string
	cmd := &cobra.Command{
		Use:   "set <percent> [--resets <RFC3339>]",
		Short: "Report the quota used; pause the factory if it has reached 100% less the reserve",
		Long: "set keeps the reading, then reads it against the reserve and, when the usage has reached 100% less\n" +
			"R(t), runs mw pause with the reading as its reason (the pause's end computed as mw pause computes\n" +
			"it). --resets is when the week resets; without it, the last reading's if still ahead, else the\n" +
			"next Friday 09:00 America/New_York. Run on the home.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			percent, err := strconv.ParseFloat(args[0], 64)
			if err != nil {
				return fmt.Errorf("%q is not a percent: %w", args[0], err)
			}
			var at time.Time
			if resets != "" {
				if at, err = time.Parse(time.RFC3339, resets); err != nil {
					return fmt.Errorf("--resets %q is not a time like 2026-10-23T13:00:00Z: %w", resets, err)
				}
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			zone, err := time.LoadLocation("America/New_York")
			if err != nil {
				zone = time.UTC
			}
			out := cmd.OutOrStdout()
			return application.UsageSet{
				Readings: application.UsageReadings{Notes: mwGateway(dir, host)}, Floors: application.UsageFloors{Notes: mwGateway(dir, host)},
				Zone: zone, Out: out,
				Pause: usagePause(dir, host, out),
			}.Run(cmd.Context(), percent, at)
		},
	}
	cmd.Flags().StringVar(&resets, "resets", "", "when the week resets (RFC3339)")
	return cmd
}

func newUsageShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the last reading and where it stands against the reserve; changes nothing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			gw := mwGateway(dir, host)
			return application.UsageShow{
				Readings: application.UsageReadings{Notes: gw}, Floors: application.UsageFloors{Notes: gw}, Out: cmd.OutOrStdout(),
			}.Run(cmd.Context())
		},
	}
}

func newUsageFloorCmd() *cobra.Command {
	var until string
	cmd := &cobra.Command{
		Use:   "floor <percent> --until <RFC3339> | off",
		Short: "Keep the reserve at no less than a percent until a time, then R(t) alone; off clears it",
		Long: "floor keeps the reserve at max(R(t), percent) while now is before --until, so that a tutor's answers\n" +
			"are not starved of tokens before the reset; from --until on the reserve is R(t) alone, down to 0%\n" +
			"in the last hour. mw usage set and mw usage show read it. floor off clears it. Run on the home.",
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
			use := application.UsageFloorSet{Floors: application.UsageFloors{Notes: mwGateway(dir, host)}, Out: cmd.OutOrStdout()}
			if args[0] == "off" {
				return use.Off(cmd.Context())
			}
			percent, err := strconv.ParseFloat(args[0], 64)
			if err != nil {
				return fmt.Errorf("%q is not a percent or off: %w", args[0], err)
			}
			if until == "" {
				return fmt.Errorf("--until is needed: when the floor ends, like 2026-10-30T11:00:00Z")
			}
			at, err := time.Parse(time.RFC3339, until)
			if err != nil {
				return fmt.Errorf("--until %q is not a time like 2026-10-30T11:00:00Z: %w", until, err)
			}
			return use.Run(cmd.Context(), percent, at)
		},
	}
	cmd.Flags().StringVar(&until, "until", "", "when the floor ends (RFC3339)")
	return cmd
}

// UsageStateDir is where mw usage read keeps its lock and the last capture of
// the /usage screen, under the home directory.
var UsageStateDir = filepath.Join(".local", "state", "mw-usage")

const (
	usageReadLockFile    = "read.lock"
	usageReadCaptureFile = "capture.txt"
)

func newUsageReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read",
		Short: "Read the quota used off Claude's /usage screen through tmux and set it, as mw usage set does",
		Long: "read opens a throwaway tmux session of its own (named mw-usage-probe-<pid>-<time>, never a seat's),\n" +
			"starts claude in the vault, types /usage, captures the screen and closes the session. From the\n" +
			"'Current week (all models)' block it takes the percent used and when the week resets, and does what\n" +
			"mw usage set <percent> --resets <time> does; it keeps the 'Current week (Fable)' percent beside\n" +
			"the reading, and mw usage show prints it. /usage spends no tokens.\n" +
			"On a screen it cannot read it changes nothing, says so, leaves non-zero and keeps the capture in\n" +
			"~/.local/state/mw-usage/capture.txt. Two reads never run at once: a second says one is under way\n" +
			"and does nothing. A user timer runs it every 30 minutes (docs/fuel-knobs.md). Run on the home.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			state := filepath.Join(home, UsageStateDir)
			zone, err := time.LoadLocation("America/New_York")
			if err != nil {
				zone = time.UTC
			}
			out := cmd.OutOrStdout()
			return application.UsageRead{
				Terminal: tmux.NewTerminal(), Lock: hostlock.NewTry(state, usageReadLockFile),
				Dir: dir, Zone: zone,
				Set: application.UsageSet{
					Readings: application.UsageReadings{Notes: mwGateway(dir, host)}, Floors: application.UsageFloors{Notes: mwGateway(dir, host)},
					Zone: zone, Out: out,
					Pause: usagePause(dir, host, out),
				},
				Keep: func(text string) (string, error) {
					if err := os.MkdirAll(state, 0o700); err != nil {
						return "", err
					}
					path := filepath.Join(state, usageReadCaptureFile)
					return path, os.WriteFile(path, []byte(text), 0o600)
				},
				Name: func() string { return fmt.Sprintf("mw-usage-probe-%d-%d", os.Getpid(), time.Now().UnixNano()) },
				Out:  out,
			}.Run(cmd.Context())
		},
	}
}

// usagePause is the pause a reading that has reached the reserve runs, with the
// reading as its reason.
func usagePause(dir, host string, out io.Writer) func(ctx context.Context, reason string) error {
	return func(ctx context.Context, reason string) error {
		p, err := pauseHome(ctx, dir, host, out, 0)
		if err != nil {
			return err
		}
		_, err = p.Run(ctx, application.PauseRequest{Reason: reason, Wait: application.PauseWait})
		return err
	}
}
