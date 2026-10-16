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

// With no bead, List prints one line per step not yet run on every open bead:
// bead, step, host/user, age and the step's text cut to one line. A bead named
// prints only its own steps, as it always did.
func TestHandsListWithNoBeadPrintsEveryOpenStepOnEveryOpenBead(t *testing.T) {
	tracker := handsTracker()
	tracker.AddStory("mw-f758y", domain.Story{ID: "mw-f758y.9", Title: "Restart the unit"})
	tracker.AddStory("mw-f758y", domain.Story{ID: "mw-f758y.10", Title: "Already done"})
	tracker.AddStory("mw-f758y", domain.Story{ID: "mw-f758y.11", Title: "Closed with a step left"})
	second := domain.HandsStep{ID: "restart", Host: "vps", As: "user", Run: "systemctl --user restart mw-dispatch\nsystemctl --user status mw-dispatch"}
	ranStep := domain.HandsStep{ID: "ran", Host: "vps", As: "user", Run: "true"}
	for bead, step := range map[string]domain.HandsStep{"mw-f758y.8": lingerStep(), "mw-f758y.9": second, "mw-f758y.10": ranStep, "mw-f758y.11": ranStep} {
		if _, err := addHands(tracker, &bytes.Buffer{}, application.HandsAddRequest{Bead: bead, Step: step}); err != nil {
			t.Fatal(err)
		}
	}
	mustDo(t, tracker.SetNote(context.Background(), application.HandsRanKey("mw-f758y.10", "ran"), `{"at":"2026-09-28T12:03:00Z","exit":0,"host":"vps"}`))
	mustDo(t, tracker.CloseStory(context.Background(), "mw-f758y.11", "done"))
	now := handsNow.Add(51 * time.Hour)

	var out bytes.Buffer
	if _, err := (application.HandsList{Notes: tracker, Tracker: tracker, Out: &out, Now: func() time.Time { return now }}).Run(context.Background(), ""); err != nil {
		t.Fatalf("listing every bead: %v", err)
	}
	want := "mw-f758y.8  linger  desktop/root  2d  loginctl enable-linger jwhite\n" +
		"mw-f758y.9  restart  vps/user  2d  systemctl --user restart mw-dispatch systemctl --user status mw-dispatch\n"
	if out.String() != want {
		t.Errorf("expected\n%s\ngot\n%s", want, out.String())
	}

	out.Reset()
	if _, err := (application.HandsList{Notes: tracker, Tracker: tracker, Out: &out, Now: func() time.Time { return now }}).Run(context.Background(), "mw-f758y.9"); err != nil {
		t.Fatalf("listing one bead: %v", err)
	}
	if text := out.String(); !strings.Contains(text, "restart on vps as user  sha256 ") || strings.Contains(text, "mw-f758y.8") || strings.Contains(text, "linger") {
		t.Errorf("expected only mw-f758y.9's steps, got\n%s", text)
	}
}

func TestHandsListWithNoBeadSaysWhenNothingIsWaiting(t *testing.T) {
	var out bytes.Buffer
	if _, err := (application.HandsList{Notes: handsTracker(), Out: &out}).Run(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if out.String() != "no open hands steps\n" {
		t.Errorf("got %q", out.String())
	}
}

func addHandsPushing(tracker *apptest.FakeTracker, push *apptest.FakePosternSender, req application.HandsAddRequest) error {
	_, err := application.HandsAdd{Tracker: tracker, Notes: tracker, Push: push, Now: func() time.Time { return handsNow }}.Run(context.Background(), req)
	return err
}

// A first add sends the Governor the one "New hands step" push.
func TestHandsAddFirstAddPushesANewStep(t *testing.T) {
	tracker := handsTracker()
	push := &apptest.FakePosternSender{}
	mustDo(t, addHandsPushing(tracker, push, application.HandsAddRequest{Bead: "mw-f758y.8", Step: lingerStep()}))
	sent := push.Sent()
	if len(sent) != 1 || !strings.HasPrefix(sent[0].Text, "New hands step on mw-f758y.8") {
		t.Fatalf("expected one 'New hands step' push, got %+v", sent)
	}
}

// Replacing a step he has not approved tells him nothing the first push did
// not: the step's comment and the view carry the change.
func TestHandsAddReplaceOfAnUnapprovedStepSendsNoSecondPush(t *testing.T) {
	tracker := handsTracker()
	push := &apptest.FakePosternSender{}
	mustDo(t, addHandsPushing(tracker, push, application.HandsAddRequest{Bead: "mw-f758y.8", Step: lingerStep()}))
	changed := lingerStep()
	changed.Run = "loginctl enable-linger jwhite2"
	mustDo(t, addHandsPushing(tracker, push, application.HandsAddRequest{Bead: "mw-f758y.8", Step: changed, Replace: true}))
	if sent := push.Sent(); len(sent) != 1 {
		t.Fatalf("expected exactly one push, got %+v", sent)
	}
	if steps := handsSteps(t, tracker, "mw-f758y.8"); len(steps) != 1 || steps[0].Run != changed.Run {
		t.Fatalf("expected the step replaced all the same, got %+v", steps)
	}
}

// Replacing a step he approved (it ran, which only an approval does) voids
// that approval, and he is told so.
func TestHandsAddReplaceOfAnApprovedStepPushesChangedStep(t *testing.T) {
	tracker := handsTracker()
	push := &apptest.FakePosternSender{}
	mustDo(t, addHandsPushing(tracker, push, application.HandsAddRequest{Bead: "mw-f758y.8", Step: lingerStep()}))
	mustDo(t, tracker.SetNote(context.Background(), application.HandsRanKey("mw-f758y.8", "linger"), `{"at":"x","exit":1,"host":"desktop","why":"no"}`))
	changed := lingerStep()
	changed.Run = "loginctl enable-linger jwhite2"
	mustDo(t, addHandsPushing(tracker, push, application.HandsAddRequest{Bead: "mw-f758y.8", Step: changed, Replace: true}))
	sent := push.Sent()
	if len(sent) != 2 {
		t.Fatalf("expected two pushes, got %+v", sent)
	}
	want := "Changed hands step on mw-f758y.8: Enable lingering (your approval of the old step no longer counts; approve again)"
	if sent[1].Text != want {
		t.Fatalf("expected %q, got %q", want, sent[1].Text)
	}
}

// A user step for this host that names mw, where a user step here would not
// find mw, is kept with a warning saying so; one that finds it, one for
// another host, a root step and a step that names no mw warn of nothing
// (mw-gq6.458).
func TestHandsAddWarnsWhenAUserStepNamesMwThatItWouldNotFind(t *testing.T) {
	for _, c := range []struct {
		name, host, as, run string
		finds, warns        bool
	}{
		{"not found here", "desktop", "user", "grep -q x f || cat a >> f; } && mw cloud check", false, true},
		{"found here", "desktop", "user", "mw cloud check", true, false},
		{"another host", "laptop", "user", "mw cloud check", false, false},
		{"root", "desktop", "root", "mw cloud check", false, false},
		{"no mw", "desktop", "user", "echo mwx; cat mw.toml", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			tracker := handsTracker()
			var errs bytes.Buffer
			var asked []string
			_, err := application.HandsAdd{Tracker: tracker, Notes: tracker, Now: func() time.Time { return handsNow }, Err: &errs,
				Here: "desktop", Finds: func(program string) bool { asked = append(asked, program); return c.finds },
			}.Run(context.Background(), application.HandsAddRequest{Bead: "mw-f758y.8",
				Step: domain.HandsStep{ID: "s", Host: c.host, As: c.as, Run: c.run}})
			if err != nil {
				t.Fatal(err)
			}
			warned := strings.Contains(errs.String(), "warning: step s names mw, which a user step on desktop would not find")
			if warned != c.warns {
				t.Fatalf("expected warned=%v, got %q (asked %v)", c.warns, errs.String(), asked)
			}
			if len(handsSteps(t, tracker, "mw-f758y.8")) != 1 {
				t.Fatal("expected the step kept either way")
			}
		})
	}
}
