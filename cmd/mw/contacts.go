package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/google"

	"github.com/spf13/cobra"
)

// peopleEndpoint is Google's People API; a test puts a stand-in in its place.
var peopleEndpoint = google.PeopleEndpoint

// contactBook is his address book through the Google link, or nil when the
// config cannot say where the link is kept: a card is then written as typed.
func contactBook() application.ContactBook {
	account, tokens, err := googleAccount()
	if err != nil {
		return nil
	}
	return google.People{Tokens: tokens, Access: account, Endpoint: peopleEndpoint}
}

// newContactsCmd builds `mw contacts`: the home's look into his address book
// through the Google link (mw google link), for names and mobile numbers only.
func newContactsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contacts",
		Short: "Look up a name in his Google contacts (home only; needs mw google link)",
	}
	cmd.AddCommand(newContactsFindCmd())
	return cmd
}

func newContactsFindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "find <name>",
		Short: "Print each contact the name matches with its mobile number",
		Long: "find searches his Google contacts for <name> (people.searchContacts, reading names and\n" +
			"phone numbers only) and prints a line for each match: the name, two spaces and its\n" +
			"mobile numbers, or '(no mobile number)'. With no match it prints 'no contact named <name>'\n" +
			"and exits 0. Not linked, it says 'not linked: mw google link' and fails.\n\n" +
			"A To do card with a name-only 'Text: <name> | <draft>' line is given the one number this\n" +
			"finds, when it finds exactly one contact with exactly one mobile, as it is filed or edited.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			account, tokens, err := googleAccount()
			if err != nil {
				return err
			}
			book := google.People{Tokens: tokens, Access: account, Endpoint: peopleEndpoint}
			return application.ContactsFind{Book: book, Out: cmd.OutOrStdout()}.Run(cmd.Context(), args[0])
		},
	}
}
