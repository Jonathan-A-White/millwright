package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
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
	root.AddCommand(newPosternViewCmd())
	root.AddCommand(newPosternBeadCmd())
	root.AddCommand(newPosternServeCmd())
	root.AddCommand(newPosternNginxCmd())
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

// posternBackend is the postern backend config postern_backend points at,
// authenticating every call with keys — the Mayor's postern key — signing
// the backend's challenge (postern's docs/api.md Authentication section).
func posternBackend(keys *postern.KeyFile) (*postern.HTTP, error) {
	base, err := config.PosternBackend()
	if err != nil {
		return nil, err
	}
	return postern.NewHTTP(base, keys), nil
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
	var unreadCount, apply bool

	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "Read the postern's messages addressed to this host's key",
		Long: "inbox reads the postern's message records addressed to this host's key, decrypts\n" +
			"them, and prints them newest first: class, from, txid, thread, when and text. A message's\n" +
			"thread is the bead a decision-needed question (or its reply) names, the bead or topic its\n" +
			"own plaintext wrapper names (mw postern send --thread/--topic), or \"general\" otherwise.\n" +
			"Reading marks them read, by moving a cursor kept in a bd kv note, never an event of its own.\n\n" +
			"A message's sender is the BRC-78 envelope's own key, not the payload's own claim: a\n" +
			"payload — or, once the backend can supply one, a transaction signing key — that disagrees\n" +
			"with the envelope is printed with what it falsely claimed, and never read as a reply. A\n" +
			"verified sender equal to postern_governor_key prints as \"the Governor\".\n\n" +
			"A reply whose plaintext names a bead this host's tracker knows is not printed: its\n" +
			"answer is appended to the bead verbatim, with the txid and the sender's public key, the\n" +
			"question's note is cleared, and the Mayor is mailed so the notifier wakes the seat. A\n" +
			"reply naming a bead the tracker does not know is printed as text, and nothing is written.\n\n" +
			"A Release tap — an answer, trimmed and case-folded, of \"release\" — releases the epic's\n" +
			"held stories itself, exactly as mw release would, but only when the reply's verified\n" +
			"sender is postern_governor_key and the question it answers offered Release among its\n" +
			"options. Any other signer, a question that never offered Release, a bead that is not an\n" +
			"epic, or one with nothing held, releases nothing; the Mayor is always mailed why.\n\n" +
			"A message carrying an attachment is downloaded, its sha256 checked against the hash it\n" +
			"was announced under, decrypted with the postern key, and written 0600 under the postern\n" +
			"inbox's own state directory, named by the message's txid; its path is printed under the\n" +
			"message, and a bead comment naming it ends with \" [image: <path>]\".\n\n" +
			"--unread-count prints only how many are unread, without reading them, so a notifier can\n" +
			"poll it without consuming anything.\n\n" +
			"--apply is the zero-token pass the postern backend's on-message hook runs: every message\n" +
			"since the cursor that the Governor verifiably sent and this host knows how to apply is\n" +
			"applied at once, as the Governor — a reply, a comment in a bead's thread, or an action\n" +
			"(postern's docs/protocol.md section 13: release, hold, priority, verified) — each at most\n" +
			"once per txid, commented on its bead and mailed to the Mayor. It moves no cursor and\n" +
			"prints no message's text, only one line per message applied. A plain read then shows an\n" +
			"applied message as that one line: applied <kind> <bead> txid <id>. An action this host\n" +
			"does not know, or whose record's signer the backend did not vouch for, is left for the\n" +
			"Mayor to read as text.\n\n" +
			"A voice note from the Governor (an audio attachment, section 14) is heard on this host\n" +
			"when postern_transcribe_cmd is set: the command, split on whitespace, runs with the\n" +
			"decrypted audio's path appended, for at most 5 minutes, and what it prints is the\n" +
			"transcript. It is written on the bead (GOVERNOR (voice) via postern, txid <id>:\n" +
			"<transcript>), sent back to the Governor in the same thread as a transcript of the note,\n" +
			"and mailed to the Mayor — once per txid, in the --apply pass or the Mayor's read,\n" +
			"whichever sees it first. contrib/postern-transcribe is the command this rig ships.",
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
			backend, err := posternBackend(keys)
			if err != nil {
				return err
			}
			governorKey, err := config.PosternGovernorKey()
			if err != nil {
				return err
			}
			attachmentDir, err := config.PosternInboxDir()
			if err != nil {
				return err
			}
			inbox := application.PosternInbox{
				Postern:       backend,
				Cipher:        posternCipher(keys),
				Keys:          keys,
				Memory:        gateway,
				Tracker:       gateway,
				Mailbox:       gateway,
				Host:          host,
				GovernorKey:   governorKey,
				AttachmentDir: attachmentDir,
				Lock:          posternInboxLock(attachmentDir),
				Out:           cmd.OutOrStdout(),
			}
			if unreadCount {
				_, err = inbox.UnreadCount(cmd.Context())
				return err
			}
			if err := hearVoiceNotes(&inbox, backend, keys, governorKey); err != nil {
				return err
			}
			if apply {
				_, err = inbox.Apply(cmd.Context())
				return err
			}
			_, err = inbox.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&unreadCount, "unread-count", false, "print only how many messages are unread")
	cmd.Flags().BoolVar(&apply, "apply", false, "apply the Governor's replies, bead comments and actions at once, moving no cursor")
	cmd.MarkFlagsMutuallyExclusive("unread-count", "apply")
	return cmd
}

// hearVoiceNotes gives inbox a transcriber, when postern_transcribe_cmd names
// one, and a sender to hand each transcript back to the Governor by, on
// postern_channel. With no transcriber configured it changes nothing, so a
// read never fails for a sending setting it would not use.
func hearVoiceNotes(inbox *application.PosternInbox, backend application.Postern, keys *postern.KeyFile, governorKey string) error {
	command, err := config.PosternTranscribeCmd()
	if err != nil || command == "" {
		return err
	}
	channel, err := config.PosternChannel()
	if err != nil {
		return err
	}
	floatSats, err := config.PosternFloatSats()
	if err != nil {
		return err
	}
	inbox.Transcriber = postern.NewCommandTranscriber(command)
	inbox.Sender = &application.PosternSend{
		Postern:     backend,
		Cipher:      inbox.Cipher,
		Keys:        keys,
		GovernorKey: governorKey,
		FloatSats:   int64(floatSats),
		Channel:     channel,
		Now:         posternClock,
	}
	return nil
}

// posternInboxLockWait is how long a pass waits for another to finish: longer
// than the five minutes a voice note's transcription may take.
const posternInboxLockWait = 6 * time.Minute

// posternInboxLock is the lock every postern inbox read and apply pass takes
// on this host, kept beside the attachments it writes, so that two passes
// never apply one message together.
func posternInboxLock(attachmentDir string) *hostlock.Lock {
	return hostlock.New(filepath.Dir(attachmentDir), hostlock.WithWait(posternInboxLockWait))
}

// newPosternSendCmd builds `mw postern send`, encrypting from the postern key
// to postern_governor_key and sending through the postern backend at
// postern_backend, by postern_channel.
func newPosternSendCmd() *cobra.Command {
	var class, bead, recommend, thread, topic string
	var options, attach []string

	cmd := &cobra.Command{
		Use:   "send [<text>]",
		Short: "Send a message to the Governor over the postern",
		Long: "send builds a message record, classed --class (default message), and sends it by\n" +
			"postern_channel, printing the txid. The direct channel (the default) hands the record\n" +
			"straight to the postern backend (postern's docs/protocol.md section 9), whose txid is\n" +
			"direct:<sha256>; the chain channel signs a transaction spending the postern key's own\n" +
			"testnet balance to carry it, and broadcasts it.\n\n" +
			"It refuses when postern_governor_key is not set, when --class is not one of message,\n" +
			"decision-needed, landing or alarm, or — on the chain channel only — when the key's\n" +
			"balance would exceed postern_float_sats, naming the excess.\n\n" +
			"--bead, --recommend and --option (repeatable) ask a decision-needed question about a\n" +
			"bead: <text> becomes the question, and postern's docs/protocol.md section 6 question is\n" +
			"sent in its place. Once broadcast, the bead is commented QUESTION with the txid and\n" +
			"marked open, so mw postern inbox knows a reply to it answers this bead. They are refused\n" +
			"with any --class but decision-needed.\n\n" +
			"--thread <bead-id> or --topic <name> wraps <text> in postern's docs/protocol.md section\n" +
			"6 thread envelope, so mw postern inbox prints it under that bead or topic rather than the\n" +
			"general thread. They are mutually exclusive, and refused alongside --bead: a decision-needed\n" +
			"question's own bead is already its thread. Once sent, a message in a bead's thread is\n" +
			"commented on that bead too: MAYOR via postern, txid <id>: <text>.\n\n" +
			"--attach <file> (repeatable) encrypts the file to the Governor, uploads it to the\n" +
			"backend's blob store and announces it in the message (sections 8 and 14): at most 8 MiB,\n" +
			"typed by its extension — .png .jpg .jpeg .webp .webm .ogg .oga .opus .m4a .mp4 .mp3\n" +
			".pdf .txt .md .log. Several files are several messages, <text> the caption on the last.\n" +
			"Refused with --bead. <text> may be left out when a file is attached.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := ""
			if len(args) == 1 {
				text = args[0]
			}
			if len(args) == 0 && len(attach) == 0 {
				return fmt.Errorf("mw postern send: what should it say? give the text, or --attach a file")
			}
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
			channel, err := config.PosternChannel()
			if err != nil {
				return err
			}
			backend, err := posternBackend(keys)
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
				Channel:     channel,
				Now:         posternClock,
				Out:         cmd.OutOrStdout(),
			}
			_, err = send.Run(cmd.Context(), application.PosternSendRequest{
				Class: class, Text: text, Bead: bead, Recommend: recommend, Options: options,
				Thread: thread, Topic: topic, Attachments: attach,
			})
			return err
		},
	}
	cmd.Flags().StringArrayVar(&attach, "attach", nil, "a file to send with the message, encrypted to the Governor (repeatable)")
	cmd.Flags().StringVar(&class, "class", "", "the message's class: message, decision-needed, landing or alarm (default message)")
	cmd.Flags().StringVar(&bead, "bead", "", "the bead a decision-needed question is about")
	cmd.Flags().StringVar(&recommend, "recommend", "", "the option a decision-needed question recommends")
	cmd.Flags().StringArrayVar(&options, "option", nil, "an option a decision-needed question offers (repeatable)")
	cmd.Flags().StringVar(&thread, "thread", "", "the bead this message's thread is (refused with --topic or a decision-needed question)")
	cmd.Flags().StringVar(&topic, "topic", "", "the named topic this message's thread is (refused with --thread or a decision-needed question)")
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
			"(needs_you), closed in the last 24 hours and not yet marked VERIFIED on a comment\n" +
			"(landed), in progress then open and unblocked by priority (working), and how many\n" +
			"are closed in all (closed_count). A needs_you, working or landed entry carries the\n" +
			"bead's description and newest three comments too, each cut to 4000 runes. It\n" +
			"encrypts that JSON to postern_governor_key and writes it atomically to\n" +
			"postern_snapshot_path (default ~/.local/state/mw/snapshot.bin).\n\n" +
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

// newPosternServeCmd builds `mw postern serve`: the hand step of setting
// this host's postern config lines and making the snapshot's directory, and
// — given the postern backend's environment file — the backend's own
// POSTERN_ lines, idempotent and printing the way back. Every value it
// writes defaults to what this host's config already says, so a flag is only
// needed to change one.
func newPosternServeCmd() *cobra.Command {
	var backend, snapshotPath, governorKey, backupDir string
	var envFile, addr, viewPath, mwPath, mayorKey string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Set this host's postern config lines, and the postern backend's environment",
		Long: "serve writes or replaces postern_backend, postern_snapshot_path and postern_governor_key\n" +
			"in this host's config file — idempotent, so a re-run with the same values changes\n" +
			"nothing — and makes the snapshot's own directory. It backs the config file up first,\n" +
			"unless there is none yet to back up, and prints the backup path and the way back. Each\n" +
			"value defaults to what the config already says (postern_backend's own default is\n" +
			"http://desktop.mw:8787); postern_governor_key has none, so it must be said once.\n\n" +
			"--env-file names the postern backend's environment file (a systemd EnvironmentFile), on\n" +
			"the host the backend runs on. serve then also writes or replaces the backend's own lines\n" +
			"there, every other line left as it was, backed up first the same way:\n" +
			"  POSTERN_ADDR        --addr (default 127.0.0.1:8787; the desktop's is 10.88.0.3:8787)\n" +
			"  POSTERN_VIEW_FILE   --view-path (default postern_view_path, ~/.local/state/postern/view.b64)\n" +
			"  POSTERN_BEAD_CMD    \"<--mw> postern bead\" (--mw defaults to this mw)\n" +
			"  POSTERN_ON_MESSAGE  \"<--mw> postern inbox --apply\"\n" +
			"  POSTERN_MAYOR_KEY   --mayor-key (default this host's postern key's public half)\n" +
			"  POSTERN_ISSUER_KEY  the Governor's key: he issues the licence\n" +
			"and writes postern_view_path into the config beside the three, and makes the view's\n" +
			"directory. Restart the backend afterwards: it reads its environment only when it starts.\n\n" +
			"--dry-run prints everything it would write without touching anything.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("there is no home directory to write %s in: %w", config.File, err)
			}
			if backend == "" {
				if backend, err = config.PosternBackend(); err != nil {
					return err
				}
			}
			if snapshotPath == "" {
				if snapshotPath, err = config.PosternSnapshotPath(); err != nil {
					return err
				}
			}
			if governorKey == "" {
				if governorKey, err = config.PosternGovernorKey(); err != nil {
					return err
				}
			}
			if envFile != "" {
				if viewPath == "" {
					if viewPath, err = config.PosternViewPath(); err != nil {
						return err
					}
				}
				if mwPath == "" {
					if mwPath, err = os.Executable(); err != nil {
						return fmt.Errorf("finding the mw that is running, for the backend to run: %w", err)
					}
				}
				if mayorKey == "" {
					if mayorKey, err = posternMayorKey(); err != nil {
						return err
					}
				}
			}
			serve := application.PosternServe{
				Files:      postern.NewHandFile(),
				ConfigPath: filepath.Join(home, config.File),
				BackupDir:  backupDir,
				Out:        cmd.OutOrStdout(),
			}
			_, err = serve.Run(cmd.Context(), application.PosternServeRequest{
				Backend: backend, SnapshotPath: snapshotPath, GovernorKey: governorKey, DryRun: dryRun,
				EnvFile: envFile, Addr: addr, ViewPath: viewPath, Mw: mwPath, MayorKey: mayorKey,
			})
			return err
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "", "the postern backend's URL (default: postern_backend)")
	cmd.Flags().StringVar(&snapshotPath, "snapshot-path", "", "where mw postern snapshot writes the encrypted snapshot, a full path (default: postern_snapshot_path)")
	cmd.Flags().StringVar(&governorKey, "governor-key", "", "the Governor's compressed public key, as hex (default: postern_governor_key)")
	cmd.Flags().StringVar(&envFile, "env-file", "", "the postern backend's environment file, a full path, to write its POSTERN_ lines into")
	cmd.Flags().StringVar(&addr, "addr", application.DefaultPosternAddr, "where the backend listens, POSTERN_ADDR")
	cmd.Flags().StringVar(&viewPath, "view-path", "", "the live view the backend serves, POSTERN_VIEW_FILE (default: postern_view_path)")
	cmd.Flags().StringVar(&mwPath, "mw", "", "the full path of the mw the backend runs (default: this mw)")
	cmd.Flags().StringVar(&mayorKey, "mayor-key", "", "the Mayor's postern public key, as hex (default: this host's postern key)")
	cmd.Flags().StringVar(&backupDir, "backup-dir", application.DefaultPosternHandBackupDir, "where the files are backed up before they are changed")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would change without touching anything")
	return cmd
}

// posternMayorKey is the public half of this host's postern key: the Mayor's,
// on the host the Mayor sits on.
func posternMayorKey() (string, error) {
	keys, err := posternKeys()
	if err != nil {
		return "", err
	}
	exists, err := keys.Exists()
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("no postern key at %s to name the Mayor's key by: run mw postern key init first, or pass --mayor-key", keys.Path())
	}
	key, _, err := keys.PublicKey()
	return key, err
}

// newPosternNginxCmd builds `mw postern nginx`: the VPS-local hand step of
// ensuring the postern's /api/events and /snapshot locations and its /api
// upstream in the nginx site, backed up, tested and reloaded, printing the
// way back.
func newPosternNginxCmd() *cobra.Command {
	var conf, backend, backupDir string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "nginx",
		Short: "Ensure the postern's /api/events and /snapshot locations and /api upstream in the nginx site",
		Long: "nginx ensures a `location = /api/events` block ahead of the general /api/ location — the\n" +
			"backend's event stream, with proxy_buffering and proxy_cache off, proxy_read_timeout 1h,\n" +
			"proxy_http_version 1.1 and the Connection header cleared — and a `location = /snapshot`\n" +
			"block, aliased to postern_snapshot_path, and points the /api upstream(s) at --backend\n" +
			"(default: postern_backend, http://desktop.mw:8787), in the nginx site named by --conf —\n" +
			"idempotent, so a re-run that would change nothing touches nothing. It backs the site file\n" +
			"up first, writes it, then runs `nginx -t` and, only once that passes, `systemctl reload\n" +
			"nginx`, printing every command and the way back. A failed nginx -t restores the backup, so\n" +
			"a bad edit is never left live.\n\n" +
			"--dry-run prints what it would do without touching anything.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			snapshotPath, err := config.PosternSnapshotPath()
			if err != nil {
				return err
			}
			if backend == "" {
				if backend, err = config.PosternBackend(); err != nil {
					return err
				}
			}
			nginx := application.PosternNginx{
				Conf:      postern.NewHandFile(),
				ConfPath:  conf,
				BackupDir: backupDir,
				Runner:    postern.NewNginxRunner(),
				Out:       cmd.OutOrStdout(),
			}
			_, err = nginx.Run(cmd.Context(), application.PosternNginxRequest{
				Backend: backend, SnapshotPath: snapshotPath, DryRun: dryRun,
			})
			return err
		},
	}
	cmd.Flags().StringVar(&conf, "conf", "", "the nginx site file to edit (required)")
	cmd.Flags().StringVar(&backend, "backend", "", "the /api upstream's URL (default: postern_backend)")
	cmd.Flags().StringVar(&backupDir, "backup-dir", application.DefaultPosternHandBackupDir, "where the site file is backed up before it is changed")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would change without touching anything")
	_ = cmd.MarkFlagRequired("conf")
	return cmd
}
