package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

type verifyRig struct {
	beads   *apptest.FakeVerifyBeads
	grinder *apptest.FakeGrinder
	mail    *apptest.FakeMailbox
	inbox   string
	runs    string
	out     bytes.Buffer
}

func newVerifyRig(t *testing.T, answer string) *verifyRig {
	t.Helper()
	r := &verifyRig{
		beads: &apptest.FakeVerifyBeads{
			Work: application.WorkInHand{Ready: []application.StoryDetail{
				verifyDemo("d-1", "e-1", "Open the app and look for the green tick."),
				verifyDemo("d-2", "e-2", "Open the list and see the blue badge."),
			}},
			Closed:   []application.ClosedStory{verifyClosed("s-1", "e-9", "landed on main", 24*time.Hour)},
			Comments: map[string][]application.Comment{"s-1": verifyComment(closingWithCheck)},
			Now:      verifyNow,
		},
		grinder: &apptest.FakeGrinder{Result: application.SessionResult{Answer: json.RawMessage(answer)}},
		mail:    apptest.NewFakeMailbox(),
		inbox:   t.TempDir(),
		runs:    t.TempDir(),
	}
	for name, age := range map[string]time.Duration{"direct-aaa.txt": time.Hour, "direct-bbb.jpg": 2 * time.Hour} {
		path := filepath.Join(r.inbox, name)
		if err := os.WriteFile(path, []byte("evidence "+name), 0o600); err != nil {
			t.Fatal(err)
		}
		at := verifyNow.Add(-age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *verifyRig) match() application.VerifyMatch {
	return application.VerifyMatch{
		Beads: r.beads, Grinder: r.grinder, Mailbox: r.mail,
		Settings: application.VerifySettings{Model: "haiku", Effort: "medium", Confidence: 0.8},
		InboxDir: r.inbox, RunDir: r.runs, Host: "laptop", Now: func() time.Time { return verifyNow }, Out: &r.out,
	}
}

const verifyTwoAnswers = `{"matches":[
 {"bead":"d-1","txid":"direct:aaa","shows":"the green tick after Save","confidence":0.9},
 {"bead":"d-2","txid":"direct:bbb","shows":"a blue blob, maybe","confidence":0.5}]}`

func TestVerifyMatchProposesOnlyTheMatchesOverTheConfidence(t *testing.T) {
	r := newVerifyRig(t, verifyTwoAnswers)
	result, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 1 || r.beads.Written[0].ID != "d-1" {
		t.Fatalf("expected one comment on d-1, got %+v", r.beads.Written)
	}
	text := r.beads.Written[0].Text
	if firstWordOf(text) != "PROPOSED" || !strings.Contains(text, "direct:aaa") || !strings.Contains(text, "the green tick after Save") ||
		!strings.Contains(text, "confidence 0.90") || !strings.Contains(text, "haiku") || !strings.Contains(text, result.Run) {
		t.Fatalf("the proposal is not in the form: %q", text)
	}
	if !strings.HasPrefix(text, "PROPOSED VERIFIED by the Governor's use, from direct:aaa (2026-10-21 17:00Z): ") {
		t.Fatalf("the proposal opens wrongly: %q", text)
	}
	if got := r.mail.Writes(); got != 1 {
		t.Fatalf("expected one mail, got %d writes", got)
	}
	inbox, _ := r.mail.Inbox(context.Background(), "mayor")
	if len(inbox) != 1 || inbox[0].Subject != "Proposed: 1 verified by his use" ||
		!strings.Contains(inbox[0].Body, "mw verify confirm d-1 --txid direct:aaa") || strings.Contains(inbox[0].Body, "d-2") {
		t.Fatalf("the mail: %+v", inbox)
	}
	raw, err := os.ReadFile(filepath.Join(r.runs, result.Run+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if record["model"] != "haiku" || record["effort"] != "medium" || record["tokens"] == nil || len(record["candidates"].([]any)) != 3 || len(record["matches"].([]any)) != 2 {
		t.Fatalf("the run record: %s", raw)
	}
	if !strings.Contains(string(raw), "a blue blob, maybe") {
		t.Fatalf("the match under the confidence is in the run's log: %s", raw)
	}
}

func firstWordOf(text string) string {
	if fields := strings.Fields(text); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

func TestVerifyMatchProposesNothingTwiceForTheSameTxid(t *testing.T) {
	r := newVerifyRig(t, verifyTwoAnswers)
	for range 2 {
		if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour}); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.beads.Written) != 1 {
		t.Fatalf("expected the one proposal written once, got %+v", r.beads.Written)
	}
	if got := r.mail.Writes(); got != 1 {
		t.Fatalf("a run that proposes nothing new mails nothing, got %d mails", got)
	}
}

func TestVerifyMatchNeverWritesVerifiedAsTheFirstWordOrCloses(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[{"bead":"s-1","txid":"direct:aaa","shows":"VERIFIED already","confidence":1}]}`)
	if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	for _, w := range r.beads.Written {
		if firstWordOf(w.Text) != "PROPOSED" {
			t.Fatalf("a comment that is not a proposal: %q", w.Text)
		}
	}
}

func TestVerifyMatchDropsAnAnswerNamingAnUnknownBeadOrTxid(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[
	 {"bead":"ghost","txid":"direct:aaa","shows":"x","confidence":0.99},
	 {"bead":"d-1","txid":"direct:ghost","shows":"y","confidence":0.99}]}`)
	if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 0 || r.mail.Writes() != 0 {
		t.Fatalf("expected nothing proposed, got %+v and %d mails", r.beads.Written, r.mail.Writes())
	}
}

func TestVerifyMatchDryRunCallsNoAgentAndWritesNothing(t *testing.T) {
	r := newVerifyRig(t, verifyTwoAnswers)
	if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if len(r.grinder.Calls()) != 0 || len(r.beads.Written) != 0 || r.mail.Writes() != 0 {
		t.Fatalf("a dry run starts, writes and mails nothing")
	}
	said := r.out.String()
	for _, want := range []string{"d-1", "d-2", "s-1", "direct:aaa", "direct:bbb", "bytes"} {
		if !strings.Contains(said, want) {
			t.Errorf("the dry run does not say %q:\n%s", want, said)
		}
	}
	if entries, _ := os.ReadDir(r.runs); len(entries) != 0 {
		t.Fatalf("a dry run keeps no run record")
	}
}

func TestVerifyMatchAsksOneBoundedAgentCallOverADirOfFiles(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[]}`)
	if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	calls := r.grinder.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected one grind, got %d", len(calls))
	}
	call := calls[0].Call
	if call.Model != "haiku" || call.Effort != "medium" || call.Timeout != 10*time.Minute || call.MaxTurns <= 0 || call.Schema == "" || call.System == "" {
		t.Fatalf("the call is not bounded: %+v", call)
	}
	names := []string{}
	for name := range calls[0].Files {
		names = append(names, name)
	}
	joined := strings.Join(names, " ")
	for _, want := range []string{"candidate-d-1.txt", "candidate-d-2.txt", "candidate-s-1.txt", "direct-aaa.txt", "direct-bbb.jpg"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the dir lacks %s: %v", want, names)
		}
	}
	if r.mail.Writes() != 0 {
		t.Fatalf("no mail on zero matches")
	}
}

func TestVerifyMatchRefusedAnswerWritesNothingButKeepsTheRecord(t *testing.T) {
	r := newVerifyRig(t, ``)
	r.grinder.Result = application.SessionResult{IsError: true, Subtype: "error_max_turns"}
	result, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour})
	if err == nil || len(r.beads.Written) != 0 {
		t.Fatalf("expected an error and no comment, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(r.runs, result.Run+".json")); statErr != nil {
		t.Fatalf("the record of a refused run is kept: %v", statErr)
	}
}

func TestVerifyMatchWithNothingToMatchStartsNoAgent(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[]}`)
	r.inbox = t.TempDir()
	if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if len(r.grinder.Calls()) != 0 {
		t.Fatalf("no evidence, no agent")
	}
}

func TestVerifyMatchReadsOnlyTheNewestLandingsAndEveryDemo(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[]}`)
	for n := range 130 {
		id := fmt.Sprintf("s-%03d", n+10)
		r.beads.Closed = append(r.beads.Closed, verifyClosed(id, "e-9", "landed on main", time.Duration(n+1)*time.Minute))
		r.beads.Comments[id] = verifyComment(closingWithCheck)
	}
	if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	files := r.grinder.Calls()[0].Files
	landings := 0
	for name := range files {
		if strings.HasPrefix(name, "candidate-s-") {
			landings++
		}
	}
	if landings != 120 {
		t.Fatalf("expected the newest 120 landings, got %d", landings)
	}
	if _, ok := files["candidate-d-1.txt"]; !ok {
		t.Fatalf("every open demo is read")
	}
	if _, ok := files["candidate-s-010.txt"]; !ok {
		t.Fatalf("the newest landing is read")
	}
}
