package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/configfile"

	"github.com/spf13/cobra"
)

// newConfigCmd builds `mw config`: reading and changing one key of
// ~/.config/mw/config.toml.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read or change one key of ~/.config/mw/config.toml",
	}
	cmd.AddCommand(newConfigSetCmd(), newConfigGetCmd())
	return cmd
}

func newConfigSetCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "set [<table>.]<key> <value>",
		Short: "Change one key of ~/.config/mw/config.toml, after a dated backup",
		Long: "set changes exactly one key of ~/.config/mw/config.toml and nothing else: every other byte\n" +
			"of the file (comments, order, other tables) is kept. A missing key is added at the end of its\n" +
			"table, and a missing table is appended. A comment after the old value stays where it was.\n\n" +
			"Before it writes anything it copies the file to config.toml.bak-<UTC YYYY-MM-DDTHHMMSSZ>\n" +
			"beside it, and it prints the key's old value (or unset), the new value, the backup, and the\n" +
			"way back as one whole command (`cp <backup> <config>`) that puts the file as it was.\n\n" +
			"  mw config set after_landing.millwright 'make build'\n" +
			"  mw config set cloud.caps.desktop 3\n" +
			"  mw config set cap 2\n" +
			"  mw config set tester.rigs '[\"lampas\"]'\n\n" +
			"A value that is already TOML (a quoted string, an array, an inline table, a number, true or\n" +
			"false, a date) is written as typed; any other value is written as a quoted string. To store\n" +
			"the string 5 and not the number, pass '\"5\"'. The table may be dotted (cloud.caps); the key\n" +
			"after the last dot, and every part, must be a bare key. A key of the root table that mw reads\n" +
			"(cap, builder_nice, builder_jobs, host and the rest) is named without a table; any other\n" +
			"top-level key is refused, naming the known ones.\n\n" +
			"It refuses, with a non-zero status, writing nothing and making no backup: a key with no\n" +
			"table that is not a known top-level key, a value that would break the file, a file that does not parse as TOML before or after\n" +
			"the change, and a value the key already holds (unchanged). --dry-run prints the same and\n" +
			"writes nothing.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.FilePath()
			if err != nil {
				return err
			}
			return application.ConfigSet{File: configfile.New(path), Out: cmd.OutOrStdout()}.
				Run(cmd.Context(), args[0], args[1], dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would change, the backup and the way back, and write nothing")
	return cmd
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [<table>.]<key>",
		Short: "Print one key of ~/.config/mw/config.toml",
		Long: "get prints the value of one key of ~/.config/mw/config.toml, a string without its quotes.\n" +
			"It fails, saying the key is unset, when the file has no such key.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.FilePath()
			if err != nil {
				return err
			}
			return application.ConfigGet{File: configfile.New(path), Out: cmd.OutOrStdout()}.
				Run(cmd.Context(), args[0])
		},
	}
}
