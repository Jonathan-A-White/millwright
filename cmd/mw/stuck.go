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
	var inversions bool
	cmd := &cobra.Command{
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
			"could read the beads.\n\n" +
			"With --inversions it prints instead every priority inversion, one a line, with no cut (mw status cuts\n" +
			"its list at 8): a bead below the priority of the live beads that wait on it, as 'id P2→P1 waited on by\n" +
			"<id> (P1)', the raise being a proposal; a his-priority bead prints 'id P3 (his)' and no raise. With none\n" +
			"it prints a single line saying so. It reads and writes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			if inversions {
				list, err := application.InversionLister{Graph: gateway}.List(cmd.Context())
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), list)
				return nil
			}
			report, err := application.StuckSurvey{Graph: gateway, Comments: gateway, Claims: gateway}.Find(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), report.String())
			return nil
		},
	}
	cmd.Flags().BoolVar(&inversions, "inversions", false, "print every priority inversion, one a line, instead of the stuck roots")
	return cmd
}
