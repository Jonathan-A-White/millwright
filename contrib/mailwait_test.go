package contrib_test

// This test drives contrib/mail-wait, retired in favour of `mw events wait`
// (mw-jrx0s.6), and the mail-notify tick's deference to `mw events follow`.
// It runs in the same throwaway world as mail-notify's test
// (mailnotify_test.go): a private tmux socket, a temporary vault and state
// directory, and stand-ins for bd, mw and systemctl on the front of PATH.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	cmd := exec.Command(script, "--kinds", "mail")
	cmd.Env = append([]string{
		"PATH=" + f.path("bin") + ":" + os.Getenv("PATH"),
		"HOME=" + f.dir,
		"MW_TEST_DIR=" + f.dir,
	}, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("mail-wait failed: %v\n%s", err, out)
	}
	return string(out)
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
	if out, err := exec.Command("sh", "-n", "mail-wait").CombinedOutput(); err != nil {
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
	f.typed("New mail for mayor: 1 message(s). Run bd mail inbox.\n")
}

func TestTheNotifierTypesTheMailWhenTheSeatsFileNamesNoMail(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-bbb")
	f.followerTells(`["message"]`)
	f.tick()
	f.typed("New mail for mayor: 1 message(s). Run bd mail inbox.\n")
}

func TestTheNotifierTypesTheMailWhenTheSeatHasNoSubscribeFile(t *testing.T) {
	f := newFactory(t)
	f.mayor("idle", actingByID)
	f.inbox("mw-ccc")
	f.write("view-follow-active", "", 0o644)
	f.tick()
	f.typed("New mail for mayor: 1 message(s). Run bd mail inbox.\n")
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

	f.typed("New mail for mayor: 1 message(s). Run bd mail inbox.\n")
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
