package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newHomeCmd builds `mw home`: which host is the factory's home, and is this
// one. It only reads the vault's home file.
func newHomeCmd() *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "home",
		Short: "Say which host is home, and whether this host is",
		Long: "home reads the vault's `home` file, a tracked file of one line (the home host's name,\n" +
			"desktop or laptop, then the UTC time and actor of the last change), and prints the home,\n" +
			"this host and whether this host is home: yes or no. Home is the one host that holds the\n" +
			"beads server, the Mayor and the live Postern backend; the other host is the boost. With no\n" +
			"home file, or one that is not understood, it says the home is unknown and leaves with 0.\n\n" +
			"--check leaves with 0 when this host is home, 1 when it is not and 2 when it cannot tell (no\n" +
			"file, or one not understood): what to do then is for the caller to decide. It prints nothing\n" +
			"on stdout, except when this host is not home: then it prints the home's name alone, one line\n" +
			"(laptop), for a caller that must know where to go, and the explanation goes to stderr. Both\n" +
			"only read: `mw home move <host>` is what moves the home.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			home := application.Home{Files: mwVault(dir, host), Host: host}
			run := home.Run
			if check {
				run = home.Check
			}
			report, err := run(cmd.Context())
			// A report comes with an error only from --check on a host that is
			// not home: the home's name goes to stdout, the error to stderr.
			if _, werr := cmd.OutOrStdout().Write([]byte(report.String())); err == nil {
				err = werr
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "leave with 0 when this host is home, 1 (printing the home's name) when it is not, 2 when it cannot tell")
	cmd.AddCommand(newHomeMoveCmd())
	return cmd
}
