package main

import (
	"io"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/sops"

	"github.com/spf13/cobra"
)

// newSecretsCmd builds `mw secrets`: the factory's tokens, kept in the vault's
// secrets.enc.yaml, encrypted with sops to the age recipient the vault's
// .sops.yaml names and opened with the home's age key. No value is ever an
// argument, printed to a terminal, logged, or put in an event, a bead or a mail.
func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Keep the factory's tokens encrypted in the vault (sops + age)",
		Long: "secrets keeps the factory's tokens in the vault's " + application.SecretsFile + ", encrypted with sops\n" +
			"to the age recipient the vault's " + sops.ConfigFile + " names. The age key that opens them is\n" +
			"~/.config/mw/age.key (mode 600), on the home only. docs/secrets.md says what is kept, who\n" +
			"holds the key and how each token is revoked at its source.",
	}
	cmd.AddCommand(newSecretsPutCmd(), newSecretsGetCmd(), newSecretsListCmd())
	return cmd
}

func newSecretsPutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "put <name>",
		Short: "Keep the value on stdin under a name",
		Long: "put reads a value from stdin, never from its arguments, drops one trailing newline, and\n" +
			"keeps it under name in " + application.SecretsFile + ", then commits that file alone in the vault.\n" +
			"A name is letters, digits and underscores, starting with a letter.\n\n" +
			"  mw secrets put vultr_api_token < token.txt",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, vault, host, err := secretsStore()
			if err != nil {
				return err
			}
			return application.SecretsPut{
				Store: store,
				In:    cmd.InOrStdin(),
				Out:   cmd.OutOrStdout(),
				Vault: mwVault(vault, host),
			}.Run(cmd.Context(), args[0])
		},
	}
}

func newSecretsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Write the value kept under a name to stdout, never to a terminal",
		Long: "get writes the value kept under name to stdout, exactly, with no newline added, and only\n" +
			"when stdout is not a terminal: pipe it into what needs it.\n\n" +
			"  mw secrets get vultr_api_token | terraform ...",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, _, err := secretsStore()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			return application.SecretsGet{
				Store:         store,
				Out:           out,
				OutIsTerminal: toATerminal(out),
			}.Run(cmd.Context(), args[0])
		},
	}
}

func newSecretsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Name every secret kept, never a value",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _, _, err := secretsStore()
			if err != nil {
				return err
			}
			return application.SecretsList{Store: store, Out: cmd.OutOrStdout()}.Run(cmd.Context())
		},
	}
}

// secretsStore is the sops store over this host's vault and age key, with the
// vault directory and this host's name.
func secretsStore() (*sops.Store, string, string, error) {
	vault, err := config.Vault()
	if err != nil {
		return nil, "", "", err
	}
	host, err := config.Host()
	if err != nil {
		return nil, "", "", err
	}
	key, err := config.AgeKeyFile()
	if err != nil {
		return nil, "", "", err
	}
	return sops.New(vault, key), vault, host, nil
}

// toATerminal reports whether out may be a terminal. As atATerminal does for
// stdin, it asks the standard library rather than a true isatty: a character
// device may be a terminal. It errs toward refusing: /dev/null is one too, and
// a value is never written there.
func toATerminal(out io.Writer) bool {
	file, isFile := out.(*os.File)
	if !isFile {
		return false
	}
	info, err := file.Stat()
	return err != nil || info.Mode()&os.ModeCharDevice != 0
}
