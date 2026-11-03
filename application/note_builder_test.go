package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// reachSpy is the BuilderReach of a note: what it was asked to type, and where.
type reachSpy struct {
	host, story, line string
	err               error
}

func (r *reachSpy) Type(_ context.Context, host, story, line string) error {
	r.host, r.story, r.line = host, story, line
	return r.err
}

// aRunningStory is mw-gq6.30 claimed on host by mw, with its session started
// on the runner.
func aRunningStory(t *testing.T, host string) (*apptest.FakeTracker, *apptest.FakeRunner) {
	t.Helper()
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story under way", host)
	if err := tracker.ClaimAs("mw-gq6.30", application.MwSeat+"@"+host, statusNow.Add(application.ClaimLease)); err != nil {
		t.Fatalf("claiming the story: %v", err)
	}
	runner := apptest.NewFakeRunner()
	if err := runner.Start(context.Background(), application.SessionSpec{
		Name: application.SessionName("mw-gq6.30"), Command: []string{"claude"},
	}); err != nil {
		t.Fatalf("starting the session: %v", err)
	}
	return tracker, runner
}

func aBuilderNote(tracker *apptest.FakeTracker, runner application.Runner, reach application.BuilderReach, out *bytes.Buffer) application.Note {
	note := application.Note{
		Tracker: tracker, ToBuilder: true, Host: "vps", Runner: runner, Reach: reach,
		Now: func() time.Time { return statusNow },
	}
	if out != nil {
		note.Out = out
	}
	return note
}

func TestANoteToTheBuilderIsTypedIntoItsLiveSessionAndRecorded(t *testing.T) {
	tracker, runner := aRunningStory(t, "vps")
	var out bytes.Buffer

	report, err := aBuilderNote(tracker, runner, nil, &out).Run(context.Background(), "mw-gq6.30",
		"Never touch the lxx-morph list.\nIt was withdrawn at {now}.")
	if err != nil {
		t.Fatalf("noting to the builder: %v", err)
	}

	if report.Delivery != application.NoteTyped {
		t.Fatalf("expected the note typed into the session, got %q", report.Delivery)
	}
	typed := runner.Input(application.SessionName("mw-gq6.30"))
	if len(typed) != 1 {
		t.Fatalf("expected one line typed into the session, got %q", typed)
	}
	line := typed[0]
	if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Fatalf("expected one line ending in Enter, got %q", line)
	}
	for _, want := range []string{"Never touch the lxx-morph list.", "withdrawn at 2026-09-18 12:00Z", "mw-gq6.30", application.NoteReplyMark} {
		if !strings.Contains(line, want) {
			t.Fatalf("expected the line typed to hold %q, got %q", want, line)
		}
	}

	comments := tracker.Comments("mw-gq6.30")
	if len(comments) != 2 {
		t.Fatalf("expected the note and the record of its delivery as comments, got %q", comments)
	}
	if !strings.HasPrefix(comments[0], application.NoteToBuilderMark) || !strings.Contains(comments[0], "lxx-morph") {
		t.Fatalf("expected the first comment to be the note, marked, got %q", comments[0])
	}
	if !strings.Contains(comments[1], "typed into the session mw-gq6_30") {
		t.Fatalf("expected the second comment to record the delivery, got %q", comments[1])
	}
	if !strings.Contains(out.String(), "typed into") {
		t.Fatalf("expected the report to say it was typed, got %q", out.String())
	}
}

func TestANoteToTheBuilderOfAStoryOnAnotherHostIsTypedThereByReach(t *testing.T) {
	tracker, runner := aRunningStory(t, "desktop")
	reach := &reachSpy{}

	report, err := aBuilderNote(tracker, runner, reach, nil).Run(context.Background(), "mw-gq6.30", "Stop.")
	if err != nil {
		t.Fatalf("noting to the builder: %v", err)
	}

	if report.Delivery != application.NoteTyped || reach.host != "desktop" || reach.story != "mw-gq6.30" {
		t.Fatalf("expected the line typed on desktop, got %q via %+v", report.Delivery, reach)
	}
	if !strings.Contains(reach.line, "Stop.") {
		t.Fatalf("expected the line to hold the note, got %q", reach.line)
	}
	if len(runner.Input(application.SessionName("mw-gq6.30"))) != 0 {
		t.Fatalf("expected this host's runner left alone for a story on another host")
	}
}

func TestANoteToTheBuilderThatCannotBeTypedSaysSoAndStillLeadsTheNextAttempt(t *testing.T) {
	tracker, runner := aRunningStory(t, "desktop")
	reach := &reachSpy{err: errors.New("ssh: no route")}

	report, err := aBuilderNote(tracker, runner, reach, nil).Run(context.Background(), "mw-gq6.30", "Stop.")
	if err != nil {
		t.Fatalf("a delivery that failed is reported, not an error: %v", err)
	}

	if report.Delivery != application.NoteNotTyped || !strings.Contains(report.Why, "no route") {
		t.Fatalf("expected the note not typed, with why, got %q %q", report.Delivery, report.Why)
	}
	comments := tracker.Comments("mw-gq6.30")
	if len(comments) != 2 || !strings.HasPrefix(comments[0], application.NoteToBuilderMark) ||
		!strings.Contains(comments[1], "could not be typed") {
		t.Fatalf("expected the note kept and the failure recorded, got %q", comments)
	}
}

func TestANoteToTheBuilderOfAStoryWithNoLiveSessionLeadsItsNextAttempt(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story not started", "vps")
	runner := apptest.NewFakeRunner()
	var out bytes.Buffer

	report, err := aBuilderNote(tracker, runner, nil, &out).Run(context.Background(), "mw-gq6.30", "Skip the list.")
	if err != nil {
		t.Fatalf("noting to the builder: %v", err)
	}

	if report.Delivery != application.NoteNextAttempt {
		t.Fatalf("expected the note to lead the next attempt, got %q", report.Delivery)
	}
	if !strings.Contains(out.String(), "next attempt") {
		t.Fatalf("expected the report to say so, got %q", out.String())
	}
	if comments := tracker.Comments("mw-gq6.30"); len(comments) < 1 || !strings.HasPrefix(comments[0], application.NoteToBuilderMark) {
		t.Fatalf("expected the marked note on the bead, got %q", comments)
	}
}

func TestANoteToTheBuilderWhoseSessionIsGoneLeadsItsNextAttempt(t *testing.T) {
	tracker, _ := aRunningStory(t, "vps")
	runner := apptest.NewFakeRunner() // the session is gone

	report, err := aBuilderNote(tracker, runner, nil, nil).Run(context.Background(), "mw-gq6.30", "Skip the list.")
	if err != nil {
		t.Fatalf("noting to the builder: %v", err)
	}
	if report.Delivery != application.NoteNextAttempt {
		t.Fatalf("expected the note to lead the next attempt, got %q", report.Delivery)
	}
}

func TestAPlainNoteTypesNothing(t *testing.T) {
	tracker, runner := aRunningStory(t, "vps")
	note := aBuilderNote(tracker, runner, nil, nil)
	note.ToBuilder = false

	if _, err := note.Run(context.Background(), "mw-gq6.30", "Just a comment."); err != nil {
		t.Fatalf("noting: %v", err)
	}
	if typed := runner.Input(application.SessionName("mw-gq6.30")); len(typed) != 0 {
		t.Fatalf("expected nothing typed for a plain note, got %q", typed)
	}
	if comments := tracker.Comments("mw-gq6.30"); len(comments) != 1 || comments[0] != "Just a comment." {
		t.Fatalf("expected the comment as written, got %q", comments)
	}
}

func TestTheNotesToTheBuilderAreReadBackFromTheComments(t *testing.T) {
	comments := []application.Comment{
		{Text: "an ordinary comment"},
		{Text: application.NoteToBuilderMark + " first"},
		{Text: "mw note: typed into the session x"},
		{Text: application.NoteToBuilderMark + " second"},
	}
	got := application.BuilderNotesIn(comments)
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("expected the two notes with the mark taken off, got %q", got)
	}
}

func TestTheBootPromptOfANextAttemptLeadsWithTheNotesToTheBuilder(t *testing.T) {
	detail := application.StoryDetail{BuilderNotes: []string{"Never touch the lxx-morph list."}}
	prompt := application.BootPrompt(application.Seat{Name: "builder", Rig: "millwright"}, detail)

	notes := strings.Index(prompt, "Never touch the lxx-morph list.")
	path := strings.Index(prompt, "## Path")
	if notes < 0 || path < 0 || notes > path {
		t.Fatalf("expected the notes to lead the story in the boot prompt, before its path, got:\n%s", prompt)
	}
	plain := application.BootPrompt(application.Seat{Name: "builder", Rig: "millwright"}, application.StoryDetail{})
	if strings.Contains(plain, "from the Mayor") {
		t.Fatalf("expected no notes section when there are none, got:\n%s", plain)
	}
}
