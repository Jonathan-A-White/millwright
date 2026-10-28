package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/member"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newMemberCmd builds `mw member`: the factory of another person on this
// machine, who has their own Linux user. Like mw init it runs where there is
// no vault and no config file yet, so it reads neither.
func newMemberCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "member",
		Short: "Make, or stop, a member's own factory on this machine",
		Long:  "A member is another person with a Linux user of their own on this machine, whose factory\nis their own: its own vault, beads database and Dolt server, its own Claude login. See docs/family.md.",
	}
	cmd.AddCommand(newMemberInitCmd(), newMemberServeCmd(), newMemberRemoveCmd())
	return cmd
}

func newMemberInitCmd() *cobra.Command {
	var (
		name    string
		dir     string
		port    int
		holder  string
		backend string
		host    string
		prefix  string
		dolt    string
	)

	cmd := &cobra.Command{
		Use:   "init --name <name> --vault <dir> --dolt-port <port> --holder <name> --backend <url>",
		Short: "Make this Linux user's own factory: config, a Dolt user unit, a vault",
		Long: "Run AS the member's Linux user (not as the Governor), mw member init makes that user's own\n" +
			"factory and says what it did:\n\n" +
			"  ~/.config/mw/config.toml                  the vault, the host, cap = 1 and the Postern backend\n" +
			"  ~/.config/mw/beads.env                    bd's connection to the Dolt server below, with a generated\n" +
			"                                            password (mode 0600; it is printed nowhere)\n" +
			"  ~/.config/systemd/user/dolt-beads.service a Dolt user unit on --dolt-port, bound to 127.0.0.1,\n" +
			"                                            enabled and started; it serves ~/.local/share/mw/dolt-beads\n" +
			"  --vault                                   a git repository laid from the starter kit built into mw\n" +
			"                                            (seats/, CLAUDE.md), one commit, with its beads database made\n" +
			"                                            on that server (bd init against it: never bd migrate), and a\n" +
			"                                            ## Family section in seats/mayor/charter.md naming the holder\n\n" +
			"The Family section says that every epic is released only on the holder's approval (a decision\n" +
			"card they tap from their own account) and that the holder's key has the cockpit role here.\n\n" +
			"It checks everything it can before it writes anything, never overwrites, and refuses with\n" +
			"'already a member factory at <file>' when the user has one. --name, --vault, --dolt-port,\n" +
			"--holder and --backend are required; --host defaults to laptop, --prefix to --name and --dolt\n" +
			"to the dolt on the PATH. --dolt-port is from 1024 to 65535, free, and not another user's.\n\n" +
			"What it cannot do, and so leaves BY HAND (it says which are still owed when it ends):\n" +
			"  1. adduser: make the Linux user first (as root) and run this as that user.\n" +
			"  2. loginctl enable-linger <name> (as root), so the Dolt unit runs while the member is logged out.\n" +
			"  3. claude login: the member logs in to Claude as themselves.\n" +
			"  4. The git remote: make a private repository and git remote add origin, then git push, for the vault.\n\n" +
			"The way back is mw member remove, which stops and disables the unit and deletes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("there is no home directory to make a factory in: %w", err)
			}
			if dir, err = filepath.Abs(dir); err != nil {
				return err
			}
			birth := vault.NewBirth("mw@" + strings.TrimSpace(host))
			report, err := application.Member{
				Vault:    birth,
				Host:     &member.Host{},
				Template: millwright.Template(),
			}.Run(cmd.Context(), application.MemberRequest{
				Name:     strings.TrimSpace(name),
				Prefix:   strings.TrimSpace(prefix),
				Home:     home,
				Dir:      dir,
				DoltPort: port,
				Dolt:     dolt,
				Holder:   strings.TrimSpace(holder),
				Backend:  strings.TrimSpace(backend),
				Host:     strings.TrimSpace(host),
			})
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), report)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "the member's name (required)")
	cmd.Flags().StringVar(&dir, "vault", "", "the member's vault: a directory that does not exist or is empty (required)")
	cmd.Flags().IntVar(&port, "dolt-port", 0, "the port of the member's own Dolt server (required)")
	cmd.Flags().StringVar(&holder, "holder", "", "the licence holder, named in the Mayor charter's Family section (required)")
	cmd.Flags().StringVar(&backend, "backend", "", "the URL of the Postern backend (required)")
	cmd.Flags().StringVar(&host, "host", "laptop", "what the config file calls this host")
	cmd.Flags().StringVar(&prefix, "prefix", "", "what the ids of the member's stories begin with (default: --name)")
	cmd.Flags().StringVar(&dolt, "dolt", "", "the dolt program the unit runs (default: dolt on the PATH)")
	for _, required := range []string{"name", "vault", "dolt-port", "holder", "backend"} {
		cmd.MarkFlagRequired(required)
	}
	return cmd
}

func newMemberRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Stop and disable this Linux user's Dolt unit, and delete nothing",
		Long: "Run as the member's Linux user, mw member remove stops and disables dolt-beads.service\n" +
			"(systemctl --user disable --now). It deletes nothing: the config file, beads.env, the Dolt\n" +
			"data and the vault stay where they are, to be removed by hand if they are not wanted.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("there is no home directory to find a factory in: %w", err)
			}
			report, err := application.MemberRemove{Host: &member.Host{}}.Run(cmd.Context(), application.MemberRemoveRequest{Home: home})
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), report)
			return nil
		},
	}
}

func newMemberServeCmd() *cobra.Command {
	var wayBack bool

	cmd := &cobra.Command{
		Use:   "serve [--way-back]",
		Short: "Run this Linux user's own dispatch timer, so their Builders use their own sign-in",
		Long: "Run AS the member's Linux user, after mw member init, mw member serve installs the member's own\n" +
			"dispatcher: the holder's mw-dispatch.service and mw-dispatch.timer under this user's systemd\n" +
			"(hourly, like the holder's), so the member's Builders run on the member's own Codex sign-in.\n" +
			"Credentials do not cross users, so a dispatcher under the holder's user cannot serve them.\n\n" +
			"  ~/.config/systemd/user/mw-dispatch.service  the holder's dispatch service, as it is\n" +
			"  ~/.config/systemd/user/mw-dispatch.timer    the holder's timer; enabled and started\n" +
			"  ~/.config/mw/dispatch.env                   one PATH line naming where mw, bd, git, tmux and codex are\n" +
			"                                              (written only when there is none: the user owns it)\n\n" +
			"It refuses, writing nothing, when there is no member factory here, when a tool is not on the\n" +
			"PATH, when the dispatcher is already installed, and when loginctl show-user says linger is not\n" +
			"set: the timer would stop when the member logs out. It then prints the step left BY HAND, as\n" +
			"root: loginctl enable-linger <user>.\n\n" +
			"--way-back prints the commands that remove the dispatcher, and runs none of them.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("there is no home directory to find a factory in: %w", err)
			}
			if wayBack {
				fmt.Fprint(cmd.OutOrStdout(), application.MemberServeWayBack(home))
				return nil
			}
			me, err := user.Current()
			if err != nil {
				return fmt.Errorf("finding this Linux user: %w", err)
			}
			report, err := application.MemberServe{
				Host:    &member.Host{},
				Service: millwright.DispatchService,
				Timer:   millwright.DispatchTimer,
			}.Run(cmd.Context(), application.MemberServeRequest{Home: home, User: me.Username})
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), report)
			return nil
		},
	}
	cmd.Flags().BoolVar(&wayBack, "way-back", false, "print the commands that remove the dispatcher, and run none of them")
	return cmd
}
