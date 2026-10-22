package contrib_test

// This test drives contrib/mail-wait, retired in favour of `mw events wait`
// (mw-jrx0s.6), and the mail-notify tick's deference to `mw events follow`.
// It runs in the same throwaway world as mail-notify's test
// (mailnotify_test.go): a private tmux socket, a temporary vault and state
// directory, and stand-ins for bd, mw and systemctl on the front of PATH.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// retiredWait runs contrib/mail-wait in the factory's world and returns what
// it printed.
func (f *factory) retiredWait(env ...string) string {
	f.t.Helper()
	script, err := filepath.Abs("mail-wait")
	if err != nil {
		f.t.Fatal(err)
	}
	// A stand-in that is "Text file busy" for a moment fails the run at once
	// with that message and no mw run: it is run again, a few times.
	for attempt := 0; ; attempt++ {
		cmd := command(f.t, script, "--kinds", "mail")
		cmd.Env = append([]string{
			f.pathEnv(),
			"HOME=" + f.dir,
			"MW_TEST_DIR=" + f.dir,
		}, env...)
		out, err := cmd.CombinedOutput()
		if err != nil && attempt < 20 && strings.Contains(string(out), "Text file busy") {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if err != nil {
			f.t.Fatalf("mail-wait failed: %v\n%s", err, out)
		}
		return string(out)
	}
}

func TestTheWaitIsRetiredAndRunsMwEventsWaitForTheMayor(t *testing.T) {
	f := newFactory(t)
	out := f.retiredWait()
	if out != "retired: mw events wait\n" {
		t.Fatalf("mail-wait printed %q, want it to say it is retired", out)
	}
	if got := f.read("events.log"); got != "events wait --for mayor --kinds mail\n" {
		t.Fatalf("mail-wait ran mw %q, want events wait for the mayor, its arguments passed on", got)
	}
}

func TestTheRetiredWaitKeepsItsMailboxAndLimitSettings(t *testing.T) {
	f := newFactory(t)
	f.retiredWait("MW_MAIL_MAILBOX=deputy", "MW_MAIL_WAIT_LIMIT=600")
	if got := f.read("events.log"); got != "events wait --for deputy --limit 600s --kinds mail\n" {
		t.Fatalf("mail-wait ran mw %q", got)
	}
}

func TestTheWaitScriptIsExecutableAndParses(t *testing.T) {
	info, err := os.Stat("mail-wait")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("contrib/mail-wait is not executable")
	}
	if out, err := command(t, "sh", "-n", "mail-wait").CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
}

// followerTells puts the Mayor's subscribe file in the vault and says
// mw-view-follow.service is active.
func (f *factory) followerTells(kinds string) {
	f.t.Helper()
	f.write("vault/seats/mayor/subscribe.toml", "kinds = "+kinds+"\n", 0o644)
	f.write("view-follow-active", "", 0o644)
}

func TestTheNotifierLeavesTheMailLineToTheFollowerWhenItIsActiveAndTheSeatSubscribes(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.followerTells(`["mail", "message"]`)

	f.tick()

	f.nothingMoreTyped("")
	if got := f.announced(); got != "" {
		t.Fatalf("recorded %q for mail the follower is to tell", got)
	}
}

func TestTheNotifierStillTypesTheQuietAlarmWhenTheFollowerTellsTheMail(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.nudges([2]string{"long", "story mw-x has run long"})
	f.followerTells(`["mail"]`)

	f.tick()

	f.typed("Quiet alarm for mayor: story mw-x has run long. Run mw status.\n")
}

func TestTheNotifierTypesTheMailWhenTheFollowerIsNotActive(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.write("vault/seats/mayor/subscribe.toml", "kinds = [\"mail\"]\n", 0o644)
	f.tick()
	f.typed(announce("mw-aaa"))
}

func TestTheNotifierTypesTheMailWhenTheSeatsFileNamesNoMail(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-bbb")
	f.followerTells(`["message"]`)
	f.tick()
	f.typed(announce("mw-bbb"))
}

func TestTheNotifierTypesTheMailWhenTheSeatHasNoSubscribeFile(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-ccc")
	f.write("view-follow-active", "", 0o644)
	f.tick()
	f.typed(announce("mw-ccc"))
}

// heartbeat marks a wait as armed just now, or ago before it.
func (f *factory) armedWait(ago time.Duration) {
	f.t.Helper()
	f.write("state/wait-armed", strconv.FormatInt(time.Now().Add(-ago).Unix(), 10)+"\n", 0o644)
}

func TestTheNotifierLeavesMailToAnArmedWaitEvenOnAnIdlePrompt(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.armedWait(10 * time.Second)

	f.tick()

	f.nothingMoreTyped("")
	if got := f.announced(); got != "" {
		t.Fatalf("recorded %q for mail the wait has yet to announce", got)
	}
}

func TestTheNotifierLeavesAPosternRiseToAnArmedWait(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.posternKey()
	f.posternCount(2)
	f.armedWait(10 * time.Second)

	f.tick()

	f.nothingMoreTyped("")
	if got := f.read("state/postern-count"); got != "" {
		t.Fatalf("noted postern count %q the wait has yet to announce", got)
	}
}

func TestTheNotifierFallsBackToTypingWhenTheWaitsHeartbeatIsStale(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.armedWait(10 * time.Minute)

	f.tick()

	f.typed(announce("mw-aaa"))
}

func TestTheNotifierStillTypesTheQuietAlarmWhileAWaitIsArmed(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-aaa")
	f.nudges([2]string{"long", "story mw-x has run long"})
	f.armedWait(10 * time.Second)

	f.tick()

	f.typed("Quiet alarm for mayor: story mw-x has run long. Run mw status.\n")
}

// pathDirs is the PATH a factory's scripts run with, split.
func (f *factory) pathDirs() []string {
	return strings.Split(strings.TrimPrefix(f.pathEnv(), "PATH="), ":")
}

func TestTheFactorysPathHoldsNoMwOrBdButItsOwn(t *testing.T) {
	// A stand-in that is briefly "Text file busy" (a parallel test's fork holds
	// a copy of the descriptor that wrote it) is skipped by dash, which goes on
	// down PATH to the next mw it finds: the host's real one, whose `events
	// wait` blocks for 50 minutes (mw-gq6.700).
	f := newFactory(t)
	for _, dir := range f.pathDirs() {
		if dir == f.path("bin") {
			continue
		}
		for _, program := range []string{"mw", "bd"} {
			if _, err := os.Stat(filepath.Join(dir, program)); err == nil {
				t.Errorf("the factory's PATH holds %s", filepath.Join(dir, program))
			}
		}
	}
}

func TestTheWaitRunsAgainWhenItsStandInIsBusyForAMoment(t *testing.T) {
	f := newFactory(t)
	// Hold bin/mw open for writing, as a forked child of another test does.
	busy, err := os.OpenFile(f.path("bin/mw"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(300*time.Millisecond, func() { busy.Close() })
	t.Cleanup(func() { busy.Close() })

	out := f.retiredWait()

	if out != "retired: mw events wait\n" {
		t.Fatalf("mail-wait printed %q", out)
	}
	if got := f.read("events.log"); got != "events wait --for mayor --kinds mail\n" {
		t.Fatalf("mail-wait ran mw %q", got)
	}
}
