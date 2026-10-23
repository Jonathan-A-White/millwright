package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

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
			"PROVISIONAL: the usage is what the Governor reports, from his screenshot, until a source exists.",
	}
	cmd.AddCommand(newUsageSetCmd(), newUsageShowCmd())
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
				Readings: application.UsageReadings{Notes: mwGateway(dir, host)}, Zone: zone, Out: out,
				Pause: func(ctx context.Context, reason string) error {
					p, err := pauseHome(ctx, dir, host, out, 0)
					if err != nil {
						return err
					}
					_, err = p.Run(ctx, application.PauseRequest{Reason: reason, Wait: application.PauseWait})
					return err
				},
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
			all, err := application.UsageReadings{Notes: mwGateway(dir, host)}.All(cmd.Context())
			if err != nil {
				return err
			}
			if len(all) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no usage was reported: mw usage set <percent>")
				return nil
			}
			last := all[len(all)-1]
			fmt.Fprintf(cmd.OutOrStdout(), "last reading: %.1f%% at %s; the week resets %s\n",
				last.Percent, last.At.UTC().Format(time.RFC3339), last.Resets.UTC().Format(time.RFC3339))
			return application.UsageCheck{Reading: last, Out: cmd.OutOrStdout()}.Run(cmd.Context(), time.Now().UTC())
		},
	}
}
