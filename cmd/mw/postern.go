package main

import (
	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// newPosternCmd builds `mw postern`: the commands about the postern payment
// key. It has no behaviour of its own; each subcommand is one thing to do
// with it.
func newPosternCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "postern",
		Short: "Work with the postern payment key",
		Args:  cobra.NoArgs,
	}
	key := &cobra.Command{
		Use:   "key",
		Short: "Work with the postern's testnet key",
		Args:  cobra.NoArgs,
	}
	key.AddCommand(newPosternKeyInitCmd())
	key.AddCommand(newPosternKeyShowCmd())
	root.AddCommand(key)
	return root
}

// posternKeys is the postern key file config points at.
func posternKeys() (*postern.KeyFile, error) {
	path, err := config.PosternKeyFile()
	if err != nil {
		return nil, err
	}
	return postern.New(path), nil
}

// newPosternKeyInitCmd builds `mw postern key init`: makes the Mayor's
// postern key, once.
func newPosternKeyInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate the postern's testnet key, once",
		Long: "init generates a new secp256k1 testnet key and writes it 0600 to the postern key file\n" +
			"(config postern_key_file, default ~/.config/mw/postern.key), outside the vault and its\n" +
			"backups. It refuses to overwrite a key that is already there.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			_, err = application.PosternKeyInit{
				Keys: keys,
				Out:  cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
}

// newPosternKeyShowCmd builds `mw postern key show`: prints the postern
// key's public half. It never prints the private key.
func newPosternKeyShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the postern key's public key and testnet address",
		Long: "show prints the compressed public key and the testnet address of the postern key\n" +
			"(config postern_key_file, default ~/.config/mw/postern.key). It never prints the private key.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			_, err = application.PosternKeyShow{
				Keys: keys,
				Out:  cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
}
