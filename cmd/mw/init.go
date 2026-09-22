package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newInitCmd builds `mw init`: the one command that runs where there is no
// vault and no config file yet, so it reads neither. --prefix makes a fresh
// vault; --join brings this host onto one that already exists instead.
func newInitCmd() *cobra.Command {
	var (
		dir    string
		prefix string
		join   string
		host   string
		rigs   []string
	)

	cmd := &cobra.Command{
		Use:   "init --vault <dir> (--prefix <prefix> | --join <git-url>) [--host <name>] [--rig <name>=<dir>]...",
		Short: "Make a fresh vault from the template, or join one that already exists",
		Long: "init lays the template built into mw into <dir>, which must not exist or must be empty,\n" +
			"makes it a git repository with one first commit, and makes a beads database there whose\n" +
			"story ids begin with <prefix>. It refuses, and writes nothing, if <dir> has anything in it.\n\n" +
			"init --join <git-url> brings this host onto a vault that already exists somewhere else,\n" +
			"instead: it clones <git-url> into <dir> and picks up its beads database with bd bootstrap\n" +
			"rather than making one — never bd init, never bd migrate, never a forced push. Running it\n" +
			"never makes this host the vault's designated migrator. --join and --prefix are refused\n" +
			"together: a joined vault already has its own database.\n\n" +
			"Either way it then writes ~/.config/mw/config.toml (the vault, the host, cap = 1 and a\n" +
			"[rigs] table of the --rig flags) only if that file does not exist. A file that is there is\n" +
			"never read, merged or changed: init prints the lines it would have written instead.\n\n" +
			"A fresh vault ends by saying what is owed: a private remote for the vault and a push, then\n" +
			"scripts/install-units.sh. A joined vault ends with one mw sync, printed or its failure, and\n" +
			"says what a MOVE of the Mayor's home still needs beyond what this command does.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			request, err := initRequest(dir, prefix, join, host, rigs)
			if err != nil {
				return err
			}

			report, err := application.Init{
				Vault:    vault.NewBirth("mw@" + request.Host),
				Tracker:  beads.New(request.Dir),
				Template: millwright.Template(),
			}.Run(cmd.Context(), request)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), report)

			if request.URL == "" {
				return nil
			}

			syncReport, err := application.Sync{
				Vault:   mwVault(request.Dir, request.Host),
				Tracker: mwGateway(request.Dir, request.Host),
				Host:    request.Host,
				Ticks:   hostTickLogs(),
			}.Run(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), syncReport)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "vault", "", "the directory to make, or clone, the vault in: it must not exist or must be empty (required)")
	cmd.Flags().StringVar(&prefix, "prefix", "", "what the ids of a fresh vault's stories begin with")
	cmd.Flags().StringVar(&join, "join", "", "the git url of a vault that already exists, to bring this host onto instead of making one")
	cmd.Flags().StringVar(&host, "host", "", "what the config file calls this host (default: this machine's hostname)")
	cmd.Flags().StringArrayVar(&rigs, "rig", nil, "a rig checked out on this host, as <name>=<dir>; repeat for each")
	cmd.MarkFlagRequired("vault")
	return cmd
}

// initRequest turns the flags into what Init makes a vault from, or joins one
// through: every path made full, because the config file is read from any
// directory, and the host the machine's own name when none was given.
func initRequest(dir, prefix, join, host string, rigs []string) (application.InitRequest, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return application.InitRequest{}, fmt.Errorf("there is no home directory to write %s in: %w", config.File, err)
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return application.InitRequest{}, err
	}
	if host = strings.TrimSpace(host); host == "" {
		if host, err = os.Hostname(); err != nil {
			return application.InitRequest{}, fmt.Errorf("this machine will not say its hostname: pass --host: %w", err)
		}
	}

	named := map[string]string{}
	for _, rig := range rigs {
		name, where, found := strings.Cut(rig, "=")
		if !found || name == "" || where == "" {
			return application.InitRequest{}, fmt.Errorf("--rig %q is not <name>=<dir>", rig)
		}
		if where, err = filepath.Abs(where); err != nil {
			return application.InitRequest{}, err
		}
		named[name] = where
	}

	return application.InitRequest{
		Dir:        dir,
		Prefix:     prefix,
		URL:        join,
		Host:       host,
		Rigs:       named,
		ConfigPath: filepath.Join(home, config.File),
	}, nil
}
