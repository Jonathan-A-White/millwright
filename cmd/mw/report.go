package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/commandlog"
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
			"(mw cause), the [bug] stories filed and the Tester's findings, those two per landing, and\n" +
			"the escaped bugs: [bug] stories of a landing's rig filed on its day or in the 14 days after,\n" +
			"naming its story id, counted under the rig, host and model of that landing, as 'escaped bugs N\n" +
			"(Tester M)': the Tester's findings (a description carrying 'Tester finding') are the factory\n" +
			"catching a bug, so the quality bound and MODELS leave them out.\n" +
			"A bug whose first 700 characters say closing comment, sweep, LEFT BY, left undone, Seen but NOT\n" +
			"fixed or TO FILE is a sweep find, a Builder's find that names the landing it was found in and not\n" +
			"one that landing let through: it is counted on its own line, 'sweep finds S', and not in escaped\n" +
			"bugs, a change's escapes, MODELS or the quality bound (escaped bugs + sweep finds is the count\n" +
			"before they were split). A Tester finding, and the bug mw next files ('mw next landed ...') for\n" +
			"an unmet criterion, stay escapes. Known limit: a sweep find that also names its cause counts as\n" +
			"a sweep find (it may name two landings).\n" +
			"QUALITY ends with INVESTMENTS: the landings labelled investment over all the landings, the\n" +
			"window before, the arrow and the target ([factory] invest_share, 0.33 unless set; mw config\n" +
			"set factory.invest_share 0.2), and a line when the share is under half or over double it.\n" +
			"Under it, a bound line when the week calls for one: up to invest_share_max (0.33) allowed while\n" +
			"quality is falling, down to invest_share_min (0.10) asked while an app starves.\n" +
			"The window is --since (7d by default; a number of days, a day such as 2026-10-09, or a UTC\n" +
			"time) to now; the window of the same length before it is computed too and is in --json.\n" +
			"After QUALITY come SPEED (the median and p90 of actual over par, the wall time from dispatch\n" +
			"to landing, and each host's gate usual against latest; 'no par yet' where no close-out\n" +
			"measured one) and FUEL (tokens per landing from the ledger and $ per landing from the result\n" +
			"files, every attempt of a story summed; the stories' own sessions on every host, and no seat's).\n" +
			"For the whole factory (not under --rig or --host) FUEL ends with SEATS: on the home, a line for\n" +
			"each seat that ran a session in the window (the Mayor, the Deputy and the rest), its dollars and\n" +
			"dollars per landing, then 'all seats $/landing': the stories' figure plus the seats' dollars over\n" +
			"the same landings. A seat's dollars are the totalCostUSD of the last cost-state line of each main\n" +
			"session Claude Code recorded in the vault's directory whose transcript was last written in the\n" +
			"window; the seat is the first word of the session's name (a name that is no folder under seats/\n" +
			"is 'other'); a subagent adds nothing; a session with no cost-state line (one still running) adds\n" +
			"no dollars and is counted as 'N sessions without a cost figure'. Off the home SEATS is one line,\n" +
			"'seats: counted on the home'. mw report send keeps seat_usd and usd_per_landing_all in the day's\n" +
			"row. After FUEL, MODELS sets opus and sonnet (and haiku and\n" +
			"fable once they had a session) side by side: landed, first-try share, refused, retried, tokens,\n" +
			"$ and escaped bugs per landed story, '-' where a model has nothing to measure, then by kind\n" +
			"(rig and bug or feature) what each landed and its first-try share; --json has every number\n" +
			"of every kind. A number carries an arrow against the window before, level inside 5%, and\n" +
			"says better or worse by the measure. --rig and --host narrow every\n" +
			"block. A ledger row whose story is gone counts under rig unknown. The report ends with CHEAP\n" +
			"WINS: at most three one-line suggestions by fixed rules (causes unset, a dominant refusal\n" +
			"reason, attempts used up, a refusal rate up, a flagged par, a slow gate, an investment share\n" +
			"far off its target), or 'none seen';\n" +
			"--json carries every rule that held. Between FUEL and CHEAP WINS is FACTORY CHANGES: each\n" +
			"millwright landing of the last 30 days with the 'Expect: <metric> <up|down>' line of its\n" +
			"acceptance, that metric for the whole factory in the 7 days before and the 7 days after the\n" +
			"landing day ('too soon' until the 7 days are over), and WORSE when it went the wrong way by\n" +
			"more than a 10% band, and the bugs that escaped it ('too soon' until its 14 days are over).\n" +
			"HIS TIME (for the whole factory, not with --rig, --host or --since a landing) is his review\n" +
			"queue and the demos waiting, as mw status DAMPERS counts them; 'Expect: review queue down' and\n" +
			"'Expect: demos waiting down' compare the mean of the daily snapshots in metrics/factory.jsonl\n" +
			"in the 14 days before the landing day with the mean in the 14 days after.\n" +
			"It reads and writes nothing, and prints in 60 columns.\n" +
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
			// SHAPE reads the checkout of the one rig asked for, if this host has it.
			var shapeDirs map[string]string
			if rigName != "" {
				shapeDirs, _ = config.Rigs()
			}
			var commandLog application.CommandLog
			if l, err := commandlog.Default(); err == nil {
				commandLog = l
			}
			share, shareMin, shareMax, err := investShareSettings()
			if err != nil {
				return err
			}
			files := mwVault(dir, here)
			gateway, stop := reportTracker(dir, here, cmd.ErrOrStderr())
			defer stop()
			review, err := damperSurvey(gateway, dir, here)
			if err != nil {
				return err
			}
			seatSessions, seats, err := seatFuelSources(dir)
			if err != nil {
				return err
			}
			_, err = application.FactoryReport{
				SeatSessions: seatSessions,
				Seats:        seats,
				Home:         files,
				Here:         here,
				Tracker:      gateway,
				History:      files,
				Review:       review,
				Files:        files,
				Benchmarks:   vault.Benchmarks{Vault: files},
				Rules:        files,
				Seat:         BuilderSeat,
				Since:        from,
				Around:       around,
				Commits:      rig.New(),
				FactoryDir:   factoryDir,
				Shape:        rig.Shape{},
				RigDirs:      shapeDirs,
				CommandLog:   commandLog,
				CommandNames: commandNames(cmd.Root()),
				Investments:  files,
				InvestShare:  share, InvestShareMin: shareMin, InvestShareMax: shareMax,
				Rig:  rigName,
				Host: host,
				JSON: asJSON,
				Out:  cmd.OutOrStdout(),
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

// seatFuelSources is where the SEATS block of FUEL reads from: the transcripts
// of the sessions run in the vault's directory, and the names of the seats, the
// folders under seats/ in the vault.
func seatFuelSources(vaultDir string) (application.SeatSessions, []string, error) {
	root, err := claude.DefaultProjectsRoot()
	if err != nil {
		return nil, nil, err
	}
	entries, err := os.ReadDir(filepath.Join(vaultDir, application.SeatsDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("listing the seats: %w", err)
	}
	var seats []string
	for _, entry := range entries {
		if entry.IsDir() {
			seats = append(seats, entry.Name())
		}
	}
	return claude.NewSeatTranscripts(root, vaultDir), seats, nil
}

// investShareSettings reads the [factory] table's investment target and the
// bounds on it, as fractions, for mw report and mw report send.
func investShareSettings() (share, shareMin, shareMax float64, err error) {
	if share, err = config.FactoryInvestShare(); err != nil {
		return 0, 0, 0, err
	}
	if shareMin, shareMax, err = config.FactoryInvestShareBounds(); err != nil {
		return 0, 0, 0, err
	}
	return share, shareMin, shareMax, nil
}

// reportTracker is the tracker mw report and mw report send read from: the
// status tracker, which keeps what the run reads of bd. The list of every bead
// is 120 MB on the live vault and 40 s or more of bd under load, and the report
// read it afresh for its stories since and for each read of the damper counts,
// over 5 minutes at load 15 with nothing printed (mw-6ww.175.24); now it is
// read once, over the kept connection where there is a server to reach.
func reportTracker(dir, host string, said io.Writer) (*beads.Gateway, func()) {
	return keptStatusTracker(dir, host, said)
}
