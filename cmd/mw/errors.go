package main

import (
	"io/fs"
	"strings"

	"github.com/spf13/cobra"

	millwright "github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// newErrorsCmd builds `mw errors`: the Mayor keeps the common-errors register
// of the Builder's seat (mw-gq6.420).
func newErrorsCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "errors",
		Short: "Keep the common-errors register: a class seen in one rig is checked in every rig of its kinds",
		Long: "The register is seats/builder/common-errors/ in the vault: one file a class (symptom, cause,\n" +
			"the sweep over the whole class, the kinds of rig it can occur in, the shared-component verdict\n" +
			"and every rig checked, dated) and kinds.toml, the kinds of each rig. A Builder reads it by need,\n" +
			"never at boot. These verbs are how the Mayor keeps it: each writes files and prints each file it\n" +
			"wrote, spends no fuel, runs no git command and files nothing. A class found twice asks whether a\n" +
			"shared component would carry one fix to every rig; the Mayor weighs it and records the verdict.\n" +
			"mw status lists, under ERROR CLASSES TO CHECK, the classes not yet checked in every rig of their kinds.",
		Args: cobra.NoArgs,
	}
	root.AddCommand(newErrorsAddCmd(), newErrorsListCmd(), newErrorsShowCmd(), newErrorsCheckedCmd(),
		newErrorsSharedCmd(), newErrorsKindsCmd(), newErrorsSeedCmd())
	return root
}

// errorsFor reads the vault the register is kept in.
func errorsFor(cmd *cobra.Command) (application.Errors, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.Errors{}, err
	}
	seed, err := fs.Sub(millwright.Template(), application.ErrorRegisterTemplateDir)
	if err != nil {
		return application.Errors{}, err
	}
	return application.Errors{Files: vault.New(dir), Seat: BuilderSeat, Seed: seed, Out: cmd.OutOrStdout()}, nil
}

func newErrorsAddCmd() *cobra.Command {
	var add application.ErrorAdd
	var kinds string
	cmd := &cobra.Command{
		Use:   "add <slug> --symptom <line> --kinds <k1,k2> [--cause <line>] [--sweep <line>] [--shared <verdict>] [--found <rig>:<bead>]",
		Short: "Write a new error class",
		Long: "add writes <slug>.md, since today (UTC). --found records the first find as a check of that rig.\n" +
			"It refuses a slug already in the register (record a new find with checked), a slug or kind that\n" +
			"is not lower-case letters, digits and hyphens, no kinds, and a value with a newline.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			register, err := errorsFor(cmd)
			if err != nil {
				return err
			}
			add.Slug = args[0]
			add.Kinds = strings.FieldsFunc(kinds, func(r rune) bool { return r == ',' || r == ' ' })
			return register.Add(cmd.Context(), add)
		},
	}
	cmd.Flags().StringVar(&add.Symptom, "symptom", "", "what is seen, in one line")
	cmd.Flags().StringVar(&kinds, "kinds", "", "the kinds of rig it can occur in, comma-separated (pwa, go-cli, grind...)")
	cmd.Flags().StringVar(&add.Cause, "cause", "", "why it happens, in one line")
	cmd.Flags().StringVar(&add.Sweep, "sweep", "", "the check over the whole class, in one line")
	cmd.Flags().StringVar(&add.Shared, "shared", "", "the shared-component verdict: none, candidate or 'exists <where>'")
	cmd.Flags().StringVar(&add.Found, "found", "", "the first find, <rig>:<bead>")
	_ = cmd.MarkFlagRequired("symptom")
	_ = cmd.MarkFlagRequired("kinds")
	return cmd
}

func newErrorsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the classes, one a line, with the shared-component question for one found twice",
		Long: "list prints, by slug: the kinds, how often it was found, the rigs of its kinds not yet checked,\n" +
			"the verdict, the symptom, and the shared-component question for a class found twice with no\n" +
			"verdict. A file that is not a whole class is named last.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			register, err := errorsFor(cmd)
			if err != nil {
				return err
			}
			return register.List(cmd.Context())
		},
	}
}

func newErrorsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <slug>",
		Short: "Print one class, the rigs not yet checked and its question",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			register, err := errorsFor(cmd)
			if err != nil {
				return err
			}
			return register.Show(cmd.Context(), args[0])
		},
	}
}

func newErrorsCheckedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "checked <slug> <rig> <ok|found:<bead>>",
		Short: "Record that a rig was checked for a class, dated today",
		Long: "checked appends a dated check line. When a find makes the class seen twice, it says so and asks\n" +
			"the shared-component question; it files nothing.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			register, err := errorsFor(cmd)
			if err != nil {
				return err
			}
			return register.Checked(cmd.Context(), args[0], args[1], args[2])
		},
	}
}

func newErrorsSharedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shared <slug> none|candidate|exists <where>",
		Short: "Record the Mayor's shared-component verdict on a class",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			register, err := errorsFor(cmd)
			if err != nil {
				return err
			}
			return register.Shared(cmd.Context(), args[0], strings.Join(args[1:], " "))
		},
	}
}

func newErrorsKindsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kinds [<rig> [<kind>...]]",
		Short: "Set the kinds of a rig, or print them",
		Long: "kinds <rig> <kind>... replaces that rig's kinds in kinds.toml and keeps every other rig's.\n" +
			"With only a rig it prints that rig's kinds; with nothing, every rig's.",
		RunE: func(cmd *cobra.Command, args []string) error {
			register, err := errorsFor(cmd)
			if err != nil {
				return err
			}
			rig := ""
			if len(args) > 0 {
				rig = args[0]
				args = args[1:]
			}
			return register.Kinds(cmd.Context(), rig, args)
		},
	}
}

func newErrorsSeedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "Write the seed classes this mw holds into a register that lacks them",
		Long: "seed writes each class of the template's register (the first ones, from the Lampas bugs of\n" +
			"2026-10-14) that the register does not hold yet, and never overwrites one it does.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			register, err := errorsFor(cmd)
			if err != nil {
				return err
			}
			return register.SeedRegister(cmd.Context())
		},
	}
}
