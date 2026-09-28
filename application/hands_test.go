package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

var handsNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func handsTracker() *apptest.FakeTracker {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-f758y", domain.Path{})
	tracker.AddStory("mw-f758y", domain.Story{ID: "mw-f758y.8", Title: "Enable lingering"})
	return tracker
}

func lingerStep() domain.HandsStep {
	return domain.HandsStep{ID: "linger", Host: "desktop", As: "root", Run: "loginctl enable-linger jwhite", WayBack: "loginctl disable-linger jwhite"}
}

func addHands(tracker *apptest.FakeTracker, out *bytes.Buffer, req application.HandsAddRequest) (application.HandsStepRecord, error) {
	return application.HandsAdd{Tracker: tracker, Notes: tracker, Now: func() time.Time { return handsNow }, Out: out}.Run(context.Background(), req)
}

func handsSteps(t *testing.T, tracker *apptest.FakeTracker, bead string) []application.HandsStepRecord {
	t.Helper()
	raw, err := tracker.Note(context.Background(), application.HandsStepsKey(bead))
	mustDo(t, err)
	var steps []application.HandsStepRecord
	if raw != "" {
		mustDo(t, json.Unmarshal([]byte(raw), &steps))
	}
	return steps
}

// Adding a step keeps it in the bead's hands note, comments it on the bead
// exactly as it will run, and makes sure the bead is one for his hands.
func TestHandsAddKeepsTheStepCommentsItAndLabelsTheBead(t *testing.T) {
	tracker := handsTracker()
	var out bytes.Buffer

	record, err := addHands(tracker, &out, application.HandsAddRequest{Bead: "mw-f758y.8", Step: lingerStep()})
	if err != nil {
		t.Fatalf("adding: %v", err)
	}
	if record.AddedAt != "2026-09-28T12:00:00Z" || record.HandsStep != lingerStep() {
		t.Fatalf("expected the step stamped now, got %+v", record)
	}
	steps := handsSteps(t, tracker, "mw-f758y.8")
	if len(steps) != 1 || steps[0] != record {
		t.Fatalf("expected the one step in the note, got %+v", steps)
	}
	raw, _ := tracker.Note(context.Background(), application.HandsStepsKey("mw-f758y.8"))
	if !strings.Contains(raw, `"way_back":"loginctl disable-linger jwhite"`) || !strings.Contains(raw, `"added_at":"2026-09-28T12:00:00Z"`) {
		t.Fatalf("expected the note to carry §17's field names, got %s", raw)
	}
	comments := tracker.Comments("mw-f758y.8")
	want := "HANDS STEP linger on desktop as root:\n```sh\nloginctl enable-linger jwhite\n```\nway back:\n```sh\nloginctl disable-linger jwhite\n```"
	if len(comments) != 1 || comments[0] != want {
		t.Fatalf("expected the step commented\n%s\ngot %q", want, comments)
	}
	detail, _ := tracker.ShowStory(context.Background(), "mw-f758y.8")
	if !detail.Hitl() {
		t.Fatalf("expected the bead labelled hitl, got %v", detail.Labels)
	}
	if !strings.Contains(out.String(), domain.HandsSHA256("mw-f758y.8", lingerStep())) {
		t.Fatalf("expected the step's sha256 printed, got %q", out.String())
	}
}

// A step whose text holds a fence of its own is fenced with a longer one.
func TestHandsAddFencesAStepThatHoldsBackticks(t *testing.T) {
	tracker := handsTracker()
	step := domain.HandsStep{ID: "doc", Host: "desktop", As: "user", Run: "printf '```\\n'"}
	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: step}); err != nil {
		t.Fatal(err)
	}
	comment := tracker.Comments("mw-f758y.8")[0]
	if !strings.Contains(comment, "````sh\nprintf '```\\n'\n````") || !strings.Contains(comment, "way back: none") {
		t.Fatalf("expected a four-backtick fence and no way back, got %q", comment)
	}
}

// An id is used once per bead: a second step of the same id is refused
// unless it replaces the first — which changes its hash, voids any approval
// of the old one, and forgets that the old one ran.
func TestHandsAddRefusesADuplicateUnlessItReplaces(t *testing.T) {
	tracker := handsTracker()
	mustDo(t, tracker.SetLabels("mw-f758y.8", "hitl"))
	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: lingerStep()}); err != nil {
		t.Fatal(err)
	}
	mustDo(t, tracker.SetNote(context.Background(), application.HandsRanKey("mw-f758y.8", "linger"), `{"at":"x","exit":0,"host":"desktop"}`))
	changed := lingerStep()
	changed.Run = "loginctl enable-linger jwhite2"

	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: changed}); err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("expected the duplicate refused, naming --replace, got %v", err)
	}
	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: changed, Replace: true}); err != nil {
		t.Fatalf("replacing: %v", err)
	}
	steps := handsSteps(t, tracker, "mw-f758y.8")
	if len(steps) != 1 || steps[0].Run != changed.Run {
		t.Fatalf("expected the step replaced in place, got %+v", steps)
	}
	if ran, _ := tracker.Note(context.Background(), application.HandsRanKey("mw-f758y.8", "linger")); ran != "" {
		t.Fatalf("expected the old step's run forgotten, got %q", ran)
	}
	if comments := tracker.Comments("mw-f758y.8"); !strings.Contains(comments[len(comments)-1], "replacing") {
		t.Fatalf("expected the comment to say it replaces, got %q", comments[len(comments)-1])
	}
}

func TestHandsAddRefusesAnInvalidStepOrAnUnknownBead(t *testing.T) {
	tracker := handsTracker()
	bad := lingerStep()
	bad.As = "admin"
	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: bad}); err == nil {
		t.Fatal("expected a step as admin refused")
	}
	if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-gone", Step: lingerStep()}); err == nil || !strings.Contains(err.Error(), "mw-gone") {
		t.Fatalf("expected an unknown bead refused, got %v", err)
	}
	if n := tracker.Writes(); n != 0 {
		t.Fatalf("expected nothing written, got %d writes", n)
	}
}

// List prints every step of a bead: its hash, whether it ran, and its text.
func TestHandsListPrintsEveryStepWithItsHashAndRun(t *testing.T) {
	tracker := handsTracker()
	mustDo(t, tracker.SetLabels("mw-f758y.8", "hitl"))
	second := domain.HandsStep{ID: "restart", Host: "vps", As: "user", Run: "systemctl --user restart mw-dispatch"}
	for _, step := range []domain.HandsStep{lingerStep(), second} {
		if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: "mw-f758y.8", Step: step}); err != nil {
			t.Fatal(err)
		}
	}
	mustDo(t, tracker.SetNote(context.Background(), application.HandsRanKey("mw-f758y.8", "linger"), `{"at":"2026-09-28T12:03:00Z","exit":0,"host":"desktop"}`))
	var out bytes.Buffer

	steps, err := application.HandsList{Notes: tracker, Out: &out}.Run(context.Background(), "mw-f758y.8")
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected two steps, got %+v", steps)
	}
	text := out.String()
	for _, want := range []string{
		"linger on desktop as root  sha256 " + domain.HandsSHA256("mw-f758y.8", lingerStep()) + "  ran 2026-09-28T12:03:00Z on desktop, exit 0",
		"restart on vps as user  sha256 " + domain.HandsSHA256("mw-f758y.8", second) + "  not run",
		"    systemctl --user restart mw-dispatch",
		"    way back: loginctl disable-linger jwhite",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in\n%s", want, text)
		}
	}
}
