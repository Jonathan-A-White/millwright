package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/spf13/cobra"
)

// newPathCmd builds `mw path`: setting and reading a story's path keys.
func newPathCmd() *cobra.Command {
	path := &cobra.Command{
		Use:   "path",
		Short: "Set or show a story's path",
	}
	path.AddCommand(newPathSetCmd(), newPathShowCmd())
	return path
}

func newPathSetCmd() *cobra.Command {
	var from, after, hold, overClaim string
	cmd := &cobra.Command{
		Use:   "set <story-id> [--from <sibling-id>] [--after <ids|none>] [--hold \"<why>\"] [key=value ...]",
		Short: "Set a story's path keys in one write",
		Long: "set gives a story its path keys (rig, branch, harness, model, effort, formula, host,\n" +
			"files, size) in one write, in place of one bd update --set-metadata call for each.\n" +
			"size=XS|S|M|L|XL is how big the Mayor judges the story: a key of the path, never a line in\n" +
			"the body. files is the comma-separated repo paths or globs the story expects to change:\n" +
			"dispatch does not start it while a running story on its rig shares one. bump=minor has the landing raise\n" +
			"the rig's minor version, patch reset to 0, in place of its patch: the epic's last story\n" +
			"says it here, and a Builder never edits the version itself.\n\n" +
			"  mw path set B --from A               copy the seven keys from the story A\n" +
			"  mw path set B --from A model=opus    copy them, with model overridden\n" +
			"  mw path set B rig=lampas             set only the rig\n" +
			"  mw path set B size=M                 set only the size (XS, S, M, L or XL)\n" +
			"  mw path set B files=src/Reader.tsx,src/data/Rows.ts\n\n" +
			"Only the seven keys are copied from the sibling, never its files nor its other\n" +
			"metadata. --from does not copy size (nor bump): a size is the story's own, so give it\n" +
			"as size=S after --from. On a rig whose file says files_expected = true, --from with no files= prints\n" +
			"'files not copied from A: pass files=' on stderr. An unknown key, a sibling with no rig, and a story that is in\n" +
			"progress or closed are refused, and nothing is written: a path is adjusted only before a story starts.\n" +
			"So is a files key naming a directory of more than " + strconv.Itoa(application.MostFilesInADirectory) + " files in the rig's checkout here, a glob matching more than that, or\n" +
			"a whole directory (src/, src/**) or a glob ending in a wildcard (tests/e2e/*.ts) where the rig is not\n" +
			"checked out to count it.\n\n" +
			"--over-claim \"<why>\" adjusts the path of a story a Builder has claimed all the same, and writes the why on it as an\n" +
			"OVER-CLAIM: comment; a closed story is refused whatever it says.\n\n" +
			"A story with files and a size is told 'test callers: N for size S (allows M)': the rig's test\n" +
			"files (features/, tests/, *_test.go) calling what its files declare, against XS 5, S 10, M 20,\n" +
			"L 35, XL 50. Over that, a WARN on stderr says to split the story or size it up; nothing is refused.\n" +
			"callers_ok=\"<why>\" is the story's own reason the callers are fine (say, the change is additive): the count\n" +
			"line still prints for it, the WARN does not, and --from does not copy it.\n\n" +
			"Writing a size L or XL on a story whose acceptance criteria ask for more than one of a new\n" +
			"setting, a new component and a removal says so on stderr, naming the criteria (mw-gq6.852).\n" +
			"The size is also the story's turn cap: XS 60, S 90, M 120, L 160, XL 200, unless max_turns is set.\n\n" +
			"After the write, a story that mw dispatch would pass over is named, before it is un-held: no files key on a\n" +
			"rig whose file says files_expected = true, a bug with no Sweep: line, a Tester story with no Landed story:\n" +
			"line. A warning only; nothing is refused for it.\n\n" +
			"A path that pins a story to a home host (laptop or desktop) on a rig named under [heavy_net] gets a\n" +
			"WARN on stderr naming host=auto: that rig installs dependencies, so its stories are not started while\n" +
			"the host's network is metered, and a pin keeps any other host from taking them. It is set all the same.\n\n" +
			"A story in an epic whose rig asks for a last story (epic_last_story_labels) with exactly one\n" +
			"open or held story carrying it is made a need of that story, and one line says so.\n\n" +
			"--after <id,id|none> names the stories this one waits on: each is added as a blocker before the\n" +
			"path is written, in the one command (--after none is a story that waits on nothing). On a rig\n" +
			"whose file in the vault says release_on_path = true (off by default), a held story given --after\n" +
			"is then released, last, as mw release <id> releases it, with the comment 'released on path set'.\n" +
			"It stays held, and one 'held: <id>: <reason>' line says why, when it has a hold label (grilling,\n" +
			"hitl, demo, icebox), a HELD: comment, a hold on the Governor's word, no files key on a rig that\n" +
			"expects one, no rig or branch, or a gate mw dispatch would pass it over at (a bug with no Sweep:\n" +
			"line, a Tester story with no Landed story: line), or, for a story labelled model-story, the pace of\n" +
			"mw release-paced: usage over 60% of the week, missing or stale, or 3 model stories of its epic open\n" +
			"or running. With no --after nothing is released, and on a release_on_path rig one 'held:' line on stderr says so; on a rig\n" +
			"without the key --after only adds the dependencies.\n" +
			"--hold \"<why>\" keeps a held story held and writes HELD: <why> on it, so a hold always has a\n" +
			"written reason; it releases nothing, with or without --after.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			// A rig table that cannot be read leaves no checkout to count a
			// directory in: a whole directory is refused, a plain name taken.
			rigs, _ := config.Rigs()
			// Likewise a [heavy_net] table that cannot be read warns of nothing.
			heavy, _ := config.HeavyNet()
			if cmd.Flags().Changed("over-claim") && strings.TrimSpace(overClaim) == "" {
				return fmt.Errorf("--over-claim needs a reason, as --over-claim \"the Builder is on the wrong model\"")
			}
			var afterIDs []string
			if cmd.Flags().Changed("after") {
				if afterIDs, err = parseAfter(after); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("hold") && strings.TrimSpace(hold) == "" {
				return fmt.Errorf("--hold needs a reason, as --hold \"waits for the demo\"")
			}
			// The usage readings pace a story labelled model-story, as mw release-paced does.
			var readings application.UsageReadings
			if dir, vaultErr := config.Vault(); vaultErr == nil {
				if host, hostErr := config.Host(); hostErr == nil {
					readings = application.UsageReadings{Notes: mwGateway(dir, host)}
				}
			}
			retired, err := config.RetiredHosts()
			if err != nil {
				return err
			}
			var cloudBook application.CloudBook
			var cloudPlan application.CloudPlan
			if dir, vaultErr := config.Vault(); vaultErr == nil {
				if host, hostErr := config.Host(); hostErr == nil {
					cloudBook, cloudPlan = cloudStatus(dir, host)
				}
			}
			_, err = application.PathSet{Tracker: gateway, Retired: retired, CloudBook: cloudBook, CloudPlan: cloudPlan, Readings: readings, Rigs: rigs, HeavyNet: heavy, RigFiles: rig.Files{}, Rules: rigRulesIfKnown(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(),
				After: afterIDs, Hold: hold, OverClaim: overClaim}.
				Run(cmd.Context(), args[0], from, args[1:])
			return err
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "the sibling story to copy the seven path keys from")
	cmd.Flags().StringVar(&after, "after", "", "the stories this one waits on, comma-separated, or none; on a rig with release_on_path = true it also releases the held story")
	cmd.Flags().StringVar(&overClaim, "over-claim", "", "why the path of a story a Builder has claimed (in progress, or a live claim) is adjusted all the same; written as an OVER-CLAIM: comment")
	cmd.Flags().StringVar(&hold, "hold", "", "why the story stays held: written as a HELD: comment; nothing is released")
	return cmd
}

// parseAfter reads --after: story ids separated by commas, or the one word none
// for a story that waits on nothing, which is an empty list that is given.
func parseAfter(after string) ([]string, error) {
	after = strings.TrimSpace(after)
	if after == "" {
		return nil, fmt.Errorf("--after needs story ids separated by commas, or none")
	}
	if strings.EqualFold(after, "none") {
		return []string{}, nil
	}
	ids := strings.Split(after, ",")
	for _, id := range ids {
		if strings.EqualFold(strings.TrimSpace(id), "none") {
			return nil, fmt.Errorf("--after none stands alone: it is a story that waits on nothing, so it cannot be listed with other ids")
		}
	}
	return ids, nil
}

func newPathShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <story-id>",
		Short: "Print a story's ten path keys",
		Long: "show prints the eleven path keys of a story as it is worked by them. On a rig whose file\n" +
			"in the vault says files_expected = true, a story with no files key also gets one line\n" +
			"saying so: a warning, and the keys are printed all the same. So does a bug with no Sweep: line and a\n" +
			"Tester story with no Landed story: line: each is a story mw dispatch passes over.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.PathShow{Tracker: gateway, Rules: rigRulesIfKnown(), Out: cmd.OutOrStdout()}.Run(cmd.Context(), args[0])
			return err
		},
	}
}
