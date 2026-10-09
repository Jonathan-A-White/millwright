package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/cloud"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// CloudJobName is the follower's job that runs the cloud check.
const CloudJobName = "cloud"

// cloudEvery is how often the follower runs the cloud check.
const cloudEvery = time.Minute

// newCloudCmd builds `mw cloud`: the elastic cloud of the [cloud] table.
func newCloudCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "The cloud boxes made and destroyed by demand",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Make a cloud box if stories wait and every host is full, destroy an idle one",
		Long: "check is one look at the cloud, which the home's follower makes every minute: when more\n" +
			"stories wait for any host than there are sessions free across the hosts of [cloud.caps]\n" +
			"and the boxes, it makes one box with contrib/vultr-boost; a box with no story for\n" +
			"idle_minutes is destroyed; and no box is made, or kept, past monthly_cap_usd. Each move\n" +
			"is a cloud event and a line of mw status. It runs on the home only.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			check, ok, err := cloudCheck(cmd.Context(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(cmd.OutOrStdout(), "mw cloud: no [cloud] table in the config, or this host is not home: nothing to do")
				return nil
			}
			report, ran, err := runCloudCheck(cmd.Context(), check)
			if !ran {
				fmt.Fprintln(cmd.OutOrStdout(), "mw cloud: another cloud check is running here; this one did nothing")
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), report)
			return err
		},
	})
	return cmd
}

// cloudCheck is the cloud check of this host, and whether there is one to
// run: a host with no [cloud] table, or that is not home, has none.
func cloudCheck(ctx context.Context, out io.Writer) (application.CloudCheck, bool, error) {
	settings, ok, err := config.Cloud()
	if err != nil || !ok {
		return application.CloudCheck{}, false, err
	}
	dir, err := config.Vault()
	if err != nil {
		return application.CloudCheck{}, false, err
	}
	host, err := config.Host()
	if err != nil {
		return application.CloudCheck{}, false, err
	}
	files := mwVault(dir, host)
	if home, err := application.IsHome(ctx, files, host); err != nil || !home {
		return application.CloudCheck{}, false, err
	}
	plan, err := cloudPlan(settings, host)
	if err != nil {
		return application.CloudCheck{}, false, err
	}
	return application.CloudCheck{
		Work:     mwGateway(dir, host),
		Provider: cloud.Vultr{Command: settings.Command, Snapshot: settings.Snapshot, Out: out},
		Book:     vault.CloudBook{Vault: files},
		Log:      homeEventLog(),
		Host:     host,
		Plan:     plan,
		Out:      out,
	}, true, nil
}

// cloudPlan is the [cloud] table as the check reads it. Without [cloud.caps]
// the hosts are this one alone, at its own cap.
func cloudPlan(settings config.CloudSettings, host string) (application.CloudPlan, error) {
	caps := settings.HostCaps
	if len(caps) == 0 {
		n, err := config.Cap()
		if err != nil {
			return application.CloudPlan{}, err
		}
		caps = map[string]int{host: n}
	}
	return application.CloudPlan{
		MaxBoxes:      settings.MaxBoxes,
		MonthlyCapUSD: settings.MonthlyCapUSD,
		HourlyUSD:     settings.HourlyUSD,
		Idle:          time.Duration(settings.IdleMinutes) * time.Minute,
		BoxCap:        settings.BoxCap,
		HostCaps:      caps,
	}, nil
}

// cloudSpringJob is the follower's cloud job: the cloud check every minute,
// on a host whose config has a [cloud] table, and only while it is home.
func cloudSpringJob(out io.Writer) (application.SpringJob, bool, error) {
	if _, ok, err := config.Cloud(); err != nil || !ok {
		return application.SpringJob{}, false, err
	}
	return application.ClockJob(CloudJobName, cloudEvery, func(ctx context.Context) error {
		check, ok, err := cloudCheck(ctx, out)
		if err != nil || !ok {
			return err
		}
		_, _, err = runCloudCheck(ctx, check)
		return err
	}), true, nil
}

// cloudLockFile is the lock one cloud check holds while it runs, beside the
// dispatch lock: the follower's check and one by hand never make a box at once.
const cloudLockFile = "cloud.lock"

// runCloudCheck runs the check under this host's cloud lock, and reports
// whether it ran: a check that finds the lock held does nothing.
func runCloudCheck(ctx context.Context, check application.CloudCheck) (application.CloudReport, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return application.CloudReport{}, false, err
	}
	release, taken, err := hostlock.NewTry(filepath.Join(home, DispatchStateDir), cloudLockFile).TryTake(ctx)
	if err != nil || !taken {
		return application.CloudReport{}, false, err
	}
	defer release()
	report, err := check.Run(ctx)
	return report, true, err
}

// cloudStatus is the cloud's book and plan for mw status, or nil and the
// zero plan where the config has no [cloud] table.
func cloudStatus(dir, host string) (application.CloudBook, application.CloudPlan) {
	settings, ok, err := config.Cloud()
	if err != nil || !ok {
		return nil, application.CloudPlan{}
	}
	plan, err := cloudPlan(settings, host)
	if err != nil {
		return nil, application.CloudPlan{}
	}
	return vault.CloudBook{Vault: mwVault(dir, host)}, plan
}
