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
// vault and no config file yet, so it reads neither.
func newInitCmd() *cobra.Command {
	var (
		dir    string
		prefix string
		host   string
		rigs   []string
	)

	cmd := &cobra.Command{
		Use:   "init --vault <dir> --prefix <prefix> [--host <name>] [--rig <name>=<dir>]...",
		Short: "Make a fresh vault from the template, and this host's config file if it has none",
		Long: "init lays the template built into mw into <dir>, which must not exist or must be empty,\n" +
			"makes it a git repository with one first commit, and makes a beads database there whose\n" +
			"story ids begin with <prefix>. It refuses, and writes nothing, if <dir> has anything in it.\n\n" +
			"It then writes ~/.config/mw/config.toml (the vault, the host, cap = 1 and a [rigs] table\n" +
			"of the --rig flags) only if that file does not exist. A file that is there is never read,\n" +
			"merged or changed: init prints the lines it would have written instead.\n\n" +
			"It ends by saying what is owed: a private remote for the vault and a push, then\n" +
			"scripts/install-units.sh.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			request, err := initRequest(dir, prefix, host, rigs)
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
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "vault", "", "the directory to make the vault in: it must not exist or must be empty (required)")
	cmd.Flags().StringVar(&prefix, "prefix", "", "what the ids of the vault's stories begin with (required)")
	cmd.Flags().StringVar(&host, "host", "", "what the config file calls this host (default: this machine's hostname)")
	cmd.Flags().StringArrayVar(&rigs, "rig", nil, "a rig checked out on this host, as <name>=<dir>; repeat for each")
	cmd.MarkFlagRequired("vault")
	cmd.MarkFlagRequired("prefix")
	return cmd
}

// initRequest turns the flags into what Init makes a vault from: every path
// made full, because the config file is read from any directory, and the host
// the machine's own name when none was given.
func initRequest(dir, prefix, host string, rigs []string) (application.InitRequest, error) {
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
		Host:       host,
		Rigs:       named,
		ConfigPath: filepath.Join(home, config.File),
	}, nil
}
