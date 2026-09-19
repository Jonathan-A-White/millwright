package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// SeatEnv is the environment variable that says which seat a session is: what
// mw mail signs a message with, and whose mailbox it reads. A session started
// by mw dispatch carries it as <seat>@<host>.
const SeatEnv = "MW_SEAT"

// NoBody is what a message with nothing to say is stored with, so that the
// description it is kept in is never empty.
const NoBody = "(no body)"

// NewMessage is a message about to be sent: who from, who to, and what it says.
// From is the seat's own name, seat or seat@host, and To is a mailbox: mayor,
// builder, governor, or any seat. ReplyTo is the id of the message this one
// answers, or empty when it answers none; a message that answers one is linked
// to it.
type NewMessage struct {
	From    string
	To      string
	Subject string
	Body    string
	ReplyTo string
}

// Message is one message as the mailbox holds it.
type Message struct {
	ID      string
	From    string
	To      string
	Subject string
	Body    string
	// Sent is when it was sent; zero when the mailbox did not say.
	Sent time.Time
	// ReplyTo is the id of the message this one answers; empty when it answers
	// none.
	ReplyTo string
}

// Mailbox is the port mail travels through. Mail is beads, so it goes between
// hosts as the rest of the tracker does, on a sync; the adapter that talks to
// beads and an in-memory one for tests both satisfy it.
type Mailbox interface {
	// Send stores a message and reports the id it was given. It is unread until
	// its recipient reads it.
	Send(ctx context.Context, message NewMessage) (string, error)

	// Inbox lists the unread messages addressed to a mailbox, oldest first. It
	// reads and writes nothing.
	Inbox(ctx context.Context, mailbox string) ([]Message, error)

	// Get reports one message and changes nothing: it stays as unread as it was.
	// An id that names no message — including a bead that is not mail — is an
	// error.
	Get(ctx context.Context, id string) (Message, error)

	// Read reports one message and marks it read, so that it leaves its
	// recipient's inbox; reader is who read it. Reading a message that is
	// already read reports it again and changes nothing. An id that names no
	// message — including a bead that is not mail — is an error and nothing is
	// changed.
	Read(ctx context.Context, id, reader string) (Message, error)
}

// Mail is what a seat does with its mail: send, reply, list its inbox, read a message.
// Seat is the seat this session is, from $MW_SEAT, and it is never defaulted:
// a message signed by no one, or by the wrong seat, cannot be answered.
type Mail struct {
	Mailbox Mailbox

	// Seat is the value of $MW_SEAT, seat or seat@host, or empty when it is
	// unset. Send refuses without it; Inbox and Read need it unless they are
	// given another mailbox to act as.
	Seat string

	// Out is where the inbox and a message are printed. A nil Out prints nothing.
	Out io.Writer
}

// Send sends a message from this seat to a mailbox and reports the id it was
// given. It refuses, naming $MW_SEAT, when there is no seat to sign it, and
// before anything is written. A message with no body is stored as NoBody.
func (m Mail) Send(ctx context.Context, to, subject, body string) (string, error) {
	from, err := m.signer()
	if err != nil {
		return "", err
	}
	to = mailboxName(to)
	if to == "" {
		return "", fmt.Errorf("mail needs a recipient: a seat such as mayor, builder or governor")
	}
	if strings.TrimSpace(subject) == "" {
		return "", fmt.Errorf("mail needs a subject")
	}
	if strings.TrimSpace(body) == "" {
		body = NoBody
	}

	id, err := m.Mailbox.Send(ctx, NewMessage{From: from, To: to, Subject: subject, Body: body})
	if err != nil {
		return "", err
	}
	m.printf("%s -> %s\n", id, to)
	return id, nil
}

// Reply answers a message: it goes to whoever sent the original, is titled
// "Re: " and the original's subject, and is linked to the original, which is
// left as unread as it was. It refuses, naming $MW_SEAT, when there is no seat
// to sign it, and when the id is not a message, or a message that says nothing
// of who sent it — a reply with nowhere to go — and writes nothing in either
// case. A reply with no body is stored as NoBody.
func (m Mail) Reply(ctx context.Context, id, body string) (string, error) {
	from, err := m.signer()
	if err != nil {
		return "", err
	}
	original, err := m.Mailbox.Get(ctx, id)
	if err != nil {
		return "", err
	}
	to := mailboxName(original.From)
	if to == "" {
		return "", fmt.Errorf("%s says nothing of who sent it, so a reply has nowhere to go: send the answer with mail send", id)
	}
	if strings.TrimSpace(body) == "" {
		body = NoBody
	}

	replyID, err := m.Mailbox.Send(ctx, NewMessage{From: from, To: to, Subject: "Re: " + original.Subject, Body: body, ReplyTo: id})
	if err != nil {
		return "", err
	}
	m.printf("%s -> %s\n", replyID, to)
	return replyID, nil
}

// Inbox prints the unread mail of a mailbox, oldest first, one line each: the
// id, who it is from, when it was sent and its subject. as names another
// seat's mailbox; empty means this seat's own. An inbox with nothing in it says
// whose it is.
func (m Mail) Inbox(ctx context.Context, as string) ([]Message, error) {
	who, err := m.actingAs(as, "whose inbox to list")
	if err != nil {
		return nil, err
	}
	unread, err := m.Mailbox.Inbox(ctx, who)
	if err != nil {
		return nil, err
	}
	if len(unread) == 0 {
		m.printf("no unread mail for %s\n", who)
		return unread, nil
	}
	for _, message := range unread {
		m.printf("%s  from %s  %s  %s\n", message.ID, orUnknown(message.From), sentMinute(message.Sent), message.Subject)
	}
	return unread, nil
}

// Read prints one message whole — from, to, date, subject and body — and marks
// it read. as names the seat reading it; empty means this seat.
func (m Mail) Read(ctx context.Context, id, as string) (Message, error) {
	reader, err := m.actingAs(as, "who is reading")
	if err != nil {
		return Message{}, err
	}
	message, err := m.Mailbox.Read(ctx, id, reader)
	if err != nil {
		return Message{}, err
	}
	m.printf("From: %s\nTo: %s\nDate: %s\nSubject: %s\n\n%s\n",
		orUnknown(message.From), orUnknown(message.To), sentInFull(message.Sent), message.Subject, message.Body)
	return message, nil
}

// signer is the seat that signs what this session sends: $MW_SEAT, and never a
// default of anyone's.
func (m Mail) signer() (string, error) {
	from := strings.TrimSpace(m.Seat)
	if from == "" {
		return "", fmt.Errorf("mail is signed by the seat that sends it, and %s is not set: "+
			"set it to your seat (seat or seat@host); mail is never sent as anyone by default", SeatEnv)
	}
	return from, nil
}

// actingAs is the mailbox a seat works in: the one it was told to with --as, or
// else its own from $MW_SEAT, and never a default of anyone's. what says what
// was being asked, for the refusal.
func (m Mail) actingAs(as, what string) (string, error) {
	if who := mailboxName(as); who != "" {
		return who, nil
	}
	if seat := strings.TrimSpace(m.Seat); seat != "" {
		return seat, nil
	}
	return "", fmt.Errorf("%s is not set, so there is no telling %s: set it to your seat, or name a mailbox with --as", SeatEnv, what)
}

// orUnknown is a name as a message prints it: mail that says nothing of who it
// is from or to, as a bead made by hand may not, shows a question mark.
func orUnknown(name string) string {
	if name == "" {
		return "?"
	}
	return name
}

// mailboxName is a mailbox as it is stored: trimmed, and without the trailing
// slash a seat's directory name carries when it is pasted in.
func mailboxName(name string) string {
	return strings.TrimRight(strings.TrimSpace(name), "/")
}

// sentMinute is when a message was sent as an inbox line shows it, to the
// minute; empty when the mailbox did not say.
func sentMinute(sent time.Time) string {
	if sent.IsZero() {
		return ""
	}
	return sent.UTC().Format("2006-01-02T15:04")
}

// sentInFull is when a message was sent as a read message shows it.
func sentInFull(sent time.Time) string {
	if sent.IsZero() {
		return ""
	}
	return sent.UTC().Format(time.RFC3339)
}

func (m Mail) printf(format string, args ...any) {
	if m.Out != nil {
		fmt.Fprintf(m.Out, format, args...)
	}
}
