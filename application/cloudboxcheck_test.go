package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// boxCheckNow is the clock of these tests: the night cloud4 and cloud5 came up
// from the snapshot with corrupt disks (mw-gq6.1172).
var boxCheckNow = time.Date(2026, 11, 13, 0, 20, 0, 0, time.UTC)

// boxCheckPlan is cloudPlan with an idle time long enough that a box with no
// story is still there when it is checked again.
var boxCheckPlan = func() application.CloudPlan {
	plan := cloudPlan
	plan.Idle = 3 * time.Hour
	return plan
}()

// madeBox is a box Vultr has made and the home has not yet checked: up, its
// make over, as cloud4 was at 21:17Z.
func madeBox(name string) application.CloudBox {
	made := boxCheckNow.Add(-3 * time.Minute)
	return application.CloudBox{Name: name, Up: made.Add(-time.Hour), Made: made, IdleSince: made, Cap: 2, Pace: application.CloudPaceFive}
}

// boxCheck runs one cloud check at now over the book, with the tracker's notes
// as the gate each box's dispatch reads.
func boxCheck(t *testing.T, cloud *apptest.FakeCloud, book *apptest.FakeCloudBook, tracker *apptest.FakeTracker, now time.Time) application.CloudReport {
	t.Helper()
	report, err := application.CloudCheck{
		Work: tracker, Notes: tracker, Gate: tracker, Provider: cloud, Book: book, Host: "laptop",
		Plan: boxCheckPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func gateOf(t *testing.T, tracker *apptest.FakeTracker, box string) string {
	t.Helper()
	said, err := tracker.Note(context.Background(), application.BoxGateKey(box))
	if err != nil {
		t.Fatal(err)
	}
	return said
}

func bookBox(t *testing.T, book *apptest.FakeCloudBook, name string) application.CloudBox {
	t.Helper()
	for _, box := range book.State.Boxes {
		if box.Name == name {
			return box
		}
	}
	t.Fatalf("the book holds no box %s: %+v", name, book.State.Boxes)
	return application.CloudBox{}
}

// cloud4 came up from the snapshot with a corrupt vault pack and EXT4-fs errors
// in its kernel log, and took stories all the same. A box whose own check finds
// such faults takes no story: the book says why, its sessions are not counted
// free, its gate stays shut, and the book raises one alarm that names the one
// command, at every check after without the box being asked again.
func TestABoxThatFailsItsOwnCheckTakesNoStoryAndOneAlarmNamesTheCommand(t *testing.T) {
	cloud := &apptest.FakeCloud{CheckFaults: map[string][]string{"cloud4": {
		"~/millwright-vault: git fsck: error: unknown object type 0 at offset 57790430",
		"dmesg: 8 lines of EXT4-fs error, the last: EXT4-fs error (device vda2): bad block bitmap checksum",
	}}}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{madeBox("cloud4")}}}
	tracker := apptest.NewFakeTracker()

	report := boxCheck(t, cloud, book, tracker, boxCheckNow)

	box := bookBox(t, book, "cloud4")
	if box.MayWork() {
		t.Fatalf("a box that failed its own check may work: %+v", box)
	}
	if !strings.Contains(box.CheckFault, "unknown object type 0") || !strings.Contains(box.CheckFault, "EXT4-fs error") {
		t.Errorf("the book's fault is %q, want what the check found", box.CheckFault)
	}
	if report.Free != 0 || report.Coming != 0 {
		t.Errorf("the failed box counts %d sessions free and %d coming, want none of either", report.Free, report.Coming)
	}
	if said := gateOf(t, tracker, "cloud4"); strings.HasPrefix(said, application.BoxGateOK) || said == "" {
		t.Errorf("the gate of the failed box says %q, want a hold", said)
	}
	if text := report.String(); !strings.Contains(text, "cloud4 failed its own check") || !strings.Contains(text, "mw cloud down cloud4") {
		t.Errorf("the check's report does not say the box failed, with the command:\n%s", text)
	}
	alarms := book.State.MakeAlarms(cloudPlan, boxCheckNow)
	if len(alarms) != 1 || alarms[0].Key != "cloud.check:cloud4" || !strings.Contains(alarms[0].Text, "mw cloud down cloud4") || !strings.Contains(alarms[0].Text, "unknown object type 0") {
		t.Fatalf("the book's alarms are %+v, want one for cloud4's failed check with the command", alarms)
	}

	later := boxCheck(t, cloud, book, tracker, boxCheckNow.Add(time.Minute))
	if calls := cloud.CheckCalls(); len(calls) != 1 {
		t.Errorf("the box was asked to check itself %d times, want once: a failed box waits for mw cloud down", len(calls))
	}
	if len(later.Moves) != 0 {
		t.Errorf("the next check said it again: %v", later.Moves)
	}
	again := book.State.MakeAlarms(cloudPlan, boxCheckNow.Add(time.Minute))
	if len(again) != 1 || again[0].Identity != alarms[0].Identity {
		t.Errorf("the alarm a minute later is %+v, want the same one alarm (identity %q)", again, alarms[0].Identity)
	}
}

// A box Vultr reports locked is not asked to check itself and takes no story,
// however long its make has been over; once Vultr says it is not, its own check
// runs, and only a pass opens its gate and counts its sessions free.
func TestABoxVultrReportsLockedTakesNoStoryUntilItIsUnlockedAndItsCheckPasses(t *testing.T) {
	cloud := &apptest.FakeCloud{Orphans: []application.CloudInstance{{ID: "e038798f", Label: "cloud5", Status: "active", Server: "locked"}}}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{madeBox("cloud5")}}}
	tracker := apptest.NewFakeTracker()

	report := boxCheck(t, cloud, book, tracker, boxCheckNow)
	if box := bookBox(t, book, "cloud5"); box.MayWork() {
		t.Fatalf("a box Vultr reports locked may work: %+v", box)
	}
	if calls := cloud.CheckCalls(); len(calls) != 0 {
		t.Errorf("a locked box was asked to check itself: %v", calls)
	}
	if report.Free != 0 {
		t.Errorf("a locked box counts %d sessions free, want none", report.Free)
	}
	if said := gateOf(t, tracker, "cloud5"); strings.HasPrefix(said, application.BoxGateOK) || !strings.Contains(said, "locked") {
		t.Errorf("the gate of the locked box says %q, want a hold that says locked", said)
	}

	cloud.Orphans[0].Server = "ok"
	report = boxCheck(t, cloud, book, tracker, boxCheckNow.Add(time.Minute))
	box := bookBox(t, book, "cloud5")
	if !box.MayWork() || box.Checked.IsZero() {
		t.Fatalf("the box, unlocked and its check passed, may not work: %+v", box)
	}
	if calls := cloud.CheckCalls(); len(calls) != 1 || calls[0] != "cloud5" {
		t.Errorf("the checks asked were %v, want cloud5 once", calls)
	}
	if report.Free != 2 {
		t.Errorf("the cleared box counts %d sessions free, want its cap of 2", report.Free)
	}
	if said := gateOf(t, tracker, "cloud5"); !strings.HasPrefix(said, application.BoxGateOK) {
		t.Errorf("the gate of the cleared box says %q, want %s", said, application.BoxGateOK)
	}

	// Vultr locks it again: the gate shuts, and a new pass of its check is needed.
	cloud.Orphans[0].Server = "locked"
	boxCheck(t, cloud, book, tracker, boxCheckNow.Add(2*time.Minute))
	if box := bookBox(t, book, "cloud5"); box.MayWork() {
		t.Errorf("a cleared box Vultr locks again may still work: %+v", box)
	}
	if said := gateOf(t, tracker, "cloud5"); strings.HasPrefix(said, application.BoxGateOK) {
		t.Errorf("the gate of the box locked again says %q, want a hold", said)
	}
}

// A box that cannot be reached for its check is neither passed nor failed: it
// takes no story, raises no alarm of a failed check, and is asked again at the
// next check.
func TestABoxThatCannotBeAskedToCheckItselfStaysHeldAndIsAskedAgain(t *testing.T) {
	cloud := &apptest.FakeCloud{Unreachable: map[string]bool{"cloud4": true}}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{madeBox("cloud4")}}}
	tracker := apptest.NewFakeTracker()

	boxCheck(t, cloud, book, tracker, boxCheckNow)
	box := bookBox(t, book, "cloud4")
	if box.MayWork() || box.CheckFault != "" {
		t.Fatalf("a box that could not be asked is %+v, want held with no fault found", box)
	}
	if alarms := book.State.MakeAlarms(cloudPlan, boxCheckNow); len(alarms) != 0 {
		t.Errorf("a box that could not be asked raised %+v", alarms)
	}
	cloud.Unreachable = nil
	boxCheck(t, cloud, book, tracker, boxCheckNow.Add(time.Minute))
	if box := bookBox(t, book, "cloud4"); !box.MayWork() {
		t.Errorf("the box, reached and passed, may not work: %+v", box)
	}
	if calls := cloud.CheckCalls(); len(calls) != 2 {
		t.Errorf("the box was asked %d times, want twice", len(calls))
	}
}

// cloud4's faults showed hours after it came up. A cleared box is asked to
// check itself again every CloudBoxRecheck, and one that fails then takes no
// new story: its gate shuts and the alarm names the command.
func TestAClearedBoxIsCheckedAgainAndAFailureShutsItsGate(t *testing.T) {
	cloud := &apptest.FakeCloud{}
	box := madeBox("cloud4")
	box.Dispatched = box.Made.Add(time.Minute)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{box}}}
	tracker := apptest.NewFakeTracker()

	boxCheck(t, cloud, book, tracker, boxCheckNow)
	boxCheck(t, cloud, book, tracker, boxCheckNow.Add(application.CloudBoxRecheck-time.Minute))
	if calls := cloud.CheckCalls(); len(calls) != 1 {
		t.Fatalf("a box cleared less than %s ago was asked %d times, want once", application.CloudBoxRecheck, len(calls))
	}
	cloud.CheckFaults = map[string][]string{"cloud4": {"~/millwright: git fsck: error: wrong index v1 file size in pack-507c018c.idx"}}
	later := boxCheckNow.Add(application.CloudBoxRecheck + time.Minute)
	boxCheck(t, cloud, book, tracker, later)
	box = bookBox(t, book, "cloud4")
	if box.MayWork() || !strings.Contains(box.CheckFault, "wrong index v1 file size") {
		t.Fatalf("the box that failed its later check is %+v, want held with the fault", box)
	}
	if said := gateOf(t, tracker, "cloud4"); strings.HasPrefix(said, application.BoxGateOK) {
		t.Errorf("the gate of the box says %q after its failed check, want a hold", said)
	}
	if alarms := book.State.MakeAlarms(cloudPlan, later); len(alarms) != 1 || !strings.Contains(alarms[0].Text, "mw cloud down cloud4") {
		t.Errorf("the alarms are %+v, want one with the command", alarms)
	}
}

// A box being made has its gate shut before the provider is asked for it, so
// that a note a destroyed box of the name left open is not the new box's.
func TestAMakeShutsTheGateOfTheNameBeforeTheBoxIsAskedFor(t *testing.T) {
	cloud := &apptest.FakeCloud{}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11"}}
	tracker := apptest.NewFakeTracker()
	if err := tracker.SetNote(context.Background(), application.BoxGateKey("cloud1"), application.BoxGateOK+" 2026-11-11T05:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		tracker.AddStory("", domain.Story{ID: "mw-any." + string(rune('1'+i)), Title: "A story for any host"})
	}
	for _, id := range []string{"mw-any.1", "mw-any.2", "mw-any.3"} {
		if err := tracker.SetStoryMetadata(context.Background(), id, map[string]string{"rig": "millwright", "branch": "main", "harness": "claude", "model": "opus", "effort": "high", "formula": "tdd-feature", "host": domain.HostAuto}); err != nil {
			t.Fatal(err)
		}
	}
	report := boxCheck(t, cloud, book, tracker, boxCheckNow)
	if len(report.Making) != 1 || report.Making[0] != "cloud1" {
		t.Fatalf("the check made %v, want cloud1 (report: %s)", report.Making, report)
	}
	if said := gateOf(t, tracker, "cloud1"); strings.HasPrefix(said, application.BoxGateOK) || said == "" {
		t.Errorf("the gate of the box being made says %q, want a hold", said)
	}
}

// The quiet alarm and the starved-rig check read a box that may not work as a
// box that is down: it has no sync to be quiet about.
func TestABoxThatMayNotWorkIsDownToTheQuietAlarm(t *testing.T) {
	held, cleared := madeBox("cloud4"), madeBox("cloud2")
	cleared.Checked = boxCheckNow
	book := &apptest.FakeCloudBook{State: application.CloudState{Boxes: []application.CloudBox{held, cleared}}}
	down := application.CloudBoxesDown{Book: book, Plan: cloudPlan}
	if is, err := down.Down(context.Background(), "cloud4"); err != nil || !is {
		t.Errorf("a box whose check has not passed is down=%v (%v), want down", is, err)
	}
	if is, err := down.Down(context.Background(), "cloud2"); err != nil || is {
		t.Errorf("a cleared box is down=%v (%v), want up", is, err)
	}
}

// On the box, a dispatch claims nothing until the home has opened its gate: no
// note, or a hold, stops the pass before the sync, and the pass says why.
func TestDispatchOnABoxClaimsNothingUntilTheHomeOpensItsGate(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	var out bytes.Buffer
	ticks := &apptest.FakeTickLog{}
	dispatch.Host, dispatch.Memory, dispatch.Out, dispatch.Log = "cloud4", tracker, &out, ticks
	dispatch.Boot.Host = "cloud4"
	tracker.AddEpic("mw-box", domain.Path{
		Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "cloud4",
	})
	tracker.AddStory("mw-box", domain.Story{ID: "mw-box.1", Title: "A story"})

	for _, gate := range []string{"", application.BoxGateHold + " 2026-11-13T00:20:00Z: it failed its own check: git fsck: unknown object type 0"} {
		if gate != "" {
			if err := tracker.SetNote(ctx, application.BoxGateKey("cloud4"), gate); err != nil {
				t.Fatal(err)
			}
		}
		out.Reset()
		report, err := dispatch.Run(ctx)
		if err != nil {
			t.Fatalf("dispatching with the gate %q: %v", gate, err)
		}
		if report.BoxHeld == "" || len(report.Started) != 0 || len(runner.Names()) != 0 {
			t.Fatalf("with the gate %q the box started %+v (held %q)", gate, report.Started, report.BoxHeld)
		}
		if d, _ := tracker.ShowStory(ctx, "mw-box.1"); d.Assignee != "" {
			t.Fatalf("with the gate %q the box claimed the story for %q", gate, d.Assignee)
		}
		if !strings.Contains(out.String(), "cloud4") || !strings.Contains(out.String(), "takes no story") {
			t.Errorf("with the gate %q the pass said %q, want it to say the box takes no story", gate, out.String())
		}
	}
	if lines := ticks.Lines(); len(lines) != 2 || !strings.Contains(lines[1], " "+application.DispatchLogBoxHeld) {
		t.Errorf("the dispatch log says %q, want two lines of %q", lines, application.DispatchLogBoxHeld)
	}

	if err := tracker.SetNote(ctx, application.BoxGateKey("cloud4"), application.BoxGateOK+" 2026-11-13T00:21:00Z"); err != nil {
		t.Fatal(err)
	}
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching with the gate open: %v", err)
	}
	if report.BoxHeld != "" || len(report.Started) != 1 {
		t.Fatalf("with the gate open: held %q, started %d, want one story started", report.BoxHeld, len(report.Started))
	}
}

// A host that is no box has no gate: the home's own dispatch reads none.
func TestDispatchOnAHostThatIsNoBoxReadsNoGate(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Memory = tracker
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.BoxHeld != "" || len(report.Started) != 1 {
		t.Fatalf("the vps, no box, was held %q and started %d", report.BoxHeld, len(report.Started))
	}
	if reads := tracker.NoteReads(application.BoxGateKey("vps")); reads != 0 {
		t.Errorf("the vps read a box gate %d times", reads)
	}
}

// The book of cloud4 and cloud5 kept "active locked", Vultr's word of their
// makes, for hours after Vultr had said ok. Once a box is made the word is
// dropped: LockedSince alone says a made box is locked.
func TestTheBookDropsVultrsWordOfTheMakeOnceTheBoxIsMade(t *testing.T) {
	cloud := &apptest.FakeCloud{Orphans: []application.CloudInstance{{ID: "758a84ee", Label: "cloud4", Status: "active", Server: "ok"}}}
	box := madeBox("cloud4")
	box.Vultr = "active locked"
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{box}}}
	boxCheck(t, cloud, book, apptest.NewFakeTracker(), boxCheckNow)
	if got := bookBox(t, book, "cloud4"); got.Vultr != "" || !got.LockedSince.IsZero() || !got.MayWork() {
		t.Errorf("the made box Vultr says is ok is %+v, want no word of its make, not locked, and cleared", got)
	}
}
