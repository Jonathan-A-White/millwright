package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
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
	in := parseForwardInput(w.plain.Input)
	title := in.text
	if strings.TrimSpace(in.summary) != "" {
		title = in.summary
	}
	_, err = g.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, g.Host),
		To:      w.grind.Forward,
		Subject: forwardSubject(w.plain.Grist.App, title),
		Body:    forwardBody(w, in, paths),
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

// forwardedInput is a forwarded request taken apart: its words, its summary,
// and the other fields of an object input as '<key>: <value>' lines.
type forwardedInput struct {
	text    string
	summary string
	lines   []string
}

// parseForwardInput takes the request apart. An object's "text" is the words
// and its "summary" (when a string) the subject; every other top-level field is
// a line, the summary first and the rest sorted by key, a string as it is and
// anything else as compact JSON. Any other input is its own words.
func parseForwardInput(input json.RawMessage) forwardedInput {
	var fields map[string]json.RawMessage
	if json.Unmarshal(input, &fields) != nil || fields == nil {
		return forwardedInput{text: forwardText(input)}
	}
	var in forwardedInput
	keys := make([]string, 0, len(fields))
	for key, raw := range fields {
		if key == "text" && json.Unmarshal(raw, &in.text) == nil {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if _, ok := fields["summary"]; ok {
		keys = append([]string{"summary"}, withoutKey(keys, "summary")...)
	}
	for _, key := range keys {
		raw := fields[key]
		var value string
		if json.Unmarshal(raw, &value) != nil {
			var compact bytes.Buffer
			if json.Compact(&compact, raw) != nil {
				compact.Reset()
				compact.Write(raw)
			}
			value = compact.String()
		} else if key == "summary" {
			in.summary = value
		}
		line := key + ":"
		if value != "" {
			line += " " + value
		}
		in.lines = append(in.lines, line)
	}
	return in
}

func withoutKey(keys []string, drop string) []string {
	var kept []string
	for _, k := range keys {
		if k != drop {
			kept = append(kept, k)
		}
	}
	return kept
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

// forwardBody is the mail: the full text, the other fields of the request one
// to a line, then who sent it and where the pictures are kept.
func forwardBody(w *gristWork, in forwardedInput, paths []string) string {
	var b strings.Builder
	text := in.text
	if strings.TrimSpace(text) == "" {
		text = "(no text)"
	}
	b.WriteString(text)
	for _, line := range in.lines {
		b.WriteString("\n" + line)
	}
	name := w.plain.Grist
	fmt.Fprintf(&b, "\n\n--\nFrom the key %s (fingerprint %s)\nApp %s, kind %s, version %s\nGrist %s\n",
		w.to, w.sender, name.App, name.Kind, name.V, w.record.Txid)
	if len(paths) == 0 {
		b.WriteString("Pictures: none\n")
		return forwardWithRequestLine(b.String(), w.record.Txid)
	}
	fmt.Fprintf(&b, "Pictures: %d, kept on the mill's host\n", len(paths))
	for _, p := range paths {
		b.WriteString(p + "\n")
	}
	return forwardWithRequestLine(b.String(), w.record.Txid)
}

// forwardWithRequestLine ends the mail with the command that links the request
// to the bead the Mayor files for it, ready but for the bead.
func forwardWithRequestLine(body, txid string) string {
	return body + "\n" + GristRequestCommand("<bead>", txid) + "\n"
}

// keepForwardRun keeps a forwarded grist's record as a run of the kind
// "forward": no model, no tokens, the pictures already kept by Forwards.
func (g GristGrind) keepForwardRun(ctx context.Context, w *gristWork) {
	if g.Runs == nil {
		return
	}
	run := GristRun{
		Txid:   w.record.Txid,
		Sender: w.to,
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
