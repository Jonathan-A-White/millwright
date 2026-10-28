package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/reportlog"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newReportSendCmd builds `mw report send`: the week's summary, posted to the
// Governor's Factory channel once a UTC day, from the home.
func newReportSendCmd() *cobra.Command {
	var force, dryRun bool
	var class string
	cmd := &cobra.Command{
		Use:   "send [--class factory|parent] [--force] [--dry-run]",
		Short: "Post the phone-sized summary of the week to the Factory channel, once a day, from the home",
		Long: "send builds the week's summary from the same numbers as mw report, in at most 19 lines of\n" +
			"60 columns (QUALITY with the investment share against its target, SPEED and FUEL, each\n" +
			"number with its arrow against the week before, then MODELS a line a model, and CHEAP WINS),\n" +
			"and posts it as a plain message to the Factory channel, printing its txid.\n" +
			"It goes out from the home alone: on any other host it says which is home and sends\n" +
			"nothing. It sends at most once a UTC day, remembering the day in\n" +
			"~/.local/state/mw/report/sent.jsonl; a second run says when it sent. --force sends again;\n" +
			"--dry-run prints the summary and sends and remembers nothing. Beside the post it writes what\n" +
			"mw report only shows: a factory change that went WORSE (FACTORY CHANGES) is mailed to the\n" +
			"Mayor once, and the day's key metrics are appended to metrics/factory.jsonl in the vault,\n" +
			"one snapshot a day. mw-report.timer runs it at 12:00 UTC.\n" +
			"--class parent is a member's factory writing to its parent instead: a few plain sentences,\n" +
			"with no bead id, story title, rig, model or fuel figure: how many areas of work moved, roughly\n" +
			"how much was finished, whether something did not pass its checks, and how many things wait on\n" +
			"a decision; a day with nothing moved is one line. It posts to postern_governor_key on this\n" +
			"factory's own backend, from any host, at the local hour of the [report] table's parent_hour\n" +
			"(7 unless set; mw config set report.parent_hour 6) and at no other time, at most once a local\n" +
			"day, remembered apart from the factory report's. [report] name is who it is about. --force\n" +
			"posts at any hour; --dry-run prints the note. mw-parent-note.timer runs it every hour.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if class != "factory" && class != "parent" {
				return fmt.Errorf("--class %q: it is factory or parent", class)
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			here, err := config.Host()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			files := mwVault(dir, here)
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			governorKey, err := config.PosternGovernorKey()
			if err != nil {
				return err
			}
			floatSats, err := config.PosternFloatSats()
			if err != nil {
				return err
			}
			channel, err := config.PosternChannel()
			if err != nil {
				return err
			}
			backend, err := posternBackend(keys)
			if err != nil {
				return err
			}
			threads, err := posternThreads()
			if err != nil {
				return err
			}
			share, shareMin, shareMax, err := investShareSettings()
			if err != nil {
				return err
			}
			gateway := mwGateway(dir, here)
			review, err := damperSurvey(gateway)
			if err != nil {
				return err
			}
			post := application.PosternSend{
				Threads:     threads,
				Postern:     backend,
				Chain:       newChain(backend, keys, cmd.OutOrStdout()),
				Cipher:      posternCipher(keys),
				Keys:        keys,
				GovernorKey: governorKey,
				FloatSats:   int64(floatSats),
				Channel:     channel,
				Now:         posternClock,
			}
			if class == "parent" {
				name, hour, err := config.ReportParent()
				if err != nil {
					return err
				}
				_, err = application.ParentNoteSend{
					Report: application.FactoryReport{
						Tracker: gateway,
						Files:   files,
						Seat:    BuilderSeat,
						Review:  review,
					},
					Name:   name,
					Hour:   hour,
					Sent:   reportlog.New(filepath.Join(home, reportlog.DefaultDir)),
					Post:   post,
					Force:  force,
					DryRun: dryRun,
					Out:    cmd.OutOrStdout(),
				}.Run(cmd.Context())
				return err
			}
			_, err = application.FactoryReportSend{
				Report: application.FactoryReport{
					Tracker:     gateway,
					Files:       files,
					Benchmarks:  vault.Benchmarks{Vault: files},
					Rules:       files,
					Seat:        BuilderSeat,
					Notes:       gateway,
					Mailbox:     gateway,
					Snapshots:   files,
					Review:      review,
					InvestShare: share, InvestShareMin: shareMin, InvestShareMax: shareMax,
				},
				Home:   files,
				Host:   here,
				Sent:   reportlog.New(filepath.Join(home, reportlog.DefaultDir)),
				Post:   post,
				Force:  force,
				DryRun: dryRun,
				Out:    cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&class, "class", "factory", "factory: the week's summary to the Factory channel, from the home; parent: a member's plain daily note to the parent")
	cmd.Flags().BoolVar(&force, "force", false, "send again though today's report has gone")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the summary; send and remember nothing")
	return cmd
}
