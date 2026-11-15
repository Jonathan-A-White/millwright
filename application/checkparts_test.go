package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

const (
	// aTipThePartPassedAt and theTipNow are two commits of one story branch.
	aTipThePartPassedAt = "1598c69170f9aaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	theTipNow           = "40f778c3993cbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// A call of mw check --part N tells a Builder that part N passed only at the
// commit at the branch's tip. A part whose record is of a pass at an earlier
// commit, and which is not started because the host is over its load limit, is
// said not to have run at this commit, naming the earlier one, and is an error
// (mw-gq6.1304). This is the part that takes the merge slot, run in the call.
func TestAPartPassedAtAnEarlierCommitIsNotReportedAsPassedAtTheTip(t *testing.T) {
	d := aDetachedPartRig(t, true)
	d.parts.command = "npm run gate:shots"
	d.tune = func(k *application.Check) {
		k.Slot = rig.NewSlots(rig.WithSlotPoll(5 * time.Millisecond))
		k.Landing = &headedLanding{fakeRetryLanding: &fakeRetryLanding{AheadCount: 1}, head: theTipNow}
	}
	if _, err := d.records.PutRunFile(context.Background(), "mw-x.1", application.CheckPartFileName(1, 1), "tip "+aTipThePartPassedAt+"\npassed\nok\n"); err != nil {
		t.Fatal(err)
	}
	d.load.set(true)

	err := d.check(30 * time.Second)
	if err == nil {
		t.Fatal("expected a part that did not run at the tip not to pass")
	}
	for _, want := range []string{"part 1 of 1 (vitest 5/6) has not run at this commit", "last passed at 1598c69170f9"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the line to say %q, got %q", want, err.Error())
		}
	}
	if strings.Contains(d.told.String(), "passed") {
		t.Errorf("expected no 'passed' line for a part that did not run at the tip, got %q", d.told.String())
	}
	if d.parts.ranCount() != 0 {
		t.Errorf("expected the part not to have been run on a host over its load limit, it ran %d time(s)", d.parts.ranCount())
	}
}

// The same, in the call that has run the part and passed it at the tip: it still
// passes.
func TestAPartPassedAtTheTipIsReportedAsPassed(t *testing.T) {
	d := aDetachedPartRig(t, true)
	d.parts.command = "npm run gate:shots"
	d.tune = func(k *application.Check) {
		k.Slot = rig.NewSlots(rig.WithSlotPoll(5 * time.Millisecond))
		k.Landing = &headedLanding{fakeRetryLanding: &fakeRetryLanding{AheadCount: 1}, head: theTipNow}
	}
	if _, err := d.records.PutRunFile(context.Background(), "mw-x.1", application.CheckPartFileName(1, 1), "tip "+aTipThePartPassedAt+"\npassed\nok\n"); err != nil {
		t.Fatal(err)
	}
	close(d.parts.release)

	if err := d.check(30 * time.Second); err != nil {
		t.Fatalf("expected the part to run and pass at the tip, got %v", err)
	}
	if !strings.Contains(d.told.String(), "passed at 40f778c3993c") {
		t.Errorf("expected the pass to name the tip, got %q", d.told.String())
	}
}
