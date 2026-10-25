package main

import (
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"

	"github.com/spf13/cobra"
)

// newCauseCmd builds `mw cause <story-id> <class> "<why>" [--fix <bead>]`: the
// Mayor's naming of why a story failed, kept where it can be counted.
func newCauseCmd() *cobra.Command {
	var fix string
	cmd := &cobra.Command{
		Use:   `cause <story-id> <` + strings.Join(application.CauseClasses, "|") + `> "<why>" [--fix <bead>]`,
		Short: "Record why a story failed: its class of cause and the reason",
		Long: "cause records on a story why it failed: one class of cause (" + strings.Join(application.CauseClasses, ", ") + "),\n" +
			"the reason in a few words, and with --fix the bead that mends it. The class, reason and fix\n" +
			"are written as the story's metadata (cause, cause_why, cause_fix) so that causes can be\n" +
			"counted, and as one comment beginning \"CAUSE (<class>):\" so that a person reading the\n" +
			"story finds it. Naming a cause again replaces the class and adds another comment.\n\n" +
			"A class outside the four, an empty reason, and a story or fix that is not a bead are\n" +
			"refused, and nothing is written. Nothing else of the story is touched: not its status,\n" +
			"its claim nor its attempts.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.Cause{Tracker: gateway, Out: cmd.OutOrStdout()}.
				Run(cmd.Context(), args[0], args[1], args[2], fix)
			return err
		},
	}
	cmd.AddCommand(newCauseListCmd())
	cmd.Flags().StringVar(&fix, "fix", "", "the bead that mends the cause")
	return cmd
}

// newCauseListCmd builds `mw cause list [--since <day>] [--rig <rig>]`: which
// story got which class of cause, one line each.
func newCauseListCmd() *cobra.Command {
	var since, rigName string
	cmd := &cobra.Command{
		Use:   "list [--since 7d|<day>] [--rig <rig>]",
		Short: "List the stories a cause is recorded for: id, rig, class, why, fix bead",
		Long: "list prints one line for each story touched in the window that mw cause has named: its id,\n" +
			"its rig, the class of cause, the reason and the fix bead (- when none), joined by ' · ',\n" +
			"ordered by id. mw report and mw loop brief count causes per rig; this names the stories.\n" +
			"--since is a number of days (7d, the default), a day such as 2026-10-09, or a UTC time;\n" +
			"--rig narrows to one rig. It reads the stories and nothing else: no session, no tokens.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			from, err := application.ParseReportSince(since, time.Now())
			if err != nil {
				return err
			}
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.CauseList{Tracker: gateway, Since: from, Rig: rigName, Out: cmd.OutOrStdout()}.
				Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "where the window starts: 7d (the default), a day (2026-10-09) or a UTC time")
	cmd.Flags().StringVar(&rigName, "rig", "", "narrow the list to this rig")
	return cmd
}
