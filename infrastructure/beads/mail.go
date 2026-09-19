package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Jonathan-A-White/millwright/application"
)

// Gateway also carries the factory's mail, which is beads.
var _ application.Mailbox = (*Gateway)(nil)

// TypeMail is beads' name for the type of bead a message is filed as. It is a
// custom type the vault declares (`types.custom`), and not beads' own message
// type: bd makes a message ephemeral, and an ephemeral bead never syncs, so
// mail filed as one would never leave the host that sent it.
const TypeMail = "mail"

// LabelMail is the label every message carries, so that mail can be found
// whatever its type.
const LabelMail = "mail"

// mailPriority is the priority a message is filed at: mail is nobody's urgent
// work, and beads' own default is the same.
const mailPriority = application.DefaultPriority

// dependencyRelated is the kind of dependency a reply is linked to the message
// it answers by: a link that waits on nothing, which is how bin/mw-mail linked
// them (`--deps related:<id>`).
const dependencyRelated = "related"

// Send implements application.Mailbox. A message is a bead of type mail:
// assigned to the recipient, its subject the title, its body the description,
// and who it is from and to in the metadata. It is open until it is read, and
// versioned, which is what lets it travel to the other host on a sync. A reply
// is also linked to the message it answers. This is the shape the stand-in
// bin/mw-mail wrote, so mail already sent stays readable.
func (g *Gateway) Send(ctx context.Context, message application.NewMessage) (string, error) {
	metadata, err := metadataJSON(map[string]string{"from": message.From, "to": message.To})
	if err != nil {
		return "", err
	}
	args := []string{
		"create", message.Subject,
		"--type", TypeMail,
		"--priority", strconv.Itoa(mailPriority),
		"--assignee", message.To,
		"--label", LabelMail,
		"--description", message.Body,
		"--storage-class", "versioned",
		"--metadata", metadata,
	}
	if message.ReplyTo != "" {
		args = append(args, "--deps", dependencyRelated+":"+message.ReplyTo)
	}
	return g.created(ctx, "the message", args)
}

// Get implements application.Mailbox: the message as it is, read or not. A
// bead that is not mail is refused, as Read refuses it.
func (g *Gateway) Get(ctx context.Context, id string) (application.Message, error) {
	mail, err := g.mailBead(ctx, id)
	if err != nil {
		return application.Message{}, err
	}
	return mail.message(), nil
}

// mailBead is the bead an id names, when it is mail.
func (g *Gateway) mailBead(ctx context.Context, id string) (bead, error) {
	found, err := g.showOne(ctx, id)
	if err != nil {
		return bead{}, err
	}
	if found.Type != TypeMail {
		return bead{}, fmt.Errorf("%s is a %s, not mail", id, describeType(found.Type))
	}
	return found, nil
}

// Inbox implements application.Mailbox: the open mail assigned to a mailbox,
// oldest first. bd's default limit is asked to step aside, because an inbox
// that showed the first fifty of a hundred would lose the rest without a word.
func (g *Gateway) Inbox(ctx context.Context, mailbox string) ([]application.Message, error) {
	out, err := g.call(ctx, "list", "--type", TypeMail, "--assignee", mailbox, "--status", StatusOpen, "--limit", "0", "--json")
	if err != nil {
		return nil, err
	}
	found, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading the mail of %s: %w", mailbox, err)
	}
	unread := make([]application.Message, 0, len(found))
	for _, mail := range inFiledOrder(found) {
		unread = append(unread, mail.message())
	}
	return unread, nil
}

// Read implements application.Mailbox. Reading is closing the bead, signed by
// the reader; a message already closed is reported and left as it is. A bead
// that is not mail is refused before anything is closed, since closing a story
// because someone asked to read it would be a poor way to find that out.
func (g *Gateway) Read(ctx context.Context, id, reader string) (application.Message, error) {
	mail, err := g.mailBead(ctx, id)
	if err != nil {
		return application.Message{}, err
	}
	if mail.Status == StatusOpen {
		args := []string{"close", id, "--reason", "read by " + reader}
		if reader != "" {
			// The last --actor given is the one bd uses: the message is closed
			// by the seat that read it, not by mw on its behalf.
			args = append([]string{"--actor", reader}, args...)
		}
		if _, err := g.call(ctx, args...); err != nil {
			return application.Message{}, err
		}
	}
	return mail.message(), nil
}

// describeType is what to call a bead's type in a sentence: a bead bd printed no
// type for is a bead.
func describeType(kind string) string {
	if kind == "" {
		return "bead"
	}
	return kind
}

// message is this bead as a message. A message the stand-in wrote carries who it
// is from and to in its metadata; one without them is still readable, from
// no one to whoever it was assigned to.
func (b bead) message() application.Message {
	from, _ := b.Metadata["from"].(string)
	to, _ := b.Metadata["to"].(string)
	if to == "" {
		to = b.Assignee
	}
	return application.Message{
		ID:      b.ID,
		From:    from,
		To:      to,
		Subject: b.Title,
		Body:    b.Description,
		Sent:    b.created(),
		ReplyTo: b.answers(),
	}
}

// answers is the id of the message this bead is a reply to: the bead it has a
// related dependency on. bd prints that as a whole linked bead on `bd show`
// and as an edge on `bd list`; an edge that runs the other way, from a reply to
// this bead, is an answer to it, not what it answers.
func (b bead) answers() string {
	for _, raw := range b.Dependencies {
		var e edge
		if json.Unmarshal(raw, &e) == nil && e.Issue != "" && e.DependsOn != "" {
			if e.Issue == b.ID && e.Kind == dependencyRelated {
				return e.DependsOn
			}
			continue
		}
		var link linked
		if json.Unmarshal(raw, &link) == nil && link.ID != "" && link.Kind == dependencyRelated {
			return link.ID
		}
	}
	return ""
}
