//go:build beads_integration

package beads_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// shownMail is the fields of a bead as bd shows it that mail is made of.
type shownMail struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	Type        string            `json:"issue_type"`
	Assignee    string            `json:"assignee"`
	Labels      []string          `json:"labels"`
	Metadata    map[string]string `json:"metadata"`
}

// showMail reads a bead straight from bd, not through the Gateway, so that what
// the Gateway wrote is judged by what is on disk.
func showMail(t *testing.T, vault, id string) shownMail {
	t.Helper()
	var shown []shownMail
	if err := json.Unmarshal([]byte(bdRun(t, vault, beads.Program, "show", id, "--json")), &shown); err != nil || len(shown) != 1 {
		t.Fatalf("reading %s back from bd: %v (%d beads)", id, err, len(shown))
	}
	return shown[0]
}

func TestGatewaySendsMailAsTheStandInDidAndReadsMailTheStandInSent(t *testing.T) {
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// The vault declares the custom type, as the factory's own does.
	bdRun(t, vault, beads.Program, "config", "set", "types.custom", "mail")
	gateway := beads.New(vault)

	// Mail sent by the Gateway is a bead of type mail, shaped as bin/mw-mail
	// shapes it: assigned to the recipient, subject the title, body the
	// description, from and to in the metadata, labelled mail, open.
	sent, err := gateway.Send(ctx, application.NewMessage{
		From: "builder@laptop", To: "mayor", Subject: "Ready for review", Body: "The branch is up.",
	})
	if err != nil {
		t.Fatalf("sending mail: %v", err)
	}
	bead := showMail(t, vault, sent)
	if bead.Type != "mail" || bead.Assignee != "mayor" || bead.Title != "Ready for review" ||
		bead.Description != "The branch is up." || bead.Status != "open" {
		t.Errorf("expected an open bead of type mail for mayor titled %q, got %+v", "Ready for review", bead)
	}
	if bead.Metadata["from"] != "builder@laptop" || bead.Metadata["to"] != "mayor" {
		t.Errorf("expected the metadata to say from builder@laptop to mayor, got %v", bead.Metadata)
	}
	if !strings.Contains(strings.Join(bead.Labels, ","), "mail") {
		t.Errorf("expected the label mail, got %q", bead.Labels)
	}

	// Mail created the stand-in's way, with bd directly, is in the inbox too.
	standIn := bdRun(t, vault, beads.Program, "create", "From the stand-in", "-t", "mail", "-p", "2",
		"-a", "mayor", "-l", "mail", "-d", "Sent before mw mail.", "--storage-class", "versioned",
		"--metadata", `{"from": "governor", "to": "mayor"}`, "--silent")
	bdRun(t, vault, beads.Program, "create", "For someone else", "-t", "mail", "-a", "builder", "-l", "mail",
		"-d", "Not the Mayor's.", "--metadata", `{"from": "mayor", "to": "builder"}`, "--silent")

	unread, err := gateway.Inbox(ctx, "mayor")
	if err != nil {
		t.Fatalf("listing the inbox of mayor: %v", err)
	}
	// bd stamps whole seconds, so two messages sent in one go tie: which of them
	// comes first is not the case's to say.
	if len(unread) != 2 || !(unread[0].ID == sent && unread[1].ID == standIn || unread[0].ID == standIn && unread[1].ID == sent) {
		t.Fatalf("expected the inbox of mayor to hold %s and %s, got %+v", sent, standIn, unread)
	}
	got := unread[0]
	if got.ID != standIn {
		got = unread[1]
	}
	if got.From != "governor" || got.To != "mayor" || got.Subject != "From the stand-in" ||
		got.Body != "Sent before mw mail." || got.Sent.IsZero() {
		t.Errorf("expected the stand-in's mail to read whole, got %+v", got)
	}

	// Reading it closes it, and it leaves the inbox; reading it again is harmless.
	read, err := gateway.Read(ctx, standIn, "mayor")
	if err != nil {
		t.Fatalf("reading %s: %v", standIn, err)
	}
	if read.From != "governor" || read.Body != "Sent before mw mail." {
		t.Errorf("expected the message read to be the one sent, got %+v", read)
	}
	if got := showMail(t, vault, standIn).Status; got != "closed" {
		t.Errorf("expected %s to be closed once read, it is %s", standIn, got)
	}
	if _, err := gateway.Read(ctx, standIn, "mayor"); err != nil {
		t.Errorf("reading %s a second time: %v", standIn, err)
	}
	unread, err = gateway.Inbox(ctx, "mayor")
	if err != nil || len(unread) != 1 || unread[0].ID != sent {
		t.Errorf("expected only %s left unread for mayor, got %+v: %v", sent, unread, err)
	}
	if unread, err := gateway.Inbox(ctx, "nobody"); err != nil || len(unread) != 0 {
		t.Errorf("expected no mail for nobody, got %+v: %v", unread, err)
	}

	// A bead that is not mail is refused, and left open.
	story := bdRun(t, vault, beads.Program, "create", "Not mail at all", "-t", "task", "--silent")
	if _, err := gateway.Read(ctx, story, "mayor"); err == nil || !strings.Contains(err.Error(), story) {
		t.Errorf("expected reading %s to be refused, naming it, got %v", story, err)
	}
	if got := showMail(t, vault, story).Status; got != "open" {
		t.Errorf("expected %s to stay open, it is %s", story, got)
	}
}

func TestGatewayLinksAReplyToTheMessageItAnswersAsTheStandInDid(t *testing.T) {
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	bdRun(t, vault, beads.Program, "config", "set", "types.custom", "mail")
	gateway := beads.New(vault)

	original, err := gateway.Send(ctx, application.NewMessage{
		From: "builder@laptop", To: "mayor", Subject: "Ready for review", Body: "The branch is up.",
	})
	if err != nil {
		t.Fatalf("sending mail: %v", err)
	}
	reply, err := gateway.Send(ctx, application.NewMessage{
		From: "mayor", To: "builder@laptop", Subject: "Re: Ready for review", Body: "Merged.", ReplyTo: original,
	})
	if err != nil {
		t.Fatalf("sending the reply: %v", err)
	}

	// The link is a related dependency of the reply on the original, which is
	// what `--deps related:<id>` made of it in bin/mw-mail.
	var shown []struct {
		Dependencies []struct {
			ID   string `json:"id"`
			Kind string `json:"dependency_type"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(bdRun(t, vault, beads.Program, "show", reply, "--json")), &shown); err != nil || len(shown) != 1 ||
		len(shown[0].Dependencies) != 1 || shown[0].Dependencies[0].ID != original || shown[0].Dependencies[0].Kind != "related" {
		t.Errorf("expected %s to depend on %s by a related link, got %+v (%v)", reply, original, shown, err)
	}

	// Get reports the reply as linked, and the original as answering nothing;
	// it reads without closing anything.
	got, err := gateway.Get(ctx, reply)
	if err != nil || got.ReplyTo != original || got.From != "mayor" || got.To != "builder@laptop" {
		t.Errorf("expected the reply to say it answers %s, from mayor to builder@laptop, got %+v: %v", original, got, err)
	}
	got, err = gateway.Get(ctx, original)
	if err != nil || got.ReplyTo != "" {
		t.Errorf("expected the original to answer nothing, got %+v: %v", got, err)
	}
	if status := showMail(t, vault, original).Status; status != "open" {
		t.Errorf("expected %s to stay open after Get, it is %s", original, status)
	}

	// The inbox lists the reply as linked too, from the edge bd lists.
	unread, err := gateway.Inbox(ctx, "builder@laptop")
	if err != nil || len(unread) != 1 || unread[0].ReplyTo != original {
		t.Errorf("expected the reply in the inbox of builder@laptop, answering %s, got %+v: %v", original, unread, err)
	}

	// A bead that is not mail is refused by Get, as it is by Read.
	story := bdRun(t, vault, beads.Program, "create", "Not mail at all", "-t", "task", "--silent")
	if _, err := gateway.Get(ctx, story); err == nil || !strings.Contains(err.Error(), story) {
		t.Errorf("expected getting %s to be refused, naming it, got %v", story, err)
	}
}
