package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// The old home, as the planned move asks of it. Each call is written into calls
// with an "old:" in front, so a test reads the old home's steps and the new
// home's in the one order they ran.

func (w *moveWorld) MailMayor(_ context.Context, ssh []string, from, subject, body string) error {
	if from != "mw@laptop" || strings.Join(ssh, " ") != "ssh desktop" {
		return errors.New("the hand-off mail is sent on the old home, as the move: " + from + " over " + strings.Join(ssh, " "))
	}
	w.handoffMail = subject + " | " + body
	return w.did("old: mail mayor")
}

func (w *moveWorld) MayorGone(_ context.Context, _ []string, wait time.Duration) (bool, string, error) {
	if wait != 15*time.Minute {
		return false, "", errors.New("the wait is 15 minutes")
	}
	err := w.did("old: wait for the Mayor to hand off")
	if w.mayorStays {
		return false, "Mayor after handoff 98 (tmux window @5), since 2026-09-29T13:00:00Z", err
	}
	return true, ".mayor-acting is empty", err
}

func (w *moveWorld) Sync(context.Context, []string) error { return w.did("old: mw sync") }

func (w *moveWorld) OldBeadsCount(context.Context, []string) (int, error) {
	return w.oldBeads, w.did("old: bd count")
}

func (w *moveWorld) OldUnitInstalled(_ context.Context, _ []string, unit string) (bool, error) {
	return w.oldInstalled[unit], w.did("old: systemctl installed? " + unit)
}

func (w *moveWorld) OldStopUnit(_ context.Context, _ []string, unit string) (bool, error) {
	return true, w.did("old: systemctl --user stop " + unit)
}

func (w *moveWorld) OldDisableUnit(_ context.Context, _ []string, unit string) (bool, error) {
	return true, w.did("old: systemctl --user disable --now " + unit)
}

func (w *moveWorld) OldPush(context.Context, []string) error { return w.did("old: bd dolt push") }

func (w *moveWorld) Mirror(context.Context, []string) error { return w.did("old: mw postern mirror") }

// planned is a move, told to be planned, of a home whose old end answers.
func planned(w *moveWorld, out *bytes.Buffer) application.HomeMove {
	w.oldHomeUp = true
	m := move(w, out)
	m.Old = w
	m.Planned = true
	m.OldHomeDead = false
	return m
}

func TestAPlannedMoveStandsTheOldHomeDownBeforeTouchingTheNewOne(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}

	if err := planned(w, out).Run(context.Background()); err != nil {
		t.Fatalf("planned move: %v\n%s", err, out)
	}

	wantCalls(t, w,
		// 1. the old home answers
		"ssh desktop",
		// 2. the old home stands down: hand off, flush, stop the writers
		"old: mail mayor",
		"old: wait for the Mayor to hand off",
		"old: mw sync",
		"old: systemctl installed? postern-backend",
		"old: systemctl --user stop postern-backend",
		"old: mw postern mirror",
		"old: bd count",
		"old: systemctl installed? dolt-beads",
		"old: bd dolt push",
		"old: systemctl --user disable --now dolt-beads",
		// 3. beads, from the old home's final push
		"git: read refs/dolt/data time",
		"host lock taken",
		"aside .beads/embeddeddolt",
		"bd bootstrap --yes",
		"git checkout -- .beads/config.yaml",
		"bd count",
		"systemctl installed? dolt-beads",
		"systemctl --user start dolt-beads",
		"bd count",
		"host lock released",
		// 4. the vault
		"git pull",
		"write home",
		"git commit home",
		"git rev-parse HEAD",
		"git push",
		// 5. postern
		"systemctl installed? postern-backend",
		"systemctl --user start postern-backend",
		"healthz http://laptop.mw:8787",
		// 6. the Mayor
		"mail mayor",
		"bin/mayor-up",
		// 7. what was lost
		"postern index",
	)
	mustContain(t, "hand-off mail", w.handoffMail, "Hand off now: the home moves to laptop")
	mustContain(t, "report", out.String(),
		"Step 1 of 7", "Step 2 of 7", "Step 7 of 7", "Way back:", "Home is now laptop")
}

func TestAPlannedMovesMailToTheNewMayorSaysPlannedMove(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}

	if err := planned(w, out).Run(context.Background()); err != nil {
		t.Fatalf("planned move: %v\n%s", err, out)
	}

	mail, err := w.FakeMailbox.Inbox(context.Background(), "mayor")
	if err != nil || len(mail) != 1 {
		t.Fatalf("expected one message to the Mayor here, got %v, %v", mail, err)
	}
	mustContain(t, "mail", mail[0].Body, "planned move", "2026-09-29T13:37:19Z")
	if strings.Contains(mail[0].Body, "dead") {
		t.Errorf("a planned move's mail says the old home is dead: %s", mail[0].Body)
	}
	mustContain(t, "report", out.String(), "planned move", "nothing is lost")
}

func TestAMayorThatDoesNotHandOffStopsThePlannedMoveWithTheNewHomeUntouched(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.mayorStays = true

	err := planned(w, out).Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "step 2") || !strings.Contains(err.Error(), "did not hand off") {
		t.Fatalf("expected the move to stop at step 2, saying the Mayor did not hand off, got %v", err)
	}
	// Only the old home was touched, and only by the mail; it is never killed or synced.
	wantCalls(t, w, "ssh desktop", "old: mail mayor", "old: wait for the Mayor to hand off")
	mustContain(t, "error", err.Error(), "Mayor after handoff 98", "never killed")
	// The mail was sent, so its way back is printed: tell the Mayor to carry on.
	mustContain(t, "ways back", out.String(), "Ways back", "carry on")
	if inbox, _ := w.FakeMailbox.Inbox(context.Background(), "mayor"); len(inbox) != 0 {
		t.Errorf("mail reached the new home's Mayor: %v", inbox)
	}
}

func TestAPlannedMoveStopsWhereTheOldHomeStandDownFails(t *testing.T) {
	cases := []struct{ failAt, step, ran string }{
		{"old: mail mayor", "step 2", "old: mail mayor"},
		{"old: mw sync", "step 2", "old: mw sync"},
		{"old: systemctl --user stop postern-backend", "step 2", "old: systemctl --user stop postern-backend"},
		{"old: mw postern mirror", "step 2", "old: mw postern mirror"},
		{"old: bd count", "step 2", "old: bd count"},
		{"old: bd dolt push", "step 2", "old: bd dolt push"},
		{"old: systemctl --user disable --now dolt-beads", "step 2", "old: systemctl --user disable --now dolt-beads"},
	}
	for _, c := range cases {
		t.Run(c.failAt, func(t *testing.T) {
			w, out := newMoveWorld(), &bytes.Buffer{}
			w.failAt = c.failAt

			err := planned(w, out).Run(context.Background())

			if err == nil || !strings.Contains(err.Error(), c.step) {
				t.Fatalf("expected the move to stop at %s, got %v", c.step, err)
			}
			if last := w.calls[len(w.calls)-1]; last != c.ran {
				t.Errorf("expected nothing to run after %q, but %q did", c.ran, last)
			}
			for _, call := range w.calls {
				if !strings.HasPrefix(call, "old: ") && call != "ssh desktop" {
					t.Errorf("the new home was touched (%q) though the old home did not stand down", call)
				}
			}
		})
	}
}

func TestAPlannedMoveGivesTheWayBackOfWhatStoppedOnTheOldHome(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.failAt = "old: mw postern mirror"

	err := planned(w, out).Run(context.Background())

	if err == nil {
		t.Fatal("expected the move to stop")
	}
	mustContain(t, "ways back", out.String(), "Ways back",
		"ssh desktop systemctl --user start postern-backend", "carry on")
}

// The flush that matters is the last one: bd dolt push, after the writers have
// stopped and before dolt-beads is gone, since a server-mode push goes through
// the server. A push that fails leaves the new home untouched and gives the way
// back, which names enable --now for dolt-beads.
func TestAPlannedMoveThatCannotPushStopsOnTheOldHomeAndNamesTheWayBackToDoltBeads(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.failAt = "old: bd dolt push"

	err := planned(w, out).Run(context.Background())

	stopped, ok := application.HomeMoveStoppedIn(err)
	if !ok || stopped.Step != 2 {
		t.Fatalf("expected the move to stop at step 2, got %v", err)
	}
	for _, call := range w.calls {
		if !strings.HasPrefix(call, "old: ") && call != "ssh desktop" {
			t.Errorf("the new home was touched (%q) though the final push failed", call)
		}
		if strings.Contains(call, "disable --now dolt-beads") {
			t.Errorf("dolt-beads was disabled after a push that failed")
		}
	}
	mustContain(t, "error", err.Error(), "bd dolt push", "new home")
	mustContain(t, "ways back", out.String(), "Ways back", "ssh desktop systemctl --user enable --now dolt-beads")
}

func TestAPlannedMoveSkipsTheUnitsTheOldHomeDoesNotHave(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.oldInstalled[application.DoltBeadsUnit] = false

	if err := planned(w, out).Run(context.Background()); err != nil {
		t.Fatalf("planned move: %v\n%s", err, out)
	}
	for _, call := range w.calls {
		if call == "old: systemctl --user disable --now dolt-beads" {
			t.Errorf("disabled a unit the old home does not have")
		}
	}
	mustContain(t, "report", out.String(), "no dolt-beads unit on desktop")
}

func TestAPlannedMoveNeedsTheOldHomeUp(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	m := planned(w, out)
	w.oldHomeUp = false

	err := m.Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "--planned needs the old home up") || !strings.Contains(err.Error(), "--old-home-dead") {
		t.Fatalf("expected a refusal saying --planned needs the old home up, got %v", err)
	}
	wantCalls(t, w, "ssh desktop")
	mustContain(t, "error", err.Error(), "Nothing was changed")
}

func TestPlannedAndOldHomeDeadContradictAndRefuseBeforeRunningAnything(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	m := planned(w, out)
	m.OldHomeDead = true

	err := m.Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "--planned") || !strings.Contains(err.Error(), "--old-home-dead") {
		t.Fatalf("expected a refusal naming both flags, got %v", err)
	}
	wantCalls(t, w)
}

func TestAPlannedDryRunSaysTheOldHomeStepsAndRunsNone(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	m := planned(w, out)
	m.DryRun = true

	if err := m.Run(context.Background()); err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}

	wantCalls(t, w)
	mustContain(t, "dry run", out.String(),
		"Step 1 of 7", "Step 7 of 7", "Hand off now: the home moves to laptop", "15m0s", "mw sync",
		"mw postern mirror", "postern-backend", "dolt-beads", "bd dolt push", "disable --now", "enable --now", "planned move", "never killed")
	if n := strings.Count(out.String(), "Way back:"); n != 7 {
		t.Errorf("expected a way back for each of the seven steps, got %d:\n%s", n, out)
	}
}

func TestADeadMovesOldHomeUpIsStillRefusedAndPointsAtPlanned(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.oldHomeUp = true

	err := move(w, out).Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "old home is up: use --planned") {
		t.Fatalf("expected a refusal pointing at --planned, got %v", err)
	}
	wantCalls(t, w, "ssh desktop")
}

// A stale .beads/dolt on the new home is what dolt-beads would serve: the move
// compares the old home's count, taken after its final flush, with the new home's.
func TestAPlannedMoveThatFindsAStaleDoltDirectoryStopsAtTheBeadsStepWithBothCounts(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.hasDolt = true
	w.oldBeads, w.beads = 10, 7

	err := planned(w, out).Run(context.Background())

	stopped, ok := application.HomeMoveStoppedIn(err)
	if !ok || stopped.Step != 3 || !stopped.Changed {
		t.Fatalf("expected the move to stop at step 3 with things changed, got %v", err)
	}
	mustContain(t, "error", err.Error(), "7", "10", "old home", doltMoved.To, asideMoved.To)
	for _, call := range w.calls {
		if call == "write home" {
			t.Errorf("wrote the home file over a database that is not the old home's")
		}
	}
	mustContain(t, "ways back", out.String(), "The move stopped", "Ways back", doltMoved.To, doltMoved.From, asideMoved.To)
	if strings.Contains(out.String(), "Home is now laptop") {
		t.Errorf("a move that stopped said it finished:\n%s", out)
	}
}

func TestAPlannedMoveWhoseCountsAgreeGoesOnAndSaysTheyAgree(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.hasDolt = true
	w.oldBeads, w.beads = 4422, 4422

	if err := planned(w, out).Run(context.Background()); err != nil {
		t.Fatalf("planned move: %v\n%s", err, out)
	}
	mustContain(t, "report", out.String(), "the old home counted 4422", doltMoved.To)
}

func TestADeadMoveSaysThereIsNoOldCountToCompareAndNeverGuesses(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.oldBeads, w.beads = 10, 7 // not asked: the old home is dead

	if err := move(w, out).Run(context.Background()); err != nil {
		t.Fatalf("a dead move has no count to fail on: %v\n%s", err, out)
	}
	for _, call := range w.calls {
		if call == "old: bd count" {
			t.Errorf("asked a dead home for its count")
		}
	}
	mustContain(t, "report", out.String(), "no old count to compare", "dead")
}

func TestAnOldCountThatCannotBeTakenStopsThePlannedMoveOnTheOldHome(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.failAt = "old: bd count"

	err := planned(w, out).Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "step 2") {
		t.Fatalf("expected the move to stop at step 2, got %v", err)
	}
	for _, call := range w.calls {
		if !strings.HasPrefix(call, "old: ") && call != "ssh desktop" {
			t.Errorf("the new home was touched (%q) with no count to compare", call)
		}
	}
}
