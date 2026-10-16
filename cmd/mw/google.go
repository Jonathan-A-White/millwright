package main

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/google"

	"github.com/spf13/cobra"
)

// googleEndpoints and googleOpen are Google's OAuth and this machine's
// browser; a test puts stand-ins in their place.
var (
	googleEndpoints = google.GoogleEndpoints
	googleOpen      = google.OpenBrowser
)

// newGoogleCmd builds `mw google`: the home's link to his Google account, for
// reading his contacts and nothing else. The refresh token is kept in
// ~/.config/mw/google-token.json, mode 0600, and is never printed, logged, or
// put in the vault, a bead or a mail.
func newGoogleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "google",
		Short: "Link his Google account on the home, for reading his contacts only",
		Long: "google links his Google account to the home's mw once, asking for one scope,\n" +
			application.GoogleContactsScope + ", nothing else. The sign-in uses\n" +
			"the Desktop OAuth client he made in the Google Cloud console, its JSON at\n" +
			"~/.config/mw/google-client.json (google_client_file in the config file, or $" + config.GoogleClientFileEnv + ").\n" +
			"The refresh token is kept in ~/.config/mw/google-token.json, mode 0600, and never shown.",
	}
	cmd.AddCommand(newGoogleLinkCmd(), newGoogleStatusCmd(), newGoogleUnlinkCmd())
	return cmd
}

func newGoogleLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link",
		Short: "Sign his Google account in for contacts.readonly and keep the token (home only)",
		Long: "link signs his Google account in through a browser on this machine (PKCE, a loopback\n" +
			"redirect on a free port), asking for contacts.readonly alone. The browser is opened\n" +
			"when it can be, and the address is printed either way. The refresh token is kept in\n" +
			"~/.config/mw/google-token.json, mode 0600, replacing any link before. It runs on the\n" +
			"home only; a grant of anything but contacts.readonly is revoked and nothing is kept.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			account, tokens, err := googleAccount()
			if err != nil {
				return err
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			return application.GoogleLink{
				Account: account,
				Tokens:  tokens,
				Out:     cmd.OutOrStdout(),
				Home: func(ctx context.Context) (bool, error) {
					return application.IsHome(ctx, mwVault(dir, host), host)
				},
				Open: googleOpen,
			}.Run(cmd.Context())
		},
	}
}

func newGoogleStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Say 'linked (<scope>)' or 'not linked', never the token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, tokens, err := googleAccount()
			if err != nil {
				return err
			}
			return application.GoogleStatus{Tokens: tokens, Out: cmd.OutOrStdout()}.Run(cmd.Context())
		},
	}
}

func newGoogleUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink",
		Short: "Have Google revoke the token, then delete the token file",
		Long: "unlink posts the kept token to Google's revoke endpoint, then deletes\n" +
			"~/.config/mw/google-token.json. While Google cannot be reached the file is kept and\n" +
			"unlink fails, so a live token is never forgotten here. Nothing linked is no error.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			account, tokens, err := googleAccount()
			if err != nil {
				return err
			}
			return application.GoogleUnlink{Account: account, Tokens: tokens, Out: cmd.OutOrStdout()}.Run(cmd.Context())
		},
	}
}

// googleAccount is Google's side of the link and the token file, from the
// config file.
func googleAccount() (google.OAuth, google.TokenFile, error) {
	client, err := config.GoogleClientFile()
	if err != nil {
		return google.OAuth{}, google.TokenFile{}, err
	}
	token, err := config.GoogleTokenFile()
	if err != nil {
		return google.OAuth{}, google.TokenFile{}, err
	}
	return google.OAuth{ClientFile: client, Endpoints: googleEndpoints}, google.TokenFile{File: token}, nil
}
