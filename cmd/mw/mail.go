package main

import (
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newMailCmd builds `mw mail`: the seats' mail. It has no behaviour of its own;
// each subcommand is one thing a seat does with its mail.
func newMailCmd() *cobra.Command {
	mail := &cobra.Command{
		Use:   "mail",
		Short: "Send, list and read the mail between seats",
		Long: "mail is how sessions, seats and the Governor leave each other messages. Mail is beads: a\n" +
			"message is a bead of type mail, so it travels between hosts with `mw sync`.\n\n" +
			"A message is signed by the seat in $MW_SEAT (seat or seat@host), which is never defaulted,\n" +
			"and inbox and read take their mailbox from $MW_SEAT too, or from --as.",
		Args: cobra.NoArgs,
	}
	mail.AddCommand(newMailSendCmd(), newMailInboxCmd(), newMailReadCmd())
	return mail
}

// mailUseCase is the mail use case as this session runs it: on this host's
// vault, as the seat $MW_SEAT names.
func mailUseCase(cmd *cobra.Command) (application.Mail, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.Mail{}, err
	}
	host, err := config.Host()
	if err != nil {
		return application.Mail{}, err
	}
	return application.Mail{
		Mailbox: mwGateway(dir, host),
		Seat:    os.Getenv(application.SeatEnv),
		Out:     cmd.OutOrStdout(),
	}, nil
}

// newMailSendCmd builds `mw mail send`.
func newMailSendCmd() *cobra.Command {
	var subject, body string

	cmd := &cobra.Command{
		Use:   "send <to> -s <subject> [-m <body>]",
		Short: "Send a message from the seat in $MW_SEAT",
		Long: "send files a message to a seat's mailbox (mayor, builder, governor, or any seat) and\n" +
			"prints its id and who it went to. It is signed by $MW_SEAT, seat or seat@host; with\n" +
			"$MW_SEAT unset it refuses and writes nothing, and never signs as anyone by default.\n" +
			"A message sent with no body says so.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mail, err := mailUseCase(cmd)
			if err != nil {
				return err
			}
			_, err = mail.Send(cmd.Context(), args[0], subject, body)
			return err
		},
	}
	cmd.Flags().StringVarP(&subject, "subject", "s", "", "the subject of the message (required)")
	cmd.Flags().StringVarP(&body, "body", "m", "", "the body of the message")
	return cmd
}

// newMailInboxCmd builds `mw mail inbox`.
func newMailInboxCmd() *cobra.Command {
	var as string

	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "List the unread mail of a seat",
		Long: "inbox lists the unread mail of the seat in $MW_SEAT, or of the seat --as names, oldest\n" +
			"first: one line each with its id, who it is from, when it was sent and its subject.\n" +
			"It reads and writes nothing. With neither $MW_SEAT nor --as it refuses.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mail, err := mailUseCase(cmd)
			if err != nil {
				return err
			}
			_, err = mail.Inbox(cmd.Context(), as)
			return err
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "list this seat's mailbox instead of the one $MW_SEAT names")
	return cmd
}

// newMailReadCmd builds `mw mail read`.
func newMailReadCmd() *cobra.Command {
	var as string

	cmd := &cobra.Command{
		Use:   "read <id>",
		Short: "Print a message and mark it read",
		Long: "read prints a message's from, to, date, subject and body, and marks it read, which takes\n" +
			"it out of its recipient's inbox. Reading it again prints it again and changes nothing. It\n" +
			"is signed by $MW_SEAT, or by the seat --as names. An id that is not mail is refused.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mail, err := mailUseCase(cmd)
			if err != nil {
				return err
			}
			_, err = mail.Read(cmd.Context(), args[0], as)
			return err
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "read as this seat instead of the one $MW_SEAT names")
	return cmd
}
