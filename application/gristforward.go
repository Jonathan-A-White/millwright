package application

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// gristForwardSubjectLen is how many characters of the text a forwarded
// grist's mail subject carries.
const gristForwardSubjectLen = 80

// gristForwardAnswer is what the app is answered when its grist was passed on.
const gristForwardAnswer = `{"status":"sent"}`

// forward passes a grist whose grind says forward on to the Mayor: it opens the
// photos as any grind would, keeps them under the mill's state directory, sends
// one mail to the seat, and settles the grist answered {"status":"sent"}. No
// session runs. A grist that cannot be mailed is failed, so the app may send it
// again; w.settle keeps the first outcome, so an attachment that could not be
// opened has already settled it.
func (g GristGrind) forward(ctx context.Context, w *gristWork, privKey string) {
	dir, err := os.MkdirTemp(g.TempDir, "mw-grist-")
	if err != nil {
		w.settle(GristFailed, GristReasonNotSent)
		return
	}
	defer os.RemoveAll(dir)
	g.openPhotos(ctx, w, privKey, dir)
	if w.status != "" {
		return
	}
	if g.Mailbox == nil || g.Forwards == nil {
		w.settle(GristFailed, GristReasonNotSent)
		return
	}
	var pictures []GristRunAttachment
	for _, clip := range w.clips {
		pictures = append(pictures, GristRunAttachment{Ext: posternAttachmentExtension(clip.mime), Data: clip.data})
	}
	paths, err := g.Forwards.Keep(ctx, w.record.Txid, pictures)
	if err != nil {
		w.settle(GristFailed, GristReasonNotSent)
		return
	}
	text := forwardText(w.plain.Input)
	_, err = g.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, g.Host),
		To:      w.grind.Forward,
		Subject: forwardSubject(w.plain.Grist.App, text),
		Body:    forwardBody(w, text, paths),
	})
	if err != nil {
		w.settle(GristFailed, GristReasonNotSent)
		return
	}
	w.answer = json.RawMessage(gristForwardAnswer)
	w.settle(GristAnswered, "")
	w.answered = g.now()
	w.harnessStarted = w.answered
	g.keepForwardRun(ctx, w)
}

// forwardText is the words of a forwarded request: its "text" when it is an
// object with one, else the request itself.
func forwardText(input json.RawMessage) string {
	var asObject struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(input, &asObject) == nil && asObject.Text != nil {
		return *asObject.Text
	}
	var asString string
	if json.Unmarshal(input, &asString) == nil {
		return asString
	}
	if len(input) == 0 || string(input) == "null" {
		return ""
	}
	return string(input)
}

// forwardSubject is "Feedback from <app>: " and the first 80 characters of the
// text, its whitespace made single spaces.
func forwardSubject(app, text string) string {
	line := strings.Join(strings.Fields(text), " ")
	if line == "" {
		line = "(no text)"
	}
	if runes := []rune(line); len(runes) > gristForwardSubjectLen {
		line = string(runes[:gristForwardSubjectLen])
	}
	return fmt.Sprintf("Feedback from %s: %s", app, line)
}

// forwardBody is the mail: the full text, then who sent it and where the
// pictures are kept.
func forwardBody(w *gristWork, text string, paths []string) string {
	var b strings.Builder
	if strings.TrimSpace(text) == "" {
		text = "(no text)"
	}
	b.WriteString(text)
	name := w.plain.Grist
	fmt.Fprintf(&b, "\n\n--\nFrom the key %s (fingerprint %s)\nApp %s, kind %s, version %s\nGrist %s\n",
		w.to, w.sender, name.App, name.Kind, name.V, w.record.Txid)
	if len(paths) == 0 {
		b.WriteString("Pictures: none\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Pictures: %d, kept on the mill's host\n", len(paths))
	for _, p := range paths {
		b.WriteString(p + "\n")
	}
	return b.String()
}

// keepForwardRun keeps a forwarded grist's record as a run of the kind
// "forward": no model, no tokens, the pictures already kept by Forwards.
func (g GristGrind) keepForwardRun(ctx context.Context, w *gristWork) {
	if g.Runs == nil {
		return
	}
	run := GristRun{
		Txid:   w.record.Txid,
		Input:  w.plain.Input,
		Answer: GristRunAnswer{Status: w.status, Answer: w.answer},
		Timing: GristRunTiming{
			Txid: w.record.Txid, App: w.plain.Grist.App, Kind: GristForwardKind,
			Sent: sentTime(w.record.Ts), Received: w.received.UTC(),
			HarnessStarted: w.harnessStarted.UTC(), Answered: w.answered.UTC(),
			Seconds: w.answered.Sub(w.received).Seconds(),
		}.withWaits(),
	}
	if err := g.Runs.Keep(ctx, run); err != nil {
		w.notes = append(w.notes, fmt.Sprintf("the record of the run of %s could not be kept: %v", shortTxid(w.record.Txid), err))
	}
}

// GristForwardKind is the kind mw grist runs and stats count a forwarded grist
// under.
const GristForwardKind = "forward"
