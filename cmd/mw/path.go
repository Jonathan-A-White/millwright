package main

import (
	"strconv"

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
	var from string
	cmd := &cobra.Command{
		Use:   "set <story-id> [--from <sibling-id>] [key=value ...]",
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
			"as size=S after --from. An unknown key, a sibling with no rig, and a story that is in\n" +
			"progress or closed are refused, and nothing is written: a path is adjusted only before a story starts.\n" +
			"So is a files key naming a directory of more than " + strconv.Itoa(application.MostFilesInADirectory) + " files in the rig's checkout here, a glob matching more than that, or\n" +
			"a whole directory (src/, src/**) or a glob ending in a wildcard (tests/e2e/*.ts) where the rig is not\n" +
			"checked out to count it.\n\n" +
			"A story with files and a size is told 'test callers: N for size S (allows M)': the rig's test\n" +
			"files (features/, tests/, *_test.go) naming its files by base name, against XS 5, S 10, M 20,\n" +
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
			"open or held story carrying it is made a need of that story, and one line says so.",
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
			_, err = application.PathSet{Tracker: gateway, Rigs: rigs, HeavyNet: heavy, RigFiles: rig.Files{}, Rules: rigRulesIfKnown(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}.
				Run(cmd.Context(), args[0], from, args[1:])
			return err
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "the sibling story to copy the seven path keys from")
	return cmd
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
