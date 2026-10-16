package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
			"is a cloud event and a line of mw status. It runs on the home only.\n\n" +
			"A box is not waited for: the check writes it into the book as being made and starts\n" +
			"the make in a session of its own, which a stopped caller does not stop; the book says\n" +
			"when it is up.",
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
	cmd.AddCommand(newCloudMakeCmd())
	cmd.AddCommand(newCloudCapCmd())
	return cmd
}

// newCloudCapCmd builds `mw cloud cap <n>`: box_cap, changed where the cloud
// keeps it, for the next cloud check to set on the boxes already up.
func newCloudCapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cap <n>",
		Short: "Set how many sessions each cloud box runs at once, boxes up now included",
		Long: "cap writes box_cap, n from 1 to 8, in the [cloud] table of the home's ~/.config/mw/config.toml,\n" +
			"in the vault's copy of it (hosts/vultr/cloud-table.toml) and as cap in\n" +
			"hosts/vultr/vultr-boost.tfvars, which the next box is made with, and commits the vault's two.\n" +
			"It prints the old value, the way back. It reaches no box: the next cloud check, within a\n" +
			"minute, sets the cap on each box up, raised or lowered; running sessions finish and none\n" +
			"starts above a lowered cap. It runs on the home only.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("a box runs 1 to %d sessions at once, not %q", application.CloudBoxCapMax, args[0])
			}
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
			files := mwVault(dir, host)
			report, err := application.CloudCap{
				Files: cloud.CapFiles{Config: filepath.Join(home, config.File), Vault: files},
				Home:  files,
				Host:  host,
			}.Run(cmd.Context(), n)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), report)
			return nil
		},
	}
}

// newCloudMakeCmd builds `mw cloud make <box>`: the process the cloud check
// begins, in a session of its own, for a box it wrote into the book as being
// made. Hidden: nobody runs it by hand.
func newCloudMakeCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "make <box>",
		Short:  "Make one cloud box the check began, and write how it ended into the book",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			check, ok, err := cloudCheck(cmd.Context(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no [cloud] table in the config, or this host is not home: no box is made")
			}
			return application.CloudMake{
				Provider: check.Provider,
				Book:     check.Book,
				Log:      check.Log,
				Notice:   check.Notice,
				Host:     check.Host,
				Plan:     check.Plan,
				Out:      check.Out,
				Lock:     waitForCloudLock,
			}.Run(cmd.Context(), args[0])
		},
	}
}

// cloudMakeLockWait is how long a make waits for a cloud check's lock: a check
// holds it for a moment, a minute at most.
const cloudMakeLockWait = 10 * time.Minute

// waitForCloudLock takes this host's cloud lock, waiting for a check that holds
// it.
func waitForCloudLock(ctx context.Context) (func(), error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, cloudMakeLockWait)
	defer cancel()
	lock := hostlock.NewTry(filepath.Join(home, DispatchStateDir), cloudLockFile)
	for {
		release, taken, err := lock.TryTake(ctx)
		if err != nil || taken {
			return release, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
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
	self, err := os.Executable()
	if err != nil {
		return application.CloudCheck{}, false, fmt.Errorf("finding mw to make a box with: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return application.CloudCheck{}, false, err
	}
	gateway := mwGateway(dir, host)
	return application.CloudCheck{
		Work:  gateway,
		Notes: gateway,
		Provider: cloud.Vultr{
			Command: settings.Command, Snapshot: settings.Snapshot, Out: out,
			UpLimit: time.Duration(settings.UpMinutes) * time.Minute,
			Self:    self, MakeLog: filepath.Join(home, DispatchStateDir, cloudMakeLog),
		},
		Book:   vault.CloudBook{Vault: files},
		Log:    homeEventLog(),
		Notice: governorNotice{},
		Host:   host,
		Plan:   plan,
		Out:    out,
	}, true, nil
}

// governorNotice is application.CloudNotice over mw card send: a card to the
// Governor, sealed to his key. A host that cannot send a card says so, and the
// cloud check goes on without.
type governorNotice struct{}

func (governorNotice) Tell(ctx context.Context, title, text string) error {
	cards, err := posternCards(nil)
	if err != nil {
		return err
	}
	_, _, err = cards.Send(ctx, application.CardSendRequest{Title: title, Items: []string{strings.NewReplacer("|", "/", "\n", " ").Replace(text)}})
	return err
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
		Up:            time.Duration(settings.UpMinutes) * time.Minute,
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

// cloudMakeLog is the file the processes making boxes append their output to,
// beside the lock.
const cloudMakeLog = "cloud-make.log"

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
