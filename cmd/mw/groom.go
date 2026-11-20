package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newGroomCmd builds `mw groom <rig> [--json]`: the Mayor's weekly grooming
// list of a rig, at zero tokens (mw-6ww.119.10.7).
func newGroomCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "groom <rig> [--json]",
		Short: "List what a rig's weekly grooming looks at: FEATURES against target, the fix budget, finished epics, stuck holds, ice candidates, paced lists, fold groups",
		Long: "groom reads the tracker once and prints, for the rig, the week's lists the Mayor grooms from:\n\n" +
			"  - FEATURES: the last 7 days' landings by lane against the rig's feature_share, and its feature_floor\n" +
			"    against the features ready now\n" +
			"  - the fix budget: (1 - feature_share) of the week's non-express landings, against the fixes landed,\n" +
			"    running and ready\n" +
			"  - every open epic whose every child is closed ('done, still open') and every epic whose only\n" +
			"    unfinished children are demos\n" +
			"  - every held bead with live beads behind it and no '" + application.HeldCommentPrefix + "' reason (as mw stuck)\n" +
			"  - features whose root blocker is a bug, with the bug\n" +
			"  - auto-found beads (found-at-close, a Tester's, [taste]) held 21 days or more: ice candidates, never\n" +
			"    one labelled his-report or his-priority, nor one a feature waits on\n" +
			"  - paced lists (plans/*/release-order*.txt) of the rig with nothing open or running\n" +
			"  - fold groups of 3 or more, as mw fold, with found-at-close beads grouped by the directory of their\n" +
			"    files key (dir:<directory>) and [taste] ones by screen\n\n" +
			"--json prints the same as eight keys: features, fix_budget, epics, held_no_reason, blocked_features,\n" +
			"ice_candidates, paced_lists, fold_groups. It runs no model and writes nothing.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			defer keepBeadsConnection(dir, cmd.ErrOrStderr())()
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.Groom{
				Source: gateway, Rig: args[0], Rules: groomRules{vault.New(dir)}, Lists: vaultPacedLists{dir: dir},
				JSON: asJSON, Out: cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return cmd
}

// groomRules reads a rig's file as a landing does: a key this mw does not know
// is no reason to lose the share and the floor.
type groomRules struct{ files *vault.Vault }

func (r groomRules) EpicRequirements(ctx context.Context, rig string) (domain.EpicRequirements, error) {
	required, _, err := r.files.LandingRequirements(ctx, rig)
	return required, err
}

// vaultPacedLists finds the paced lists the vault keeps: the files
// plans/<plan>/release-order*.txt, one story id a line.
type vaultPacedLists struct{ dir string }

func (v vaultPacedLists) PacedLists(context.Context) ([]application.PacedList, error) {
	names, err := filepath.Glob(filepath.Join(v.dir, "plans", "*", "release-order*.txt"))
	if err != nil {
		return nil, err
	}
	var lists []application.PacedList
	for _, name := range names {
		text, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		list := application.PacedList{}
		list.Name, _ = filepath.Rel(v.dir, name)
		for _, line := range strings.Split(string(text), "\n") {
			if line, _, _ = strings.Cut(line, "#"); strings.TrimSpace(line) != "" {
				list.IDs = append(list.IDs, strings.TrimSpace(line))
			}
		}
		lists = append(lists, list)
	}
	return lists, nil
}
