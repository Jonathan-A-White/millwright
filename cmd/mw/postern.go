package main

import (
	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// newPosternCmd builds `mw postern`: the commands about the postern payment
// channel. It has no behaviour of its own; each subcommand is one thing to do
// with it.
func newPosternCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "postern",
		Short: "Work with the postern payment channel",
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
	root.AddCommand(newPosternInboxCmd())
	root.AddCommand(newPosternSendCmd())
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

// posternMemory is where mw postern inbox keeps its cursor: a note in this
// host's own vault, the same gateway every command notes through.
func posternMemory() (application.PosternNotes, error) {
	dir, err := config.Vault()
	if err != nil {
		return nil, err
	}
	host, err := config.Host()
	if err != nil {
		return nil, err
	}
	return mwGateway(dir, host), nil
}

// newPosternInboxCmd builds `mw postern inbox`. The postern backend and the
// cipher have no real adapter yet — a later story wires them in — so it
// refuses, naming what is missing, until then.
func newPosternInboxCmd() *cobra.Command {
	var unreadCount bool

	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "Read the postern's messages addressed to this host's key",
		Long: "inbox reads the postern's message records addressed to this host's key, decrypts\n" +
			"them, and prints them newest first: class, from, when and text. Reading marks them\n" +
			"read, by moving a cursor kept in a bd kv note, never an event of its own.\n\n" +
			"--unread-count prints only how many are unread, without reading them, so a notifier can\n" +
			"poll it without consuming anything.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			memory, err := posternMemory()
			if err != nil {
				return err
			}
			inbox := application.PosternInbox{
				Keys:   keys,
				Memory: memory,
				Out:    cmd.OutOrStdout(),
			}
			if unreadCount {
				_, err = inbox.UnreadCount(cmd.Context())
				return err
			}
			_, err = inbox.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&unreadCount, "unread-count", false, "print only how many messages are unread")
	return cmd
}

// newPosternSendCmd builds `mw postern send`. The postern backend and the
// cipher have no real adapter yet — a later story wires them in — so it
// refuses, naming what is missing, until then.
func newPosternSendCmd() *cobra.Command {
	var class string

	cmd := &cobra.Command{
		Use:   "send <text>",
		Short: "Send a message to the Governor over the postern",
		Long: "send builds a message record, classed --class, signs a transaction spending the\n" +
			"postern key's own testnet balance to carry it, and broadcasts it, printing the txid.\n\n" +
			"It refuses when postern_governor_key is not set, when the key's balance would exceed\n" +
			"postern_float_sats, naming the excess, or when --class is not one of message,\n" +
			"decision-needed, landing or alarm.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			governorKey, err := config.PosternGovernorKey()
			if err != nil {
				return err
			}
			floatSats, err := config.PosternFloatSats()
			if err != nil {
				return err
			}
			send := application.PosternSend{
				Keys:        keys,
				GovernorKey: governorKey,
				FloatSats:   int64(floatSats),
				Out:         cmd.OutOrStdout(),
			}
			_, err = send.Run(cmd.Context(), class, args[0])
			return err
		},
	}
	cmd.Flags().StringVar(&class, "class", "", "the message's class: message, decision-needed, landing or alarm (required)")
	return cmd
}
