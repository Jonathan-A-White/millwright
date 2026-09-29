package main

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// newGristCmd builds `mw grist`: the mill, the factory's side of the AI work
// apps send it (postern's docs/protocol.md section 18).
func newGristCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "grist",
		Short: "The mill: answer the AI work apps send the factory",
		Args:  cobra.NoArgs,
	}
	root.AddCommand(newGristKeyCmd())
	root.AddCommand(newGristGrindCmd())
	return root
}

// gristKeys is the mill key file config points at.
func gristKeys() (*postern.KeyFile, error) {
	path, err := config.GristKeyFile()
	if err != nil {
		return nil, err
	}
	return postern.New(path), nil
}

// gristGrindLock is this host's grind lock, which mw dispatch counts as one
// of its sessions while it is held. A host whose grist state directory
// cannot be named has none, and counts none.
func gristGrindLock() application.GristLock {
	dir, err := config.GristStateDir()
	if err != nil {
		return nil
	}
	return hostlock.NewTry(dir, hostlock.GrindFile)
}

// newGristKeyCmd builds `mw grist key`: the mill key, made once, and its
// public half.
func newGristKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "key",
		Short: "Make the mill key if there is none, and print its public key and fingerprint",
		Long: "key generates the mill key (config grist_key_file, default ~/.config/mw/mill.key) when\n" +
			"there is none yet, 0600, a key of its own and never the Mayor's; then prints its public key\n" +
			"and its fingerprint. The postern backend is told the public key as POSTERN_MILL_KEY. It\n" +
			"never prints the private key, and never overwrites a key already there.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys, err := gristKeys()
			if err != nil {
				return err
			}
			_, err = application.GristKey{Keys: keys, Out: cmd.OutOrStdout()}.Run(cmd.Context())
			return err
		},
	}
}

// newGristGrindCmd builds `mw grist grind`: one pass of the mill.
func newGristGrindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "grind",
		Short: "Answer every grist waiting for the mill key, then stop",
		Long: "grind reads what the postern backend holds for the mill key since its last pass and answers\n" +
			"each grist once, sealed to its sender: refused when the sender's licence does not open the\n" +
			"app, the app's grind (grinds/<kind>.json at its rig's local main, config [grist-apps]) is\n" +
			"unknown, or the grist is past a grind's or the factory's limits (config [grist]); otherwise\n" +
			"ground in one short Claude Code session with no seat, answered or failed. A grind takes one\n" +
			"of this host's cap, first come first served: with none free, the grist waits for the next\n" +
			"pass, or for the next mw dispatch tick on the host that is home. Each grist handled is one line of grinds.jsonl in grist_state_dir, and its photos are\n" +
			"deleted from the backend once it is answered. The backend's POSTERN_ON_GRIST runs it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mill, err := newMill(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			_, err = mill.Run(cmd.Context())
			return err
		},
	}
}

// newMill is the mill as this host is configured to run it: what
// `mw grist grind` runs, and what the dispatch tick runs on the host that is
// home. It prints its report to out.
func newMill(out io.Writer) (application.GristGrind, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.GristGrind{}, err
	}
	host, err := config.Host()
	if err != nil {
		return application.GristGrind{}, err
	}
	atOnce, err := config.Cap()
	if err != nil {
		return application.GristGrind{}, err
	}
	keys, err := gristKeys()
	if err != nil {
		return application.GristGrind{}, err
	}
	backend, err := posternBackend(keys)
	if err != nil {
		return application.GristGrind{}, err
	}
	stateDir, err := config.GristStateDir()
	if err != nil {
		return application.GristGrind{}, err
	}
	apps, err := config.GristApps()
	if err != nil {
		return application.GristGrind{}, err
	}
	ceilings, err := config.Grist()
	if err != nil {
		return application.GristGrind{}, err
	}
	governorKey, err := config.PosternGovernorKey()
	if err != nil {
		return application.GristGrind{}, err
	}
	return application.GristGrind{
		Postern:     backend,
		Cipher:      postern.NewCipher(keys),
		Keys:        keys,
		State:       grist.New(stateDir),
		Grinds:      rig.NewGrinds(),
		Grinder:     claude.NewGrinder(),
		Tracker:     mwGateway(dir, host),
		Pass:        hostlock.NewTry(stateDir, hostlock.PassFile),
		Grinding:    hostlock.NewTry(stateDir, hostlock.GrindFile),
		Host:        host,
		Cap:         atOnce,
		Apps:        apps,
		GovernorKey: governorKey,
		Ceilings: application.GristCeilings{
			Models: ceilings.Models, MaxAttachments: ceilings.MaxAttachments,
			MaxAttachmentBytes: ceilings.MaxAttachmentBytes, DailyLimit: ceilings.DailyLimit,
			Timeout: ceilings.Timeout,
		},
		Out: out,
	}, nil
}
