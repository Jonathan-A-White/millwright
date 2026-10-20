package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/configfile"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// newGristCreditsCmd builds `mw grist credits`: the lookup and the switch over
// the state of the API credits the mill bills grists to.
func newGristCreditsCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "credits",
		Short: "Look up, switch on or off, and keep the Anthropic API key the mill bills grists to",
		Long: "credits (and credits status) prints: whether [grist] api_credits is on or off; whether the key file\n" +
			"(api_key_file) is there, yes or no, and never any of the key; the spent mark, the time the mill\n" +
			"found the key out of credit, or none; this UTC calendar month's grists billed to the key and the\n" +
			"sum of their total_cost_usd against $200; and the time of the last grist billed to the key.\n" +
			"--json prints the same as one object. It reads and changes nothing.\n\n" +
			"  on / off   set [grist] api_credits in this host's config.toml, through the writer mw config set\n" +
			"             uses, and say what changed and the way back\n" +
			"  key        read one line from stdin (echo off at a terminal) and write it as the key file, 0600,\n" +
			"             on the home only (mw home); an empty line or one not starting sk-ant- is refused\n" +
			"  clear      remove the spent mark, for when the month's credits renew",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			admin, err := newGristCreditsAdmin(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			return admin.Status(cmd.Context(), asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the status as one JSON object")

	status := &cobra.Command{
		Use:   "status",
		Short: "Print the credits' switch, key file, spent mark, this month's grists and cost, and the last one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			admin, err := newGristCreditsAdmin(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			return admin.Status(cmd.Context(), asJSON)
		},
	}
	status.Flags().BoolVar(&asJSON, "json", false, "print the status as one JSON object")
	cmd.AddCommand(status)

	for _, on := range []bool{true, false} {
		word, what := "off", "bill every grist to the subscription"
		if on {
			word, what = "on", "bill a grist to the API key while it has credit"
		}
		cmd.AddCommand(&cobra.Command{
			Use:   word,
			Short: "Set [grist] api_credits to " + fmt.Sprint(on) + ": " + what,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				admin, err := newGristCreditsAdmin(cmd.OutOrStdout())
				if err != nil {
					return err
				}
				return admin.Switch(cmd.Context(), on)
			},
		})
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "key",
		Short: "Read the Anthropic API key from stdin and write the key file, 0600, on the home only",
		Long: "key reads one line from stdin, with echo off when stdin is a terminal, and writes it as the file\n" +
			"api_key_file names (default ~/.config/mw/anthropic-api.key), mode 0600, written whole and renamed\n" +
			"into place. It refuses an empty line, a line that does not start with sk-ant-, and any host that is\n" +
			"not home (mw home), before it reads anything. It never prints the key, and does not turn the switch\n" +
			"on: mw grist credits on does.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			admin, err := newGristCreditsAdmin(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			return admin.SetKey(cmd.Context(), func() (string, error) {
				return readKeyLine(cmd.InOrStdin(), cmd.ErrOrStderr())
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Remove the spent mark, for when the month's credits renew",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			admin, err := newGristCreditsAdmin(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			return admin.Clear(cmd.Context())
		},
	})
	return cmd
}

// newGristCreditsAdmin is mw grist credits as this host is configured.
func newGristCreditsAdmin(out io.Writer) (application.GristCreditsAdmin, error) {
	settings, err := config.Grist()
	if err != nil {
		return application.GristCreditsAdmin{}, err
	}
	stateDir, err := config.GristStateDir()
	if err != nil {
		return application.GristCreditsAdmin{}, err
	}
	path, err := config.FilePath()
	if err != nil {
		return application.GristCreditsAdmin{}, err
	}
	dir, err := config.Vault()
	if err != nil {
		return application.GristCreditsAdmin{}, err
	}
	host, err := config.Host()
	if err != nil {
		return application.GristCreditsAdmin{}, err
	}
	return application.GristCreditsAdmin{
		On: settings.APICredits, KeyPath: settings.APIKeyFile,
		Keys: grist.CreditKeyFile{Path: settings.APIKeyFile}, State: &grist.CreditState{Dir: stateDir},
		Record: grist.New(stateDir), Config: configfile.New(path),
		Home: mwVault(dir, host), Host: host, Out: out,
	}, nil
}

// readKeyLine is one line of in. When in is a terminal it turns the terminal's
// echo off while the line is typed (stty -echo, which fails on anything that is
// not a terminal, so a pipe or a file is read as it is) and on again after,
// and the prompt goes to prompt.
func readKeyLine(in io.Reader, prompt io.Writer) (string, error) {
	if file, ok := in.(*os.File); ok {
		off := exec.Command("stty", "-echo")
		off.Stdin = file
		if off.Run() == nil {
			defer func() {
				on := exec.Command("stty", "echo")
				on.Stdin = file
				_ = on.Run()
				fmt.Fprintln(prompt)
			}()
			fmt.Fprint(prompt, "Anthropic API key (not shown): ")
		}
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err == io.EOF && line != "" {
		err = nil
	}
	return line, err
}
