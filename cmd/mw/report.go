package main

import (
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newReportCmd builds `mw report`: the factory's quality, speed and fuel over a window.
func newReportCmd() *cobra.Command {
	var since, rigName, host string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "report [--since 7d|<day>|<story>|<commit>] [--rig <rig>] [--host <host>] [--json]",
		Short: "How the factory is doing: quality first, then speed against par and fuel per landing, each against the week before",
		Long: "report sums the factory's quality over a window, from the builder ledger, the stories and\n" +
			"the Tester's FINDINGS: per rig, then per model, then per host, what landed, what was refused\n" +
			"and under which reason, the refusal rate, the attempts a landing took, the refusals' causes\n" +
			"(mw cause), the [bug] stories filed and the Tester's findings, the last two per landing.\n" +
			"The window is --since (7d by default; a number of days, a day such as 2026-10-09, or a UTC\n" +
			"time) to now; the window of the same length before it is computed too and is in --json.\n" +
			"After QUALITY come SPEED (the median and p90 of actual over par, the wall time from dispatch\n" +
			"to landing, and each host's gate usual against latest; 'no par yet' where no close-out\n" +
			"measured one) and FUEL (tokens per landing from the ledger and $ per landing from the result\n" +
			"files, every attempt of a story summed). A number carries an arrow against the window before,\n" +
			"level inside 5%, and says better or worse by the measure. --rig and --host narrow every\n" +
			"block. A ledger row whose story is gone counts under rig unknown. The report ends with CHEAP\n" +
			"WINS: at most three one-line suggestions by fixed rules (causes unset, a dominant refusal\n" +
			"reason, attempts used up, a refusal rate up, a flagged par, a slow gate), or 'none seen';\n" +
			"--json carries every rule that held. Between FUEL and CHEAP WINS is FACTORY CHANGES: each\n" +
			"millwright landing of the last 30 days with the 'Expect: <metric> <up|down>' line of its\n" +
			"acceptance, that metric for the whole factory in the 7 days before and the 7 days after the\n" +
			"landing day ('too soon' until the 7 days are over), and WORSE when it went the wrong way by\n" +
			"more than a 10% band. It reads and writes nothing, and prints in 60 columns.\n" +
			"The INVESTMENTS part follows FACTORY CHANGES: of the factory's investments (mw invest) the wins\n" +
			"the window recorded, named with their measured gain, the losses with their lesson and the\n" +
			"better approach, and how many are still due for review.\n" +
			"--since a story id or a commit of the millwright rig is one landing instead: the same\n" +
			"quality, speed and fuel for the whole factory (or --rig, --host) in the 7 days before its\n" +
			"day and in the days after it, up to 7 (as many as there have been), side by side with the\n" +
			"change; the landing's own day is in neither. A story's day is its first landing in the\n" +
			"ledger; a commit's is that of the story its subject names, or else the day it was made.\n" +
			"mw report send posts a phone-sized summary of it to the Factory channel, once a day.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			here, err := config.Host()
			if err != nil {
				return err
			}
			from, around, err := application.ReadReportSince(since, time.Now())
			if err != nil {
				return err
			}
			// A commit is read in this host's checkout of the factory's rig; with
			// none, the report says so when it is asked for one.
			var factoryDir string
			if around != "" {
				if rigs, err := config.Rigs(); err == nil {
					factoryDir = rigs[application.FactoryRig]
				}
			}
			files := mwVault(dir, here)
			_, err = application.FactoryReport{
				Tracker:     mwGateway(dir, here),
				Files:       files,
				Benchmarks:  vault.Benchmarks{Vault: files},
				Seat:        BuilderSeat,
				Since:       from,
				Around:      around,
				Commits:     rig.New(),
				FactoryDir:  factoryDir,
				Investments: files,
				Rig:         rigName,
				Host:        host,
				JSON:        asJSON,
				Out:         cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.AddCommand(newReportSendCmd())
	cmd.Flags().StringVar(&since, "since", "", "where the window starts: 7d, a day (2026-10-09) or a UTC time; 7d by default. A story id or a millwright commit sets the days before that landing beside the days after")
	cmd.Flags().StringVar(&rigName, "rig", "", "narrow every block to this rig")
	cmd.Flags().StringVar(&host, "host", "", "narrow every block to this host")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the whole structure as JSON")
	return cmd
}
