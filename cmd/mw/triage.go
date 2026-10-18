package main

import (
	"fmt"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newTriageCmd builds `mw triage`: the nightly triage pass's input, one line
// per live work bead, and what moved between two of them.
func newTriageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "triage",
		Short: "The triage pass's input: dump the live work beads, diff two dumps",
	}
	cmd.AddCommand(newTriageDumpCmd(), newTriageDiffCmd())
	return cmd
}

func newTriageDumpCmd() *cobra.Command {
	var out string
	var sweepOrphans, apply bool
	cmd := &cobra.Command{
		Use:   "dump",
		Short: "Write one JSON line per live work bead (no mail, no formula steps)",
		Long: "dump reads every bead and writes one line of JSON per work bead that is not closed — no mail,\n" +
			"no event, no mw-mol-* formula bead — to --out: id, type, status, priority, labels, rig, size,\n" +
			"parent, created, updated, days since update, whether it is ready and for how many days, what it\n" +
			"waits on and what waits on it, the first 500 characters of its description, and his_priority\n" +
			"when the bead is labelled his-priority (the Mayor sets it whenever he records a priority from\n" +
			"the Governor's word) or the description quotes the Governor setting a priority. It prints how\n" +
			"many lines it wrote.\n\n" +
			"--sweep-orphans lists, instead, the formula beads of closed stories, and changes nothing;\n" +
			"with --apply it closes them, each with a comment saying why.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if apply && !sweepOrphans {
				return fmt.Errorf("--apply goes with --sweep-orphans")
			}
			if !sweepOrphans && out == "" {
				return fmt.Errorf("where should the dump go? give --out <file>")
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			gateway := mwGateway(dir, host)
			if sweepOrphans {
				_, err := application.TriageOrphans{Source: gateway, Closer: gateway, Apply: apply, Out: cmd.OutOrStdout()}.Run(cmd.Context())
				return err
			}
			file, err := os.Create(out)
			if err != nil {
				return err
			}
			lines, err := application.TriageDump{Source: gateway}.Run(cmd.Context(), file)
			if cerr := file.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%d beads written to %s\n", len(lines), out)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "the file to write the dump to")
	cmd.Flags().BoolVar(&sweepOrphans, "sweep-orphans", false, "list the formula beads of closed stories instead of dumping")
	cmd.Flags().BoolVar(&apply, "apply", false, "with --sweep-orphans, close them")
	return cmd
}

func newTriageDiffCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "diff <old> <new>",
		Short: "List what moved between two dumps: new, closed, moved, newly ready, newly stale",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			read := func(path string) ([]application.TriageLine, error) {
				file, err := os.Open(path)
				if err != nil {
					return nil, err
				}
				defer file.Close()
				return application.ReadTriageLines(file)
			}
			old, err := read(args[0])
			if err != nil {
				return err
			}
			fresh, err := read(args[1])
			if err != nil {
				return err
			}
			diff := application.DiffTriage(old, fresh)
			if asJSON {
				return diff.WriteJSON(cmd.OutOrStdout())
			}
			fmt.Fprint(cmd.OutOrStdout(), diff.String())
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the diff as JSON")
	return cmd
}
