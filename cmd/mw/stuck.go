package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"

	"github.com/spf13/cobra"
)

// newStuckCmd builds `mw stuck`: the held beads that live beads wait behind.
func newStuckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stuck",
		Short: "List the held beads that live beads wait behind and that cannot move on their own",
		Long: "stuck lists every held root that live beads wait behind, transitively, over 'blocks' links: a chain\n" +
			"that can never move until the root does. A live bead is open or in progress, and not an epic. The\n" +
			"roots are:\n\n" +
			"  - an iced bead (label icebox); mw ice moves a bead behind its dependents when it ices it\n" +
			"  - a held (deferred) bead whose blockers are all closed and which has no reason to be held: none of\n" +
			"    the labels grilling, hitl (or hitl:<kind>), demo or icebox, and no comment opening '" + application.HeldCommentPrefix + "'\n" +
			"    (write 'HELD: <why>' on a bead held on purpose and it is left alone)\n" +
			"  - a claimed bead whose lease ran out over " + strconv.Itoa(int(application.StuckClaimAge/time.Hour)) + " hours ago with no heartbeat since: a claim with no\n" +
			"    session (a claim bd shows no lease for is not seen)\n\n" +
			"Each root is printed with the count and the ids of the live beads behind it; a root with nothing live\n" +
			"behind it is not listed. mw status shows the same as a 'stuck:' line in its DAMPERS section, and the\n" +
			"sleep pass reads this output. It reads and writes nothing, runs no model and always exits 0 when it\n" +
			"could read the beads.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			report, err := application.StuckSurvey{Graph: gateway, Comments: gateway, Claims: gateway}.Find(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), report.String())
			return nil
		},
	}
}
