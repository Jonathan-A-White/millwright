package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.941: a refused story's kept claim holds its files from the stories
// ready beside it, and mw status said the claim was stranded and would be given
// back (it is kept, waiting on the Mayor), showed no waiting line under the
// story it held, and named no cycle when the fix waited behind what it fixes.

// aClaimOn files a story pathed to host, expecting to change files, claimed
// with a lease that ran out three hours ago, and refused when refused is set.
func aClaimOn(t *testing.T, tracker *apptest.FakeTracker, id, host, files string, refused bool) {
	t.Helper()
	story := domain.Story{ID: id, Title: "A claimed story"}
	if err := story.Overrides.Set("host", host); err != nil {
		t.Fatalf("pathing %s to %s: %v", id, host, err)
	}
	if err := story.Overrides.Set("files", files); err != nil {
		t.Fatalf("setting the files of %s: %v", id, err)
	}
	tracker.AddStory("mw-gq6", story)
	claim(t, tracker, id, 3*time.Hour)
	if refused {
		if err := tracker.SetStoryState(context.Background(), id, application.RunState, application.RunBlocked, "(landing-refused) main is red"); err != nil {
			t.Fatalf("refusing %s: %v", id, err)
		}
	}
}

func TestStatusSaysARefusedClaimOnAnotherHostIsKeptNotStranded(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.931", "laptop", "playwright.config.ts", true)
	syncedAt(t, tracker, "laptop", 5*time.Minute)

	block := flat(section(otherHostStatus(t, tracker)))
	if strings.Contains(block, "STRANDED") || strings.Contains(block, "gives the claim back") {
		t.Fatalf("expected a refused claim not called stranded, got:\n%s", block)
	}
	if !strings.Contains(block, "refused, waiting on the Mayor (mw retry or a hold)") {
		t.Fatalf("expected the refused line under the claim, got:\n%s", block)
	}
}

func TestStatusStillCallsAnUnrefusedClaimWithALapsedLeaseStrandedOnAnotherHost(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.931", "laptop", "playwright.config.ts", false)
	syncedAt(t, tracker, "laptop", 5*time.Minute)

	block := section(otherHostStatus(t, tracker))
	if !strings.Contains(block, "STRANDED: lease ran out") || strings.Contains(block, "refused") {
		t.Fatalf("expected the lapsed claim stranded and not refused, got:\n%s", block)
	}
}

// mw-gq6.950: a claim whose last run is recorded landed is never given back by
// dispatch: once its lease lapsed the host closes it (closeLanded).
func TestStatusSaysALandedClaimOnAnotherHostIsClosedNotGivenBack(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.931", "laptop", "playwright.config.ts", false)
	if err := tracker.SetStoryState(context.Background(), "mw-gq6.931", application.RunState, application.RunLanded, "landed on main"); err != nil {
		t.Fatalf("recording the landing: %v", err)
	}
	syncedAt(t, tracker, "laptop", 5*time.Minute)

	block := flat(section(otherHostStatus(t, tracker)))
	if strings.Contains(block, "gives the claim back") {
		t.Fatalf("expected a landed claim not to be promised given back, got:\n%s", block)
	}
	if !strings.Contains(block, "closes the story on its next dispatch tick") {
		t.Fatalf("expected the line to say dispatch closes the landed claim, got:\n%s", block)
	}
}

// On this host too, a landed claim with a lapsed lease is closed by the next
// dispatch tick, never given back.
func TestStatusSaysALandedClaimWithALapsedLeaseOnThisHostIsClosedNotGivenBack(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.931", "vps", "playwright.config.ts", false)
	if err := tracker.SetStoryState(context.Background(), "mw-gq6.931", application.RunState, application.RunLanded, "landed on main"); err != nil {
		t.Fatalf("recording the landing: %v", err)
	}

	printed := flat(otherHostStatus(t, tracker).String())
	if strings.Contains(printed, "gives the claim back") || !strings.Contains(printed, "closes the story on its next dispatch tick") {
		t.Fatalf("expected the landed claim to be closed, not given back, got:\n%s", printed)
	}
}

func TestStatusSaysAHandedBackClaimOnAnotherHostRunsItsCloseOutAgain(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.931", "laptop", "playwright.config.ts", false)
	if err := tracker.SetStoryState(context.Background(), "mw-gq6.931", application.RunState, application.RunCloseOutAgain, "merge slot"); err != nil {
		t.Fatalf("handing back: %v", err)
	}
	syncedAt(t, tracker, "laptop", 5*time.Minute)

	block := flat(section(otherHostStatus(t, tracker)))
	if strings.Contains(block, "STRANDED") || !strings.Contains(block, "runs its close-out alone again") {
		t.Fatalf("expected the handed-back line and no STRANDED, got:\n%s", block)
	}
}

func TestStatusSaysAReadyStoryWaitsOnAClaimOnAnotherHost(t *testing.T) {
	for _, refused := range []bool{false, true} {
		tracker := aTrackerPathedToVPS(t)
		aClaimOn(t, tracker, "mw-gq6.931", "laptop", "playwright.config.ts", refused)
		syncedAt(t, tracker, "laptop", 5*time.Minute)
		storyChanging(t, tracker.AddStory, "mw-gq6.936", "playwright.config.ts")

		printed := otherHostStatus(t, tracker).String()
		ready := printed[strings.Index(printed, "READY"):]
		ready = flat(ready[:strings.Index(ready, "\n\n")])
		if !strings.Contains(ready, "waiting: shares playwright.config.ts with mw-gq6.931 (running on laptop)") {
			t.Fatalf("refused=%v: expected the waiting line under mw-gq6.936, got:\n%s", refused, ready)
		}
		if got := strings.Contains(ready, "mw-gq6.931 is refused"); got != refused {
			t.Fatalf("refused=%v: expected the line to say so only for a refused holder, got:\n%s", refused, ready)
		}
	}
}

func TestStatusSaysAReadyStoryWaitsOnARefusedClaimOnThisHost(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.931", "vps", "playwright.config.ts", true)
	storyChanging(t, tracker.AddStory, "mw-gq6.936", "playwright.config.ts")

	printed := flat(otherHostStatus(t, tracker).String())
	if !strings.Contains(printed, "waiting: shares playwright.config.ts with mw-gq6.931; mw-gq6.931 is refused") {
		t.Fatalf("expected the waiting line to say the holder is refused, got:\n%s", printed)
	}
}

// flat is the text with the line breaks the report wraps it at made single spaces.
func flat(text string) string { return strings.Join(strings.Fields(text), " ") }

// damperLines are the lines of the DAMPERS section that name id.
func damperLines(report application.StatusReport, id string) []string {
	var lines []string
	for _, alarm := range report.Dampers.Alarms {
		if line := alarm.Line(); strings.Contains(line, id) {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestDampersNameTheCycleWhenTheFixWaitsBehindTheStoryItFixes(t *testing.T) {
	ctx := context.Background()
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.931", "laptop", "playwright.config.ts,src/a.ts", true)
	syncedAt(t, tracker, "laptop", 5*time.Minute)
	storyChanging(t, tracker.AddStory, "mw-gq6.936", "playwright.config.ts")
	if err := tracker.SetStoryMetadata(ctx, "mw-gq6.931", map[string]string{application.CauseFixField: "mw-gq6.936"}); err != nil {
		t.Fatalf("naming the fix: %v", err)
	}

	lines := damperLines(otherHostStatus(t, tracker), "mw-gq6.936")
	if len(lines) != 1 {
		t.Fatalf("expected one line naming the cycle, got %q", lines)
	}
	for _, want := range []string{"mw-gq6.931", "mw-gq6.936", "playwright.config.ts", "mw retry mw-gq6.931 --hold", "laptop"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("expected the cycle line to say %q, got %q", want, lines[0])
		}
	}
}

func TestDampersNameNoCycleWhenTheHolderIsNotRefused(t *testing.T) {
	ctx := context.Background()
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.931", "laptop", "playwright.config.ts", false)
	syncedAt(t, tracker, "laptop", 5*time.Minute)
	storyChanging(t, tracker.AddStory, "mw-gq6.936", "playwright.config.ts")
	if err := tracker.SetStoryMetadata(ctx, "mw-gq6.931", map[string]string{application.CauseFixField: "mw-gq6.936"}); err != nil {
		t.Fatalf("naming the fix: %v", err)
	}
	heldFor(t, tracker, "mw-gq6.936", 3*time.Hour)

	if lines := damperLines(otherHostStatus(t, tracker), "mw-gq6.936"); len(lines) != 0 {
		t.Fatalf("expected no line for a holder that is not refused, got %q", lines)
	}
}

func TestDampersNameAReadyStoryPassedOverBehindARefusedClaimForOverAnHour(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.754", "vps", "docs/paradigms.md", true)
	storyChanging(t, tracker.AddStory, "mw-gq6.1003", "docs/paradigms.md")
	storyChanging(t, tracker.AddStory, "mw-gq6.1005", "docs/paradigms.md")
	heldFor(t, tracker, "mw-gq6.1003", 2*time.Hour)
	heldFor(t, tracker, "mw-gq6.1005", 20*time.Minute)

	report := otherHostStatus(t, tracker)
	lines := damperLines(report, "mw-gq6.1003")
	if len(lines) != 1 {
		t.Fatalf("expected one line for the story held over an hour, got %q", lines)
	}
	for _, want := range []string{"mw-gq6.754", "mw-gq6.1003", "docs/paradigms.md", "mw retry mw-gq6.754 --hold", "vps"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("expected the line to say %q, got %q", want, lines[0])
		}
	}
	if lines := damperLines(report, "mw-gq6.1005"); len(lines) != 0 {
		t.Errorf("expected no line for a story held 20 minutes, got %q", lines)
	}
}

func TestDampersNameNoReadyStoryBehindAClaimThatIsNotRefused(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.754", "vps", "docs/paradigms.md", false)
	storyChanging(t, tracker.AddStory, "mw-gq6.1003", "docs/paradigms.md")
	heldFor(t, tracker, "mw-gq6.1003", 2*time.Hour)

	if lines := damperLines(otherHostStatus(t, tracker), "mw-gq6.1003"); len(lines) != 0 {
		t.Fatalf("expected no line, got %q", lines)
	}
}

// heldFor leaves the note vps's dispatch keeps of a story it has held for its
// files for the time given.
func heldFor(t *testing.T, tracker *apptest.FakeTracker, id string, ago time.Duration) {
	t.Helper()
	ctx := context.Background()
	held, _ := tracker.Note(ctx, application.FilesHeldKey("vps"))
	if held != "" {
		held += "\n"
	}
	held += id + " " + statusNow.Add(-ago).Format(time.RFC3339)
	if err := tracker.SetNote(ctx, application.FilesHeldKey("vps"), held); err != nil {
		t.Fatalf("keeping the files-held note: %v", err)
	}
}

func TestStatusSaysARefusedUnhostedClaimIsKeptNotGivenBack(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	anUnhostedClaim(t, tracker, "mw-gq6.50", "mw@desktop", statusNow, 7*time.Hour)
	anUnhostedClaim(t, tracker, "mw-gq6.51", "mw@desktop", statusNow, 7*time.Hour)
	if err := tracker.SetStoryState(context.Background(), "mw-gq6.50", application.RunState, application.RunBlocked, "(landing-refused) main is red"); err != nil {
		t.Fatalf("refusing mw-gq6.50: %v", err)
	}

	printed := flat(otherHostStatus(t, tracker).String())
	if strings.Count(printed, "next mw dispatch tick gives it back") != 1 {
		t.Fatalf("expected only the unrefused claim promised given back, got:\n%s", printed)
	}
	if !strings.Contains(printed, "refused, waiting on the Mayor (mw retry or a hold) mw retry mw-gq6.50 --hold") {
		t.Fatalf("expected the refused claim said kept, with its cure, got:\n%s", printed)
	}
}

// mw-gq6.954: a claim no host names whose last run landed was offered `mw retry`
// in the STUCK section, which works the story a second time on top of its own
// landed commits. A landed claim wants closing, and `mw next <id>` closes it.

// aLandedUnhostedClaim is a claim no host's Path names, lease lapsed seven
// hours ago, whose last run is recorded landed.
func aLandedUnhostedClaim(t *testing.T, tracker *apptest.FakeTracker, id string) {
	t.Helper()
	anUnhostedClaim(t, tracker, id, "mw@desktop", statusNow, 7*time.Hour)
	if err := tracker.SetStoryState(context.Background(), id, application.RunState, application.RunLanded, "landed on main"); err != nil {
		t.Fatalf("recording the landing of %s: %v", id, err)
	}
}

func TestStatusStuckLandedClaimOffersNoRetryAndNamesWhatClosesIt(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aLandedUnhostedClaim(t, tracker, "mw-gq6.50")

	printed := flat(otherHostStatus(t, tracker).String())
	if !strings.Contains(printed, "STUCK: claimed by mw@desktop") {
		t.Fatalf("expected the claim named STUCK, got:\n%s", printed)
	}
	if strings.Contains(printed, "mw retry") || strings.Contains(printed, "no dispatch tick gives back") {
		t.Fatalf("expected a landed claim offered no mw retry, got:\n%s", printed)
	}
	if !strings.Contains(printed, "mw next mw-gq6.50") {
		t.Fatalf("expected the line to name mw next as what closes it, got:\n%s", printed)
	}
}

// What the STUCK line names is run here against the same state: mw next closes
// the landed claim, without landing anything a second time.
func TestMwNextClosesAStuckLandedClaim(t *testing.T) {
	ctx := context.Background()
	tracker := aTrackerPathedToVPS(t)
	aLandedUnhostedClaim(t, tracker, "mw-gq6.50")
	lands := &fakeRetryLanding{}
	next := application.Next{
		Tracker: tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{},
		Seat: "builder", Host: "vps",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}

	report, err := next.Run(ctx, "mw-gq6.50")
	if err != nil || !report.Closed {
		t.Fatalf("expected mw next to close the landed claim, got %+v, %v", report, err)
	}
	if detail, _ := tracker.ShowStory(ctx, "mw-gq6.50"); detail.Status != apptest.StatusClosed {
		t.Fatalf("expected the story closed, got %q", detail.Status)
	}
}

// What mw retry did on a landed run: it gave the claim back, so the story was
// dispatched and worked again. It is refused now, with a line saying why.
func TestRetryRefusesAStoryWhoseLastRunLandedAndChangesNothing(t *testing.T) {
	for _, force := range []bool{false, true} {
		tracker := apptest.NewFakeTracker()
		aRetryStory(t, tracker, "mw-gq6.1")
		if err := tracker.SetStoryState(context.Background(), "mw-gq6.1", application.RunState, application.RunLanded, "landed on main"); err != nil {
			t.Fatal(err)
		}
		before, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
		if err != nil {
			t.Fatal(err)
		}
		landing := &fakeRetryLanding{AheadCount: 2, BundleSHA: "bbb", OriginTip: "4656ca5d"}
		var out strings.Builder
		retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
		retry.Force = force

		report, err := retry.Run(context.Background(), "mw-gq6.1")
		if err == nil || !report.Refused {
			t.Fatalf("force=%v: expected the retry refused, got %+v, %v", force, report, err)
		}
		for _, want := range []string{"landed", "mw next mw-gq6.1", "second time"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("force=%v: expected the refusal to say %q, got %q", force, want, err)
			}
		}
		after, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
		if err != nil {
			t.Fatal(err)
		}
		if after.Status != before.Status || after.Assignee != before.Assignee || after.Attempts != before.Attempts {
			t.Errorf("force=%v: expected the claim kept, was %+v now %+v", force, before, after)
		}
		if comments := tracker.Comments("mw-gq6.1"); len(comments) != 0 {
			t.Errorf("force=%v: expected no comment, got %q", force, comments)
		}
		if landing.bundled || landing.removed || landing.branchDeleted {
			t.Errorf("force=%v: expected nothing bundled, removed or deleted, got %+v", force, landing)
		}
	}
}

// The other run states' STUCK lines are as before: mw retry is the cure, and no
// dispatch tick gives the claim back.
func TestStatusStuckClaimInAnyOtherRunStateStillOffersRetry(t *testing.T) {
	for _, run := range []string{application.RunStopped, application.RunStuck, application.RunCancelled} {
		tracker := aTrackerPathedToVPS(t)
		anUnhostedClaim(t, tracker, "mw-gq6.50", "mw@desktop", statusNow, 7*time.Hour)
		if err := tracker.SetStoryState(context.Background(), "mw-gq6.50", application.RunState, run, "x"); err != nil {
			t.Fatal(err)
		}
		printed := flat(otherHostStatus(t, tracker).String())
		want := "its run state is " + run + ", which no dispatch tick gives back mw retry mw-gq6.50"
		if !strings.Contains(printed, want) {
			t.Errorf("run=%s: expected %q, got:\n%s", run, want, printed)
		}
	}
}

// The same sweep for OPEN BUT CLAIMED: a story open under a host's claim whose
// last run landed is closed by mw next; mw retry is refused for it.
func TestStatusOpenButClaimedLandedStoryOffersNoRetry(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	openButClaimed(t, tracker, "mw-gq6.50", "mw@laptop")
	if err := tracker.SetStoryState(context.Background(), "mw-gq6.50", application.RunState, application.RunLanded, "landed on main"); err != nil {
		t.Fatal(err)
	}

	printed := flat(otherHostStatus(t, tracker).String())
	if strings.Contains(printed, "mw retry mw-gq6.50") || !strings.Contains(printed, "mw next mw-gq6.50") {
		t.Fatalf("expected mw next and no mw retry, got:\n%s", printed)
	}
}
