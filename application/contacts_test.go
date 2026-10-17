package application_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

const steeleNumber = "+15551230001"

func matthewSteele() application.Contact {
	return application.Contact{Name: "Matthew Steele", Mobiles: []string{steeleNumber}}
}

// toDoCard files a card through the Governor's todo_add with notes, and reads
// the description the card was filed with.
func (f *applyFixture) toDoCard(t *testing.T, inbox application.PosternInbox, notes string) string {
	t.Helper()
	f.action(t, "tx-add", map[string]any{"action": "todo_add", "list": "mw-e", "title": "Text Matthew", "notes": notes})
	for _, label := range []string{application.LabelHitl, application.LabelHitlTodo} {
		mustDo(t, f.tracker.AddLabel(context.Background(), "mw-e", label))
	}
	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	epic, err := f.tracker.ShowEpic(context.Background(), "mw-e")
	mustDo(t, err)
	for _, story := range epic.Stories {
		if story.Story.Title == "Text Matthew" {
			return story.Description
		}
	}
	t.Fatalf("no card was filed; the inbox said %q", f.out.String())
	return ""
}

func TestOneMatchWithAMobileIsWrittenIntoTheTextLine(t *testing.T) {
	f := newApplyFixture(t)
	book := &apptest.FakeContactBook{Matches: map[string][]application.Contact{"Matthew": {matthewSteele()}}}
	inbox := f.inbox()
	inbox.Contacts = book

	got := f.toDoCard(t, inbox, "Text: Matthew | Running ten late\nOpen: [Call](tel:+15550000000)")

	want := "Text: Matthew Steele <" + steeleNumber + "> | Running ten late\nOpen: [Call](tel:+15550000000)"
	if got != want {
		t.Fatalf("expected the number written into the line:\n%q\ngot:\n%q", want, got)
	}
	if asked := book.Asked(); len(asked) != 1 || asked[0] != "Matthew" {
		t.Fatalf("expected one lookup of Matthew, got %q", asked)
	}
}

func TestNoGuessIsMadeWhenTheBookDoesNotGiveExactlyOneMobile(t *testing.T) {
	cases := []struct {
		name    string
		matches []application.Contact
		said    string
	}{
		{"two contacts", []application.Contact{matthewSteele(), {Name: "Matthew Jones", Mobiles: []string{"+15551230002"}}}, "Contacts: two contacts match Matthew"},
		{"no contact", nil, "Contacts: no contact named Matthew"},
		{"no mobile", []application.Contact{{Name: "Matthew Steele"}}, "Contacts: one contact matches Matthew but has no mobile number"},
		{"two mobiles", []application.Contact{{Name: "Matthew Steele", Mobiles: []string{steeleNumber, "+15551230009"}}}, "Contacts: one contact matches Matthew but has two mobile numbers"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newApplyFixture(t)
			inbox := f.inbox()
			inbox.Contacts = &apptest.FakeContactBook{Matches: map[string][]application.Contact{"Matthew": c.matches}}

			got := f.toDoCard(t, inbox, "Text: Matthew | Running ten late")

			want := "Text: Matthew | Running ten late\n" + c.said
			if got != want {
				t.Fatalf("expected the line unchanged with one line saying why:\n%q\ngot:\n%q", want, got)
			}
			if strings.Contains(got, "+1555") {
				t.Fatalf("a number was written on a guess: %q", got)
			}
		})
	}
}

func TestNotLinkedLeavesTheLineAndSaysSo(t *testing.T) {
	f := newApplyFixture(t)
	inbox := f.inbox()
	inbox.Contacts = &apptest.FakeContactBook{Err: fmt.Errorf("%w: there is no token", application.ErrGoogleNotLinked)}

	got := f.toDoCard(t, inbox, "Text: Matthew | Running ten late")

	if got != "Text: Matthew | Running ten late" {
		t.Fatalf("expected the line unchanged, got %q", got)
	}
	if !strings.Contains(f.out.String(), "not linked: mw google link") {
		t.Fatalf("expected mw to say 'not linked: mw google link', got %q", f.out.String())
	}
}

func TestALookupThatFailsLeavesTheLineAndIsSaid(t *testing.T) {
	f := newApplyFixture(t)
	inbox := f.inbox()
	inbox.Contacts = &apptest.FakeContactBook{Err: errors.New("Google's People API answered HTTP 503")}

	got := f.toDoCard(t, inbox, "Text: Matthew | Running ten late")

	if got != "Text: Matthew | Running ten late" {
		t.Fatalf("expected the line unchanged, got %q", got)
	}
	if !strings.Contains(f.out.String(), "HTTP 503") {
		t.Fatalf("expected the failure said, got %q", f.out.String())
	}
}

func TestLinesThatAreNotNameOnlyTextLinesAreNeverLookedUp(t *testing.T) {
	for _, notes := range []string{
		"Text: Matthew Steele <" + steeleNumber + "> | Running ten late",
		"Texts are due Friday | not a Text: line",
		"Text: Matthew",
		"Buy hose",
	} {
		f := newApplyFixture(t)
		book := &apptest.FakeContactBook{Matches: map[string][]application.Contact{"Matthew": {matthewSteele()}}}
		inbox := f.inbox()
		inbox.Contacts = book
		if got := f.toDoCard(t, inbox, notes); got != notes {
			t.Fatalf("expected %q unchanged, got %q", notes, got)
		}
		if asked := book.Asked(); len(asked) != 0 {
			t.Fatalf("expected no lookup for %q, got %q", notes, asked)
		}
	}
}

// A card is looked up once: an edit that leaves the number or the saying on
// the card in place makes no second lookup, an edit of anything but the notes
// makes none, and a new name is looked up.
func TestACardIsLookedUpOnceNotOnEveryWrite(t *testing.T) {
	f := newApplyFixture(t)
	mustDo(t, f.tracker.SetLabels("mw-e.3", application.LabelHitl, application.LabelHitlTodo))
	book := &apptest.FakeContactBook{Matches: map[string][]application.Contact{
		"Matthew": {matthewSteele(), {Name: "Matthew Jones", Mobiles: []string{"+15551230002"}}},
		"Dad":     {{Name: "Dad", Mobiles: []string{"+15551230003"}}},
	}}
	inbox := f.inbox()
	inbox.Contacts = book
	edit := func(txid string, action map[string]any) {
		t.Helper()
		action["action"], action["bead"] = "todo_edit", "mw-e.3"
		f.action(t, txid, action)
		if _, err := inbox.Apply(context.Background()); err != nil {
			t.Fatalf("applying: %v", err)
		}
	}
	notesOf := func() string {
		d, err := f.tracker.ShowStory(context.Background(), "mw-e.3")
		mustDo(t, err)
		return d.Description
	}

	edit("tx-1", map[string]any{"notes": "Text: Matthew | Hi"})
	saying := "Text: Matthew | Hi\nContacts: two contacts match Matthew"
	if got := notesOf(); got != saying {
		t.Fatalf("expected %q, got %q", saying, got)
	}
	// the phone sends the notes back as it read them, the saying included
	edit("tx-2", map[string]any{"notes": saying + "\nbring the hose"})
	edit("tx-3", map[string]any{"title": "Text Matthew about the hose"})
	if asked := book.Asked(); len(asked) != 1 {
		t.Fatalf("expected one lookup of Matthew in all, got %q", asked)
	}

	// a different name on the same line is not the one the saying was for
	edit("tx-4", map[string]any{"notes": "Text: Dad | Hi\nContacts: two contacts match Matthew"})
	want := "Text: Dad <+15551230003> | Hi\nContacts: two contacts match Matthew"
	if got := notesOf(); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	edit("tx-5", map[string]any{"notes": want})
	if asked := book.Asked(); len(asked) != 2 || asked[1] != "Dad" {
		t.Fatalf("expected the second lookup to be Dad and no third, got %q", asked)
	}
}

func TestOnlyTheNumberAndTheNameReachTheCard(t *testing.T) {
	f := newApplyFixture(t)
	book := &apptest.FakeContactBook{Matches: map[string][]application.Contact{"Matthew": {matthewSteele()}}}
	inbox := f.inbox()
	inbox.Contacts = book

	got := f.toDoCard(t, inbox, "Text: Matthew | Hi")

	if got != "Text: Matthew Steele <"+steeleNumber+"> | Hi" {
		t.Fatalf("unexpected card %q", got)
	}
	// nothing else of the lookup reaches the comments, the mail or the output
	var seen []string
	epic, err := f.tracker.ShowEpic(context.Background(), "mw-e")
	mustDo(t, err)
	for _, s := range epic.Stories {
		seen = append(seen, f.tracker.Comments(s.Story.ID)...)
	}
	seen = append(seen, f.subjects(t)...)
	seen = append(seen, f.out.String())
	for _, text := range seen {
		if strings.Contains(text, steeleNumber) || strings.Contains(text, "Steele") {
			t.Fatalf("the address book reached %q", text)
		}
	}
}

func TestFilingAToDoCardRunsTheSameLookup(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	book := &apptest.FakeContactBook{Matches: map[string][]application.Contact{"Matthew": {matthewSteele()}}}
	plan := twoStoryPlan()
	plan.Stories[0].Description = "Text: Matthew | Hi"
	plan.Stories[0].Labels = []string{application.LabelHitl, application.LabelHitlTodo}
	plan.Stories[1].Description = "Text: Matthew | Hi"

	filed, err := application.File{Tracker: tracker, Contacts: book, Out: &bytes.Buffer{}}.Run(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}

	card, err := tracker.ShowStory(context.Background(), filed.Stories[0].ID)
	mustDo(t, err)
	if want := "Text: Matthew Steele <" + steeleNumber + "> | Hi"; card.Description != want {
		t.Fatalf("expected %q on the card, got %q", want, card.Description)
	}
	story, err := tracker.ShowStory(context.Background(), filed.Stories[1].ID)
	mustDo(t, err)
	if story.Description != "Text: Matthew | Hi" {
		t.Fatalf("a story that is not a To do card must be left alone, got %q", story.Description)
	}
	if asked := book.Asked(); len(asked) != 1 {
		t.Fatalf("expected one lookup, got %q", asked)
	}
}

func TestContactsFindPrintsEachMatchWithItsMobile(t *testing.T) {
	book := &apptest.FakeContactBook{Matches: map[string][]application.Contact{"Matthew": {
		matthewSteele(),
		{Name: "Matthew Jones"},
		{Name: "Matt Two", Mobiles: []string{"+15551230004", "+15551230005"}},
	}}}
	var out bytes.Buffer

	if err := (application.ContactsFind{Book: book, Out: &out}).Run(context.Background(), "Matthew"); err != nil {
		t.Fatal(err)
	}

	want := "Matthew Steele  " + steeleNumber + "\n" +
		"Matthew Jones  (no mobile number)\n" +
		"Matt Two  +15551230004, +15551230005\n"
	if out.String() != want {
		t.Fatalf("expected:\n%s\ngot:\n%s", want, out.String())
	}
}

func TestContactsFindWithNoMatchIsNotAnError(t *testing.T) {
	var out bytes.Buffer

	err := application.ContactsFind{Book: &apptest.FakeContactBook{}, Out: &out}.Run(context.Background(), "Zed")

	if err != nil {
		t.Fatalf("expected exit 0 with no match, got %v", err)
	}
	if out.String() != "no contact named Zed\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestContactsFindNotLinkedSaysHowToLink(t *testing.T) {
	book := &apptest.FakeContactBook{Err: fmt.Errorf("%w: there is no token", application.ErrGoogleNotLinked)}

	err := application.ContactsFind{Book: book, Out: &bytes.Buffer{}}.Run(context.Background(), "Matthew")

	if err == nil || !strings.Contains(err.Error(), "not linked: mw google link") {
		t.Fatalf("expected 'not linked: mw google link', got %v", err)
	}
}
