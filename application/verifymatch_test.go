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
	"github.com/Jonathan-A-White/millwright/domain"
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

// A picture sent before a landing cannot show that landing met (mw-gq6.742):
// mw-9q8wp1.9 was proposed from the very ask that made it.
func TestVerifyMatchDropsEvidenceOlderThanTheLandingAndCountsIt(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[
	 {"bead":"s-late","txid":"direct:bbb","shows":"the picture of the new reading","confidence":0.82},
	 {"bead":"s-late","txid":"direct:aaa","shows":"he used the new reading","confidence":0.9}]}`)
	r.beads.Closed = append(r.beads.Closed, verifyClosed("s-late", "e-9", "landed on main", 90*time.Minute))
	r.beads.Comments["s-late"] = verifyComment(closingWithCheck)
	result, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 1 || !strings.Contains(r.beads.Written[0].Text, "direct:aaa") || strings.Contains(r.beads.Written[0].Text, "direct:bbb") {
		t.Fatalf("expected one proposal, from the evidence after the landing: %+v", r.beads.Written)
	}
	if len(result.Proposed) != 1 || result.Proposed[0].Txid != "direct:aaa" {
		t.Fatalf("proposed: %+v", result.Proposed)
	}
	raw, err := os.ReadFile(filepath.Join(r.runs, result.Run+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Dropped int `json:"dropped"`
		Matches []struct{ Txid, Outcome string }
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if record.Dropped != 1 {
		t.Fatalf("the run log should count one dropped, got %d: %s", record.Dropped, raw)
	}
	if record.Matches[0].Txid != "direct:bbb" || !strings.Contains(record.Matches[0].Outcome, "older than the landing") {
		t.Fatalf("the dropped match is not marked: %s", raw)
	}
	if !strings.Contains(r.out.String(), "1 dropped") {
		t.Fatalf("the run does not say it dropped one:\n%s", r.out.String())
	}
}

func TestVerifyMatchDropsEvidenceOlderThanTheNewestLandingOfADemosEpic(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[
	 {"bead":"d-1","txid":"direct:bbb","shows":"a picture of the screen","confidence":0.95},
	 {"bead":"d-1","txid":"direct:aaa","shows":"the green tick after Save","confidence":0.95}]}`)
	// d-1's epic e-1 has two landings: one long ago, one 90 minutes ago.
	r.beads.Closed = append(r.beads.Closed,
		verifyClosed("s-old", "e-1", "landed on main", 20*time.Hour), verifyClosed("s-new", "e-1", "landed on main", 90*time.Minute))
	if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 1 || !strings.Contains(r.beads.Written[0].Text, "direct:aaa") {
		t.Fatalf("expected only the evidence after the epic's newest landing to propose: %+v", r.beads.Written)
	}
}

func TestVerifyMatchGivesAPictureTheWordsHeSentWithIt(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[]}`)
	kept := `{"txid":"direct:bbb","text":"the reading should also say the book chapter and verse"}`
	if err := os.WriteFile(filepath.Join(r.inbox, "direct-bbb.message.json"), []byte(kept), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	call := r.grinder.Calls()[0]
	words, ok := call.Files["direct-bbb.words.txt"]
	if !ok || !strings.Contains(string(words), "the reading should also say the book chapter and verse") {
		t.Fatalf("the picture's own words are not in the dir: %v", call.Files)
	}
	if !strings.Contains(call.Call.Prompt, "direct-bbb.words.txt") {
		t.Fatalf("the prompt does not name the picture's words file:\n%s", call.Call.Prompt)
	}
	if !strings.Contains(call.Call.System, "the words he sent with a picture decide whether it asks for the change or shows it") {
		t.Fatalf("the system prompt does not say a picture's own words decide:\n%s", call.Call.System)
	}
}

func TestVerifyMatchSaysSoWhenNoWordsAreSavedWithAPicture(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[]}`)
	if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	words, ok := r.grinder.Calls()[0].Files["direct-bbb.words.txt"]
	if !ok || !strings.Contains(string(words), "No words") {
		t.Fatalf("a picture with no saved words still gets a words file that says so: %v", r.grinder.Calls()[0].Files)
	}
}

// A picture whose description does not name the story's screen is no match for
// it, whatever the agent says it shows (mw-gq6.1034, as mw-gq6.1008 for the sleep
// pass): a photo of a PowerShell error was proposed for a Reader story.
func TestVerifyMatchLeavesOutAPictureThatDoesNotShowTheStorysScreen(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[
	 {"bead":"d-reader","txid":"direct:bbb","shows":"a PowerShell error about the WSL distribution","confidence":0.95}]}`)
	r.beads.Work.Ready = append(r.beads.Work.Ready, verifyDemo("d-reader", "e-3", "Open the Reader screen and see the chapter heading."))
	result, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 0 || len(result.Proposed) != 0 {
		t.Fatalf("a picture of a PowerShell error is no proof of a Reader story: %+v", r.beads.Written)
	}
	if got := r.mail.Writes(); got != 0 {
		t.Fatalf("nothing proposed, nothing mailed, got %d mails", got)
	}
	raw, err := os.ReadFile(filepath.Join(r.runs, result.Run+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "does not name the screen") {
		t.Fatalf("the run's record does not say why the match was left out: %s", raw)
	}
}

func TestVerifyMatchProposesAPictureThatShowsTheStorysScreen(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[
	 {"bead":"d-reader","txid":"direct:bbb","shows":"the Reader screen with the chapter heading","confidence":0.95}]}`)
	r.beads.Work.Ready = append(r.beads.Work.Ready, verifyDemo("d-reader", "e-3", "Open the Reader screen and see the chapter heading."))
	if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 1 || r.beads.Written[0].ID != "d-reader" || !strings.Contains(r.beads.Written[0].Text, "direct:bbb") {
		t.Fatalf("a picture of the Reader screen is proposed as before: %+v", r.beads.Written)
	}
}

// What a message of his says is not held to a screen: only a picture is.
func TestVerifyMatchDoesNotHoldAMessageToAScreenName(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[
	 {"bead":"d-reader","txid":"direct:aaa","shows":"he says it works","confidence":0.95}]}`)
	r.beads.Work.Ready = append(r.beads.Work.Ready, verifyDemo("d-reader", "e-3", "Open the Reader screen and see the chapter heading."))
	if _, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 1 {
		t.Fatalf("a message of his is proposed as before: %+v", r.beads.Written)
	}
}

// --- standing question cards (mw-27f7myz.9) ---

// cardNote is a bead's postern.question note as the sender writes it.
func cardNote(txid string, asked time.Time, q string, options ...string) string {
	raw, _ := json.Marshal(map[string]any{"txid": txid, "options": options, "asked": asked.Format(time.RFC3339), "q": q})
	return string(raw)
}

// withCard puts an open bead with a standing card in the rig: asked ago before
// the clock.
func (r *verifyRig) withCard(bead string, ago time.Duration, options ...string) {
	if r.beads.Notes == nil {
		r.beads.Notes = map[string]string{}
	}
	if r.beads.Details == nil {
		r.beads.Details = map[string]application.StoryDetail{}
	}
	r.beads.Details[bead] = application.StoryDetail{Story: domain.Story{ID: bead, Title: "Bead " + bead}, Status: application.StatusOpen}
	r.beads.Notes[application.PosternQuestionKey(bead)] = cardNote("direct:card-"+bead, verifyNow.Add(-ago), "Which one for "+bead+"?", options...)
}

// saysIn keeps a message of his under txid, written ago before the clock in the
// thread, with the words.
func (r *verifyRig) saysIn(t *testing.T, txid, thread string, ago time.Duration, words string) {
	t.Helper()
	stem := strings.Replace(txid, ":", "-", 1)
	at := verifyNow.Add(-ago)
	kept, _ := json.Marshal(map[string]any{"txid": txid, "thread": thread, "ts": at, "text": words})
	for name, body := range map[string][]byte{stem + ".txt": []byte(words), stem + ".message.json": kept} {
		path := filepath.Join(r.inbox, name)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

func (r *verifyRig) cardMatch() application.VerifyMatch {
	m := r.match()
	m.Cards = r.beads
	return m
}

var cardRun = application.VerifyOptions{Since: 7 * 24 * time.Hour}

func TestVerifyMatchDryRunListsTheCardsWithALaterMessageAndNoOther(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[]}`)
	r.withCard("q-answered", 3*time.Hour, "A: Yes", "B: No")
	r.withCard("q-unheard", 30*time.Minute, "A: Yes", "B: No")
	r.withCard("q-elsewhere", 3*time.Hour, "A: Yes", "B: No")
	r.saysIn(t, "direct:ans", "q-answered", 90*time.Minute, "B, the second one")
	r.saysIn(t, "direct:before", "q-unheard", 2*time.Hour, "an old remark")
	r.saysIn(t, "direct:other", "q-someone-else", 90*time.Minute, "about another bead")
	dry := cardRun
	dry.DryRun = true
	if _, err := r.cardMatch().Run(context.Background(), dry); err != nil {
		t.Fatal(err)
	}
	said := r.out.String()
	if !strings.Contains(said, "q-answered") {
		t.Errorf("the card with a later message is not listed:\n%s", said)
	}
	for _, not := range []string{"q-unheard", "q-elsewhere"} {
		if strings.Contains(said, not) {
			t.Errorf("%s has no later message of his and is not a candidate:\n%s", not, said)
		}
	}
	if len(r.grinder.Calls()) != 0 || len(r.beads.Written) != 0 {
		t.Fatalf("a dry run calls and writes nothing")
	}
}

func TestVerifyMatchProposesClearingACardHeAnsweredInHisOwnWords(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[{"bead":"q-1","txid":"direct:ans","option":"B","shows":"he picks the second option","confidence":0.93}]}`)
	r.withCard("q-1", 3*time.Hour, "A: Keep it", "B: Drop it")
	r.saysIn(t, "direct:ans", "q-1", 90*time.Minute, "B, the second one")
	notes := map[string]string{}
	for k, v := range r.beads.Notes {
		notes[k] = v
	}
	result, err := r.cardMatch().Run(context.Background(), cardRun)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 1 || r.beads.Written[0].ID != "q-1" {
		t.Fatalf("expected one comment on q-1, got %+v", r.beads.Written)
	}
	text := r.beads.Written[0].Text
	for _, want := range []string{"direct:ans", "B: Drop it", `"B, the second one"`, "direct:card-q-1"} {
		if !strings.Contains(text, want) {
			t.Errorf("the proposal lacks %q: %s", want, text)
		}
	}
	if firstWordOf(text) != "PROPOSED" || strings.Contains(text, "VERIFIED") {
		t.Fatalf("the comment opens PROPOSED and is not a verification: %q", text)
	}
	inbox, _ := r.mail.Inbox(context.Background(), "mayor")
	if len(inbox) != 1 || !strings.Contains(inbox[0].Body, `mw postern clear q-1 --why "`) || !strings.Contains(inbox[0].Body, "B, the second one") {
		t.Fatalf("the mail: %+v", inbox)
	}
	if len(result.Proposed) != 1 || result.Proposed[0].Bead != "q-1" {
		t.Fatalf("proposed: %+v", result.Proposed)
	}
	if len(r.beads.Shut) != 0 || len(r.beads.Notes) != len(notes) || r.beads.Notes[application.PosternQuestionKey("q-1")] != notes[application.PosternQuestionKey("q-1")] {
		t.Fatalf("the run clears nothing and changes no note")
	}
	// The line the Mayor runs is the one posted on the mail, with no stray quote in his words.
	if line := clearLineOf(inbox[0].Body); strings.Count(line, `"`) != 2 {
		t.Fatalf("the clear line is not one quoted --why: %q", line)
	}
}

func clearLineOf(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "mw postern clear ") {
			return line
		}
	}
	return ""
}

func TestVerifyMatchProposesNothingForAQuestionBackOrALeftOverCard(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[]}`)
	r.withCard("q-1", 3*time.Hour, "A: Keep it", "B: Drop it")
	r.saysIn(t, "direct:ans", "q-1", 90*time.Minute, "what does the second one do?")
	if _, err := r.cardMatch().Run(context.Background(), cardRun); err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 0 || r.mail.Writes() != 0 {
		t.Fatalf("a question back proposes nothing, got %+v", r.beads.Written)
	}
}

func TestVerifyMatchProposesAnAnswerOnItsOwnCardOnly(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[
	 {"bead":"q-2","txid":"direct:ans2","option":"A","shows":"he picks the first","confidence":0.95},
	 {"bead":"q-1","txid":"direct:ans2","option":"A","shows":"he picks the first","confidence":0.95}]}`)
	r.withCard("q-1", 3*time.Hour, "A: Keep it", "B: Drop it")
	r.withCard("q-2", 3*time.Hour, "A: Keep it", "B: Drop it")
	r.saysIn(t, "direct:ans2", "q-2", 90*time.Minute, "A")
	result, err := r.cardMatch().Run(context.Background(), cardRun)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 1 || r.beads.Written[0].ID != "q-2" {
		t.Fatalf("the answer in q-2's thread is proposed on q-2 only: %+v", r.beads.Written)
	}
	raw, _ := os.ReadFile(filepath.Join(r.runs, result.Run+".json"))
	if !strings.Contains(string(raw), "left out") {
		t.Fatalf("the answer on the other card is kept in the record as left out: %s", raw)
	}
}

func TestVerifyMatchReadsAGeneralMessageAsAnswerToAnyCard(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[{"bead":"q-1","txid":"direct:gen","option":"A","shows":"first","confidence":0.9}]}`)
	r.withCard("q-1", 3*time.Hour, "A: Keep it", "B: Drop it")
	r.saysIn(t, "direct:gen", "general", 90*time.Minute, "Keep it, A")
	if _, err := r.cardMatch().Run(context.Background(), cardRun); err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 1 || r.beads.Written[0].ID != "q-1" {
		t.Fatalf("a general message answers: %+v", r.beads.Written)
	}
}

func TestVerifyMatchProposesACardOnceForTheSameMessage(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[{"bead":"q-1","txid":"direct:ans","option":"B","shows":"second","confidence":0.9}]}`)
	r.withCard("q-1", 3*time.Hour, "A: Keep it", "B: Drop it")
	r.saysIn(t, "direct:ans", "q-1", 90*time.Minute, "B")
	for range 2 {
		if _, err := r.cardMatch().Run(context.Background(), cardRun); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.beads.Written) != 1 || r.mail.Writes() != 1 {
		t.Fatalf("expected one comment and one mail, got %+v and %d", r.beads.Written, r.mail.Writes())
	}
}

func TestVerifyMatchLeavesOutACardAnswerNamingNoSuchOption(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[{"bead":"q-1","txid":"direct:ans","option":"C","shows":"third","confidence":0.9}]}`)
	r.withCard("q-1", 3*time.Hour, "A: Keep it", "B: Drop it")
	r.saysIn(t, "direct:ans", "q-1", 90*time.Minute, "C")
	if _, err := r.cardMatch().Run(context.Background(), cardRun); err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 0 {
		t.Fatalf("an option the card does not offer is no proposal: %+v", r.beads.Written)
	}
}

func TestVerifyMatchReadsNoCardsOfAClosedBeadOrWithoutACardSource(t *testing.T) {
	r := newVerifyRig(t, `{"matches":[]}`)
	r.withCard("q-1", 3*time.Hour, "A: Keep it", "B: Drop it")
	r.saysIn(t, "direct:ans", "q-1", 90*time.Minute, "B")
	d := r.beads.Details["q-1"]
	d.Status = application.StatusClosed
	r.beads.Details["q-1"] = d
	dry := cardRun
	dry.DryRun = true
	if _, err := r.cardMatch().Run(context.Background(), dry); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.out.String(), "q-1") {
		t.Fatalf("a closed bead's card is not a candidate:\n%s", r.out.String())
	}
}
