package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
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
	root.AddCommand(newPosternSnapshotCmd())
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

// posternCipher is the cipher mw postern inbox and send use: BRC-78, from
// the postern key. A test swaps it for one whose ciphertext it knows.
var posternCipher = func(keys *postern.KeyFile) application.Cipher { return postern.NewCipher(keys) }

// posternClock stamps a message mw postern send builds. A test fixes it.
var posternClock = time.Now

// posternBackend is the postern backend config postern_backend points at.
func posternBackend() (*postern.HTTP, error) {
	base, err := config.PosternBackend()
	if err != nil {
		return nil, err
	}
	return postern.NewHTTP(base), nil
}

// posternGateway is the beads gateway mw postern inbox and send read and
// write through: the note store Inbox keeps its cursor and Send marks a
// question open in, the tracker each comments a bead through, and — Inbox
// only — the mailbox a recorded reply mails the Mayor through. host is this
// host's own name, config host, for the mail Inbox signs.
func posternGateway() (gateway *beads.Gateway, host string, err error) {
	dir, err := config.Vault()
	if err != nil {
		return nil, "", err
	}
	host, err = config.Host()
	if err != nil {
		return nil, "", err
	}
	return mwGateway(dir, host), host, nil
}

// newPosternInboxCmd builds `mw postern inbox`, reading from the postern
// backend at postern_backend and decrypting with the postern key.
func newPosternInboxCmd() *cobra.Command {
	var unreadCount bool

	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "Read the postern's messages addressed to this host's key",
		Long: "inbox reads the postern's message records addressed to this host's key, decrypts\n" +
			"them, and prints them newest first: class, from, when and text. Reading marks them\n" +
			"read, by moving a cursor kept in a bd kv note, never an event of its own.\n\n" +
			"A reply whose plaintext names a bead this host's tracker knows is not printed: its\n" +
			"answer is appended to the bead verbatim, with the txid and the sender's public key, the\n" +
			"question's note is cleared, and the Mayor is mailed so the notifier wakes the seat. A\n" +
			"reply naming a bead the tracker does not know is printed as text, and nothing is written.\n\n" +
			"--unread-count prints only how many are unread, without reading them, so a notifier can\n" +
			"poll it without consuming anything.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			gateway, host, err := posternGateway()
			if err != nil {
				return err
			}
			backend, err := posternBackend()
			if err != nil {
				return err
			}
			inbox := application.PosternInbox{
				Postern: backend,
				Cipher:  posternCipher(keys),
				Keys:    keys,
				Memory:  gateway,
				Tracker: gateway,
				Mailbox: gateway,
				Host:    host,
				Out:     cmd.OutOrStdout(),
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

// newPosternSendCmd builds `mw postern send`, encrypting from the postern key
// to postern_governor_key and broadcasting through the postern backend at
// postern_backend.
func newPosternSendCmd() *cobra.Command {
	var class, bead, recommend string
	var options []string

	cmd := &cobra.Command{
		Use:   "send <text>",
		Short: "Send a message to the Governor over the postern",
		Long: "send builds a message record, classed --class, signs a transaction spending the\n" +
			"postern key's own testnet balance to carry it, and broadcasts it, printing the txid.\n\n" +
			"It refuses when postern_governor_key is not set, when the key's balance would exceed\n" +
			"postern_float_sats, naming the excess, or when --class is not one of message,\n" +
			"decision-needed, landing or alarm.\n\n" +
			"--bead, --recommend and --option (repeatable) ask a decision-needed question about a\n" +
			"bead: <text> becomes the question, and postern's docs/protocol.md section 6 question is\n" +
			"sent in its place. Once broadcast, the bead is commented QUESTION with the txid and\n" +
			"marked open, so mw postern inbox knows a reply to it answers this bead. They are refused\n" +
			"with any --class but decision-needed.",
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
			backend, err := posternBackend()
			if err != nil {
				return err
			}
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			send := application.PosternSend{
				Postern:     backend,
				Cipher:      posternCipher(keys),
				Keys:        keys,
				Tracker:     gateway,
				Notes:       gateway,
				GovernorKey: governorKey,
				FloatSats:   int64(floatSats),
				Now:         posternClock,
				Out:         cmd.OutOrStdout(),
			}
			_, err = send.Run(cmd.Context(), application.PosternSendRequest{
				Class: class, Text: args[0], Bead: bead, Recommend: recommend, Options: options,
			})
			return err
		},
	}
	cmd.Flags().StringVar(&class, "class", "", "the message's class: message, decision-needed, landing or alarm (required)")
	cmd.Flags().StringVar(&bead, "bead", "", "the bead a decision-needed question is about")
	cmd.Flags().StringVar(&recommend, "recommend", "", "the option a decision-needed question recommends")
	cmd.Flags().StringArrayVar(&options, "option", nil, "an option a decision-needed question offers (repeatable)")
	return cmd
}

// posternSnapshotClock stamps the written_at a snapshot is built with. A test
// fixes it.
var posternSnapshotClock = time.Now

// newPosternSnapshotCmd builds `mw postern snapshot`: the brief of every live
// epic, encrypted to the Governor's key and written where nginx serves it.
func newPosternSnapshotCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Write the encrypted snapshot of every live epic for the Governor's app",
		Long: "snapshot builds postern's docs/protocol.md section 7 JSON of every epic open or in\n" +
			"progress: each epic's children still waiting on a decision-needed question\n" +
			"(needs_you), closed in the last 7 days and not yet marked VERIFIED on a comment\n" +
			"(landed), in progress then open and unblocked by priority (working), and how many\n" +
			"are neither (closed_count). It encrypts that JSON to postern_governor_key and writes\n" +
			"it atomically to postern_snapshot_path (default ~/.local/state/mw/snapshot.bin).\n\n" +
			"--json prints the plaintext instead of writing anything, for inspection.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			snapshot := application.PosternSnapshot{
				Tracker: gateway,
				Notes:   gateway,
				Now:     posternSnapshotClock,
			}
			if jsonOut {
				doc, err := snapshot.Build(cmd.Context())
				if err != nil {
					return err
				}
				encoded, err := json.Marshal(doc)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
				return nil
			}

			keys, err := posternKeys()
			if err != nil {
				return err
			}
			governorKey, err := config.PosternGovernorKey()
			if err != nil {
				return err
			}
			// Checked here, before Build ever runs: a key file that is not
			// there yet would otherwise surface only once Cipher.Encrypt
			// reads it, after a full — and possibly slow — read of every
			// live epic (mw-tfne4.8).
			if exists, err := keys.Exists(); err != nil {
				return err
			} else if !exists {
				return fmt.Errorf("no postern key at %s: run mw postern key init first", keys.Path())
			}
			path, err := config.PosternSnapshotPath()
			if err != nil {
				return err
			}
			snapshot.Cipher = posternCipher(keys)
			snapshot.File = postern.NewSnapshotFile(path)
			snapshot.GovernorKey = governorKey
			snapshot.Out = cmd.OutOrStdout()
			_, err = snapshot.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print the plaintext snapshot JSON instead of writing the encrypted file")
	return cmd
}
