package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/pause"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// pauseSeats are the seats whose ledgers the burn is read from.
var pauseSeats = []string{"builder", "mayor", "deputy", "millhand"}

// newPauseCmd builds `mw pause`: the factory stopped at the Governor's word
// (mw-gq6.707).
func newPauseCmd() *cobra.Command {
	var reason, until, bead string
	var wait time.Duration
	var dryRun, here bool
	var rate float64
	var order string
	cmd := &cobra.Command{
		Use:   "pause --reason \"<his words>\" [--until <RFC3339>] [--bead <id>] [--dry-run]",
		Short: "Pause the factory: hold every host, check every Builder's work in to its branch, bring the cloud down",
		Long: "pause is run on the home. In order:\n" +
			"  1. every host gets the usage-limit hold note (host.<host>.harness_hold) until the time the\n" +
			"     pause lasts, so it claims nothing new: no cap is set to 0 and no dispatch unit is masked,\n" +
			"     and the hosts are the home, those of [hands_hosts] in the config file, and the cloud boxes up\n" +
			"     (a box without a [hands_hosts] line is reached at ssh mw@<box>.mw), each asked by ssh to run\n" +
			"     mw pause --here;\n" +
			"  2. the PAUSE line is typed into every running Builder's session, and each host waits up to\n" +
			"     --wait for its branch to be pushed and its story to say PAUSED; what a Builder did not do mw\n" +
			"     does itself: it commits what is left in the worktree, pushes the branch and writes the\n" +
			"     PAUSED comment;\n" +
			"  3. cloud.max_boxes is set to 0 (the old value kept), each cloud box's sessions are ended, and a\n" +
			"     box whose branches are all pushed is brought down;\n" +
			"  4. the paused set (story, host, window, branch and commit, PAUSED comment) is kept in\n" +
			"     plans/pause-<date>/paused.json in the vault, written on --bead if one is given, mailed to the\n" +
			"     Mayor, and printed.\n\n" +
			"--until sets the hold notes' end and schedules mw resume at that time as a one-shot systemd user\n" +
			"timer (mw-resume-<date>), and prints its unit. Without it the end is computed (PROVISIONAL): the\n" +
			"moment from which the quota left, burned at the measured full-throttle rate, runs out 15 minutes\n" +
			"before the reset, from the usage the Governor reported with mw usage set; --dry-run prints the\n" +
			"sum and changes nothing. With no usage report the pause lasts a week and mw resume ends it.\n\n" +
			"A second pause changes nothing and says so, except that it reaches a host the first could not.\n" +
			"Neither command touches the mw-seats windows: the Mayor and the other seats are not Builders.",
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
			if here {
				var o application.HostPauseOrder
				if err := json.Unmarshal([]byte(order), &o); err != nil {
					return fmt.Errorf("--order does not read: %w", err)
				}
				local, err := pauseHostSide(dir, host)
				if err != nil {
					return err
				}
				got, err := local.Pause(cmd.Context(), o)
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(got)
			}
			req := application.PauseRequest{Reason: reason, Bead: bead, DryRun: dryRun, Wait: wait}
			if until != "" {
				at, err := time.Parse(time.RFC3339, until)
				if err != nil {
					return fmt.Errorf("--until %q is not a time like 2026-10-23T13:00:00Z: %w", until, err)
				}
				req.Until = at
			}
			p, err := pauseHome(cmd.Context(), dir, host, cmd.OutOrStdout(), rate)
			if err != nil {
				return err
			}
			_, err = p.Run(cmd.Context(), req)
			return err
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "the Governor's words for the pause (required)")
	cmd.Flags().StringVar(&until, "until", "", "when the hold notes end and mw resume runs, as a one-shot timer (RFC3339)")
	cmd.Flags().StringVar(&bead, "bead", "", "a bead to write the paused set on")
	cmd.Flags().DurationVar(&wait, "wait", application.PauseWait, "how long to wait for the Builders to push before mw does it for them")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the resume sum and what would be paused, and change nothing")
	cmd.Flags().Float64Var(&rate, "rate", 0, "the burn at full throttle in percent of the quota an hour, in place of the measured one")
	cmd.Flags().BoolVar(&here, "here", false, "this host's part only, as the home asks it over ssh; prints JSON")
	cmd.Flags().StringVar(&order, "order", "", "with --here: the order, as JSON")
	_ = cmd.Flags().MarkHidden("here")
	_ = cmd.Flags().MarkHidden("order")
	return cmd
}

// pauseHostSide is this host's part of a pause or resume.
func pauseHostSide(dir, host string) (application.FleetHostPause, error) {
	rigs, err := config.Rigs()
	if err != nil {
		return application.FleetHostPause{}, err
	}
	cap := ""
	if n, err := config.Cap(); err == nil {
		cap = strconv.Itoa(n)
	}
	gateway := mwGateway(dir, host)
	return application.FleetHostPause{
		Host: host, Notes: gateway, Stories: gateway, Runner: tmux.New(), Git: rig.New(), Rigs: rigs, Cap: cap,
	}, nil
}

// pauseHome is everything the home's pause or resume works with.
type pauseHomeParts struct {
	files  *vault.Vault
	book   pause.Book
	fleet  pause.Fleet
	cloud  application.PauseCloud
	config pause.Config
	timer  pause.Timer
	mw     string
	host   string
	gw     interface {
		CommentOnStory(ctx context.Context, id, text string) error
	}
}

func pauseParts(ctx context.Context, dir, host string, out io.Writer) (pauseHomeParts, error) {
	files := mwVault(dir, host)
	home, err := application.IsHome(ctx, files, host)
	if err != nil {
		return pauseHomeParts{}, err
	}
	if !home {
		return pauseHomeParts{}, fmt.Errorf("%s is not the home: mw pause and mw resume are run on the home", host)
	}
	self, err := os.Executable()
	if err != nil {
		return pauseHomeParts{}, fmt.Errorf("finding mw to resume with: %w", err)
	}
	local, err := pauseHostSide(dir, host)
	if err != nil {
		return pauseHomeParts{}, err
	}
	cloud := pauseCloud(ctx, out)
	reach, err := storyReachOf(ctx, cloud)
	if err != nil {
		return pauseHomeParts{}, err
	}
	cfgPath, err := config.FilePath()
	if err != nil {
		return pauseHomeParts{}, err
	}
	gateway := mwGateway(dir, host)
	return pauseHomeParts{
		files:  files,
		book:   pause.Book{Root: dir, Files: files, Notes: gateway},
		fleet:  pause.Fleet{Home: host, Local: local, Reach: reach},
		cloud:  cloud,
		config: pause.Config{Path: cfgPath, Out: out},
		timer:  pause.Timer{Mw: self, Path: os.Getenv("PATH")},
		mw:     self,
		host:   host,
		gw:     gateway,
	}, nil
}

func pauseHome(ctx context.Context, dir, host string, out io.Writer, rate float64) (application.Pause, error) {
	parts, err := pauseParts(ctx, dir, host, out)
	if err != nil {
		return application.Pause{}, err
	}
	gateway := mwGateway(dir, host)
	planner := application.ResumePlanner{
		Readings: application.UsageReadings{Notes: gateway},
		Burn:     application.LedgerBurn{Vault: parts.files, Seats: pauseSeats},
		Rate:     rate,
	}
	return application.Pause{
		Fleet: parts.fleet, Cloud: parts.cloud, Config: parts.config, Book: parts.book, Timer: parts.timer,
		Mail: gateway, Tracker: gateway, Plan: planner.Plan, Host: host, Out: out,
	}, nil
}

// pauseCloud is the cloud as a pause sees it: the boxes the book holds, and
// the cloud's own down. A home with no [cloud] table has none.
func pauseCloud(ctx context.Context, out io.Writer) application.PauseCloud {
	check, ok, err := cloudCheck(ctx, out)
	return cloudOfPause{check: check, ok: ok && err == nil}
}

type cloudOfPause struct {
	check application.CloudCheck
	ok    bool
}

func (c cloudOfPause) Boxes(ctx context.Context) ([]string, error) {
	if !c.ok {
		return nil, nil
	}
	state, err := c.check.Book.Read(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, box := range state.Boxes {
		if !box.Making {
			names = append(names, box.Name)
		}
	}
	return names, nil
}

func (c cloudOfPause) Down(ctx context.Context, box string) (string, error) {
	if !c.ok {
		return "", fmt.Errorf("no [cloud] table in the config, or its check could not be made: no box is destroyed")
	}
	return downCloudBox(ctx, c.check, box)
}
