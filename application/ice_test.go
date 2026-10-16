package application_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

var iceNow = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

// iceFixture is an epic with an open story and a held one.
func iceFixture(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-i", domain.Path{})
	for _, id := range []string{"mw-i.1", "mw-i.2", "mw-i.3"} {
		tracker.AddStory("mw-i", domain.Story{ID: id, Title: "Story " + id})
	}
	mustDo(t, tracker.SetStatus("mw-i.2", apptest.StatusDeferred))
	mustDo(t, tracker.SetStatus("mw-i.3", apptest.StatusDeferred))
	return tracker
}

func ice(t *testing.T, tracker *apptest.FakeTracker, id, reason string) error {
	t.Helper()
	return application.Ice{Tracker: tracker, Notes: tracker, Now: func() time.Time { return iceNow }}.Run(context.Background(), id, reason)
}

func thaw(t *testing.T, tracker *apptest.FakeTracker, id string, priority *int) error {
	t.Helper()
	return application.Thaw{Tracker: tracker, Notes: tracker, Now: func() time.Time { return iceNow }}.Run(context.Background(), id, priority)
}

func TestIceLabelsHoldsAndCommentsInOneWord(t *testing.T) {
	tracker := iceFixture(t)
	if err := ice(t, tracker, "mw-i.1", "waiting on the new rig"); err != nil {
		t.Fatalf("icing: %v", err)
	}

	d, err := tracker.ShowStory(context.Background(), "mw-i.1")
	mustDo(t, err)
	if !slices.Contains(d.Labels, application.LabelIcebox) {
		t.Errorf("expected the label %q, got %v", application.LabelIcebox, d.Labels)
	}
	if d.Priority != 4 {
		t.Errorf("expected priority 4, got %d", d.Priority)
	}
	if d.Status != apptest.StatusDeferred {
		t.Errorf("expected the bead held, got %s", d.Status)
	}
	if got := tracker.Comments("mw-i.1"); len(got) != 1 || got[0] != "Iced: waiting on the new rig" {
		t.Errorf("expected the one comment 'Iced: waiting on the new rig', got %v", got)
	}
}

func TestIceNeedsAReasonAndAnUnclaimedBead(t *testing.T) {
	tracker := iceFixture(t)
	if err := ice(t, tracker, "mw-i.1", "  "); err == nil {
		t.Error("expected icing without a reason to be refused")
	}
	mustDo(t, tracker.ClaimAs("mw-i.1", "mw@laptop", iceNow))
	if err := ice(t, tracker, "mw-i.1", "later"); err == nil || !strings.Contains(err.Error(), "claimed") {
		t.Errorf("expected a claimed bead to be refused, got %v", err)
	}
	mustDo(t, tracker.SetStatus("mw-i.3", apptest.StatusClosed))
	if err := ice(t, tracker, "mw-i.3", "later"); err == nil {
		t.Error("expected a closed bead to be refused")
	}
	d, err := tracker.ShowStory(context.Background(), "mw-i.3")
	mustDo(t, err)
	if slices.Contains(d.Labels, application.LabelIcebox) || len(tracker.Comments("mw-i.3")) != 0 {
		t.Error("expected a refused ice to write nothing")
	}
}

func TestThawReversesTheLabelAndPriorityAndLeavesTheBeadHeld(t *testing.T) {
	tracker := iceFixture(t)
	mustDo(t, ice(t, tracker, "mw-i.1", "later"))
	mustDo(t, thaw(t, tracker, "mw-i.1", nil))

	d, err := tracker.ShowStory(context.Background(), "mw-i.1")
	mustDo(t, err)
	if slices.Contains(d.Labels, application.LabelIcebox) {
		t.Errorf("expected the label gone, got %v", d.Labels)
	}
	if d.Priority != 3 {
		t.Errorf("expected the default thaw priority 3, got %d", d.Priority)
	}
	if d.Status != apptest.StatusDeferred {
		t.Errorf("expected the bead still held, got %s", d.Status)
	}
	if got := tracker.Comments("mw-i.1"); len(got) != 2 || got[1] != "Thawed" {
		t.Errorf("expected the comment 'Thawed' after the ice, got %v", got)
	}

	two := 2
	mustDo(t, ice(t, tracker, "mw-i.2", "later"))
	mustDo(t, thaw(t, tracker, "mw-i.2", &two))
	d, err = tracker.ShowStory(context.Background(), "mw-i.2")
	mustDo(t, err)
	if d.Priority != 2 {
		t.Errorf("expected priority 2, got %d", d.Priority)
	}
	nine := 9
	mustDo(t, ice(t, tracker, "mw-i.3", "later"))
	if err := thaw(t, tracker, "mw-i.3", &nine); err == nil {
		t.Error("expected a priority of 9 to be refused")
	}
	d, err = tracker.ShowStory(context.Background(), "mw-i.3")
	mustDo(t, err)
	if !slices.Contains(d.Labels, application.LabelIcebox) {
		t.Error("expected a refused thaw to leave the bead iced")
	}
}

func TestThawRefusesABeadThatIsNotIced(t *testing.T) {
	tracker := iceFixture(t)
	if err := thaw(t, tracker, "mw-i.1", nil); err == nil || !strings.Contains(err.Error(), "not iced") {
		t.Errorf("expected 'not iced', got %v", err)
	}
	if len(tracker.Comments("mw-i.1")) != 0 {
		t.Error("expected nothing written")
	}
}

func TestStatusShowsTheIceboxAndLeavesIcedBeadsOutOfHeld(t *testing.T) {
	tracker := iceFixture(t)
	handsNote := func(id string) {
		raw, err := json.Marshal([]map[string]string{{"id": "speak", "host": "laptop", "as": "user", "run": "true"}})
		mustDo(t, err)
		mustDo(t, tracker.SetNote(context.Background(), application.HandsStepsKey(id), string(raw)))
	}
	handsNote("mw-i.2")
	handsNote("mw-i.3")

	// Iced a week and a day ago, and thawed two days ago.
	icedAt := iceNow.AddDate(0, 0, -8)
	mustDo(t, tracker.SetNote(context.Background(), application.IcedNoteKey("mw-i.3"), icedAt.Format(time.RFC3339)))
	mustDo(t, ice(t, tracker, "mw-i.1", "later"))
	mustDo(t, ice(t, tracker, "mw-i.3", "later"))
	mustDo(t, tracker.SetNote(context.Background(), application.IcedNoteKey("mw-i.3"), icedAt.Format(time.RFC3339)))
	mustDo(t, tracker.SetNote(context.Background(), application.ThawedNoteKey("mw-i.9"), iceNow.Add(-48*time.Hour).Format(time.RFC3339)))
	mustDo(t, tracker.SetNote(context.Background(), application.ThawedNoteKey("mw-i.8"), iceNow.AddDate(0, 0, -9).Format(time.RFC3339)))

	mayor := application.MayorReader{Tracker: tracker, Notes: tracker}
	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		HeldHands: mayor,
		Icebox:    application.IceboxReader{Beads: tracker, Notes: tracker, Now: func() time.Time { return iceNow }},
		Now:       func() time.Time { return iceNow },
	}.Run(context.Background())
	mustDo(t, err)
	text := report.String()

	if !strings.Contains(text, "ICEBOX: 2 · oldest 8d · thawed 1 in 7 days") {
		t.Errorf("expected the ICEBOX line, got:\n%s", text)
	}
	held := text[strings.Index(text, application.HeldHandsHeading):]
	if !strings.Contains(held, "mw-i.2") {
		t.Errorf("expected the plain held bead under HELD, got:\n%s", text)
	}
	if strings.Contains(held, "mw-i.3") {
		t.Errorf("expected the iced bead out of HELD, got:\n%s", text)
	}
}

func TestStatusWithNoIcedBeadsStillShowsTheLine(t *testing.T) {
	tracker := iceFixture(t)
	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		Icebox: application.IceboxReader{Beads: tracker, Notes: tracker, Now: func() time.Time { return iceNow }},
		Now:    func() time.Time { return iceNow },
	}.Run(context.Background())
	mustDo(t, err)
	if !strings.Contains(report.String(), "ICEBOX: 0") {
		t.Errorf("expected 'ICEBOX: 0', got:\n%s", report.String())
	}
}

func TestReleaseLeavesAnIcedChildHeldAndSaysSo(t *testing.T) {
	tracker := iceFixture(t)
	mustDo(t, ice(t, tracker, "mw-i.2", "later"))
	out := &strings.Builder{}

	if _, err := (application.Release{Tracker: tracker, Out: out}).Run(context.Background(), "mw-i"); err != nil {
		t.Fatalf("releasing: %v", err)
	}
	for id, want := range map[string]string{"mw-i.2": apptest.StatusDeferred, "mw-i.3": apptest.StatusOpen} {
		d, err := tracker.ShowStory(context.Background(), id)
		mustDo(t, err)
		if d.Status != want {
			t.Errorf("expected %s %s, got %s", id, want, d.Status)
		}
	}
	if !strings.Contains(out.String(), "Left iced: mw-i.2") {
		t.Errorf("expected the release to name the iced child, got:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Ready now: mw-i.2") || strings.Contains(out.String(), ", mw-i.2") {
		t.Errorf("expected the iced child not offered as ready, got:\n%s", out.String())
	}
}

func TestReleaseByIdRefusesAnIcedStory(t *testing.T) {
	tracker := iceFixture(t)
	mustDo(t, ice(t, tracker, "mw-i.2", "later"))
	out := &strings.Builder{}
	if _, err := (application.Release{Tracker: tracker, Out: out}).Run(context.Background(), "mw-i.2"); err != nil {
		t.Fatalf("releasing: %v", err)
	}
	d, err := tracker.ShowStory(context.Background(), "mw-i.2")
	mustDo(t, err)
	if d.Status != apptest.StatusDeferred {
		t.Errorf("expected the iced story still held, got %s", d.Status)
	}
	if !strings.Contains(out.String(), "iced") {
		t.Errorf("expected the output to say it is iced, got:\n%s", out.String())
	}
}

func TestApplyThawsAnIcedBeadOnTheGovernorsTap(t *testing.T) {
	f := newApplyFixture(t)
	mustDo(t, application.Ice{Tracker: f.tracker, Notes: f.tracker}.Run(context.Background(), "mw-e.1", "later"))
	f.action(t, "tx-thaw", map[string]any{"action": "thaw", "bead": "mw-e.1"})

	f.apply(t)

	d, err := f.tracker.ShowStory(context.Background(), "mw-e.1")
	mustDo(t, err)
	if slices.Contains(d.Labels, application.LabelIcebox) || d.Priority != 3 || d.Status != apptest.StatusDeferred {
		t.Errorf("expected thawed at 3 and held, got labels %v priority %d status %s", d.Labels, d.Priority, d.Status)
	}
	if got := f.subjects(t); len(got) != 1 || got[0] != "Thawed: mw-e.1" {
		t.Errorf("expected one mail to the Mayor, got %v", got)
	}
}

func TestApplyThawWithAPriorityAndRefusalOfABeadNotIced(t *testing.T) {
	f := newApplyFixture(t)
	mustDo(t, application.Ice{Tracker: f.tracker, Notes: f.tracker}.Run(context.Background(), "mw-e.1", "later"))
	f.action(t, "tx-t1", map[string]any{"action": "thaw", "bead": "mw-e.1", "priority": 1})
	f.action(t, "tx-t2", map[string]any{"action": "thaw", "bead": "mw-e.2"})

	f.apply(t)

	d, err := f.tracker.ShowStory(context.Background(), "mw-e.1")
	mustDo(t, err)
	if d.Priority != 1 {
		t.Errorf("expected priority 1, got %d", d.Priority)
	}
	if got := f.subjects(t); len(got) != 2 || got[1] != "Not applied: thaw mw-e.2" {
		t.Errorf("expected the second thaw refused, got %v", got)
	}
}

func TestApplyDoesNotThawOnAnotherKeysWord(t *testing.T) {
	f := newApplyFixture(t)
	mustDo(t, application.Ice{Tracker: f.tracker, Notes: f.tracker}.Run(context.Background(), "mw-e.1", "later"))
	f.message(t, "someone-elses-key", "tx-other", `{"action":"thaw","bead":"mw-e.1"}`)

	f.apply(t)

	d, err := f.tracker.ShowStory(context.Background(), "mw-e.1")
	mustDo(t, err)
	if !slices.Contains(d.Labels, application.LabelIcebox) || d.Priority != 4 {
		t.Errorf("expected the bead still iced, got labels %v priority %d", d.Labels, d.Priority)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx-other")); note != "" {
		t.Errorf("expected the stranger's word not applied, got %q", note)
	}
}
