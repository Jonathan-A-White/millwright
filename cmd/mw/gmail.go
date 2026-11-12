package main

import (
	"fmt"

	"github.com/Jonathan-A-White/millwright/infrastructure/gdrive"
	"github.com/Jonathan-A-White/millwright/infrastructure/gmail"

	"github.com/spf13/cobra"
)

// newGmailCmd builds `mw gmail`: a read-only look into his Gmail, on the same
// Google sign-in as mw gdrive (the same client, the same token under
// ~/.config/mw/gdrive/). Nothing is sent, changed, deleted or labelled; an
// attachment is named, not downloaded; no token or secret is printed.
func newGmailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gmail",
		Short: "Search and read his Gmail, read-only, on the mw gdrive sign-in (search, read)",
		Long: "gmail searches and reads his Gmail, signed in as him on the same Google sign-in as mw gdrive\n" +
			"(scope " + gdrive.GmailScope + ", read-only: nothing is sent, changed, deleted\n" +
			"or labelled). A sign-in made before Gmail was added needs mw gdrive auth run again.",
	}
	cmd.AddCommand(newGmailSearchCmd(), newGmailReadCmd())
	return cmd
}

func gmailClient() (gmail.Client, error) {
	s, err := gdriveSession()
	if err != nil {
		return gmail.Client{}, err
	}
	return gmail.Client{Session: s}, nil
}

func newGmailSearchCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "search <gmail query> [--limit N]",
		Short: "List messages matching a Gmail search, newest first: id, date, from, subject",
		Long: "search lists the messages matching <gmail query> (Gmail's own search syntax, like\n" +
			"'from:bills newer_than:30d'), newest first, one per line: id, date (UTC), from, subject,\n" +
			"tab-separated. --limit is how many at most (default 10, at most 100).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := gmailClient()
			if err != nil {
				return err
			}
			found, err := c.Search(cmd.Context(), args[0], limit)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(found) == 0 {
				fmt.Fprintf(out, "no message matches %q\n", oneLine(args[0]))
				return nil
			}
			for _, m := range found {
				fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", m.ID, m.Date, m.From, m.Subject)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", gmail.DefaultLimit, "the most messages to list (at most 100)")
	return cmd
}

func newGmailReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read <id>",
		Short: "Print a message's headers and plain-text body; attachments are named, not downloaded",
		Long: "read prints the message's From, To, Cc, Date and Subject, then its plain-text body (the\n" +
			"HTML part stripped to text when there is no text part), then the names of its attachments.\n" +
			"Attachments are not downloaded.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := gmailClient()
			if err != nil {
				return err
			}
			m, err := c.Read(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, h := range m.Headers {
				fmt.Fprintf(out, "%s: %s\n", h.Name, h.Value)
			}
			fmt.Fprintf(out, "\n%s\n", m.Body)
			for _, a := range m.Attachments {
				fmt.Fprintf(out, "\nattachment: %s (%s, %d bytes, not downloaded)", a.Name, a.MimeType, a.Size)
			}
			if len(m.Attachments) > 0 {
				fmt.Fprintln(out)
			}
			return nil
		},
	}
}
