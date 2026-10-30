package application_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// fakeRetryLanding is an in-memory application.Worktrees and application.Landing:
// only the methods a retry actually calls are given anything to configure, the
// rest are stubs a retry never reaches.
type fakeRetryLanding struct {
	mu sync.Mutex

	AheadCount int
	AheadErr   error

	// BehindCount is what Ahead says when it is asked the other way round, how
	// many commits the target branch has that a story's branch (base) lacks:
	// zero, a target that has not moved since the branch was cut (mw-gq6.552).
	BehindCount int

	// MergedTip is the tip MergedInto reports the story's branch has, contained
	// in the base; empty means the branch is absent or not merged.
	MergedTip string
	MergedErr error

	UncommittedPaths []string
	UncommittedErr   error
	LeftoversSHA     string
	LeftoversErr     error
	// leftoverMessages is every message CommitLeftovers was given.
	leftoverMessages []string

	BundleSHA string
	BundleErr error
	VerifyErr error

	RemoveErr error
	DeleteErr error

	// NoWorktree makes Exists say neither the worktree nor the branch is there.
	NoWorktree bool

	bundled, removed, branchDeleted bool
}

var (
	_ application.Worktrees = (*fakeRetryLanding)(nil)
	_ application.Landing   = (*fakeRetryLanding)(nil)
)

func (f *fakeRetryLanding) Fetch(context.Context, string) error { return nil }
func (f *fakeRetryLanding) Add(context.Context, string, string, string, string) error {
	return nil
}
func (f *fakeRetryLanding) Exists(context.Context, string, string, string) (bool, error) {
	return !f.NoWorktree, nil
}
func (f *fakeRetryLanding) Remove(context.Context, string, string, string) error { return nil }

func (f *fakeRetryLanding) RemoveWithoutForce(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.RemoveErr != nil {
		return f.RemoveErr
	}
	f.removed = true
	return nil
}

func (f *fakeRetryLanding) RenameBranch(context.Context, string, string, string) error { return nil }

func (f *fakeRetryLanding) DeleteBranch(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	f.branchDeleted = true
	return nil
}

func (f *fakeRetryLanding) Ahead(_ context.Context, _, _, base string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.AheadErr != nil {
		return 0, f.AheadErr
	}
	if strings.HasPrefix(base, application.BranchPrefix) {
		return f.BehindCount, nil
	}
	return f.AheadCount, nil
}

func (f *fakeRetryLanding) MergedInto(context.Context, string, string, string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.MergedErr != nil {
		return "", false, f.MergedErr
	}
	return f.MergedTip, f.MergedTip != "", nil
}

func (f *fakeRetryLanding) Uncommitted(context.Context, string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UncommittedErr != nil {
		return nil, f.UncommittedErr
	}
	return append([]string(nil), f.UncommittedPaths...), nil
}

func (f *fakeRetryLanding) CommitLeftovers(_ context.Context, _, message string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leftoverMessages = append(f.leftoverMessages, message)
	if f.LeftoversErr != nil {
		return "", f.LeftoversErr
	}
	return f.LeftoversSHA, nil
}

func (f *fakeRetryLanding) Commits(context.Context, string, string, string) ([]application.Commit, error) {
	return nil, nil
}
func (f *fakeRetryLanding) OpenLanding(context.Context, string, string) (string, error) {
	return "", nil
}
func (f *fakeRetryLanding) RefHead(context.Context, string, string) (string, error) { return "", nil }
func (f *fakeRetryLanding) UnionNotes(context.Context, string) error                { return nil }
func (f *fakeRetryLanding) Merge(context.Context, string, string) (application.Landed, error) {
	return application.Landed{}, nil
}
func (f *fakeRetryLanding) MergeUnion(context.Context, string, string, []string) (application.Landed, error) {
	return application.Landed{}, nil
}
func (f *fakeRetryLanding) BumpVersion(context.Context, string, application.VersionBump) (application.Bumped, error) {
	return application.Bumped{}, nil
}
func (f *fakeRetryLanding) Push(context.Context, string, string, string) error { return nil }
func (f *fakeRetryLanding) KeepOrigin(context.Context, string, string, string) (application.KeptOrigin, error) {
	return application.KeptOrigin{}, nil
}
func (f *fakeRetryLanding) CloseLanding(context.Context, string, string) error { return nil }

func (f *fakeRetryLanding) Bundle(context.Context, string, string, string, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.BundleErr != nil {
		return "", f.BundleErr
	}
	f.bundled = true
	return f.BundleSHA, nil
}

func (f *fakeRetryLanding) VerifyBundle(context.Context, string, string) error {
	return f.VerifyErr
}

func (f *fakeRetryLanding) Advance(context.Context, string, string, string) (application.Advanced, error) {
	return application.Advanced{}, nil
}

// aRetryStory sets up one open, claimed story on a fake tracker, attempted
// once, under a rig this host has checked out, the way a dispatch that
// started a session that later ended would leave it.
func aRetryStory(t *testing.T, tracker *apptest.FakeTracker, id string) {
	t.Helper()
	tracker.AddEpic("mw-gq6", domain.Path{
		Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "vps",
	})
	tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "The story " + id})
	if err := tracker.ClaimStory(context.Background(), id); err != nil {
		t.Fatalf("claiming %s: %v", id, err)
	}
	if err := tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.AttemptsField: "1"}); err != nil {
		t.Fatalf("setting attempts on %s: %v", id, err)
	}
}

// aCleanRemote is a remote on which host's worktree of the story holds nothing
// uncommitted, for a retry from another host that has nothing to refuse for.
func aCleanRemote(host string) *apptest.FakeWorktreeRemote {
	remote := apptest.NewFakeWorktreeRemote()
	remote.Holds(host, application.WorktreeState{})
	return remote
}

func aCleanDesktop() *apptest.FakeWorktreeRemote { return aCleanRemote("desktop") }

// aRetry builds a Retry wired to fakes, ready to run against the story
// aRetryStory set up.
func aRetry(tracker *apptest.FakeTracker, landing *fakeRetryLanding, files *apptest.FakeVaultFiles, out *strings.Builder) application.Retry {
	return application.Retry{
		Tracker:   tracker,
		Worktrees: landing,
		Landing:   landing,
		Runner:    apptest.NewFakeRunner(),
		Files:     files,
		Vault:     newFakeVault(),
		Host:      "vps",
		Rigs:      map[string]string{"millwright": "/rigs/millwright"},
		Out:       out,
	}
}

// TestRetryWithNoCommitsAheadSkipsTheBundleAndGivesTheClaimBack pins the fix
// for mw-gq6.101: a branch with nothing ahead of the target cannot be
// bundled (`git bundle create` refuses an empty bundle), so a retry of the
// very case a stop-and-mail-me condition produces — a session that ended
// having committed nothing — must skip the bundle rather than fail, and still
// go on to take the worktree and branch away and give the claim back.
func TestRetryWithNoCommitsAheadSkipsTheBundleAndGivesTheClaimBack(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	landing := &fakeRetryLanding{AheadCount: 0}
	files := &apptest.FakeVaultFiles{}
	var out strings.Builder

	report, err := aRetry(tracker, landing, files, &out).Run(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
	}

	if !report.NothingAhead {
		t.Errorf("expected NothingAhead, got %+v", report)
	}
	if report.Bundled || report.VaultPushed {
		t.Errorf("expected nothing bundled or pushed, got %+v", report)
	}
	if landing.bundled {
		t.Errorf("expected Bundle to never be called")
	}
	if !landing.removed || !landing.branchDeleted {
		t.Errorf("expected the worktree and branch to be taken away, got %+v", landing)
	}
	if len(files.Commits()) != 0 {
		t.Errorf("expected nothing committed in the vault, got %v", files.Commits())
	}

	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatalf("reading the story back: %v", err)
	}
	if detail.Status != application.StatusOpen || detail.Assignee != "" {
		t.Errorf("expected the story open and unassigned, got status %q assignee %q", detail.Status, detail.Assignee)
	}

	if !strings.Contains(out.String(), "nothing to keep") {
		t.Errorf("expected the printed report to say nothing to keep, got %q", out.String())
	}
	if strings.Contains(out.String(), "vault") {
		t.Errorf("expected no vault line when nothing was pushed, got %q", out.String())
	}
}

// TestRetryThatFailsPartwayReportsOnlyWhatItDid pins the second fault of
// mw-gq6.101: the report printed the plan, not what happened, before the
// error. A push that fails after the bundle already verified must leave the
// worktree, branch and claim untouched, and the printed report must say only
// that the bundle was made — not that the vault was pushed, the worktree and
// branch are gone, or the claim was given back.
// TestRetryOfAStoryWithNoWorktreeReadsNothingAndGivesTheClaimBack pins
// mw-gq6.296: a story refused at pour never had a worktree or branch cut, so a
// retry must not read one (git status on a missing directory fails) but give the
// claim back and say there was nothing to keep.
func TestRetryOfAStoryWithNoWorktreeReadsNothingAndGivesTheClaimBack(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	landing := &fakeRetryLanding{NoWorktree: true, UncommittedErr: context.DeadlineExceeded}
	files := &apptest.FakeVaultFiles{}
	var out strings.Builder

	report, err := aRetry(tracker, landing, files, &out).Run(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
	}
	if !report.NoWorktree || !report.ClaimReleased {
		t.Errorf("expected NoWorktree and ClaimReleased, got %+v", report)
	}
	if report.WorktreeGone || report.BranchGone || report.Bundled || landing.removed || landing.bundled {
		t.Errorf("expected no worktree or branch touched, got %+v", report)
	}
	if !strings.Contains(out.String(), "no worktree or branch") {
		t.Errorf("expected the report to say so, got %q", out.String())
	}
	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != application.StatusOpen || detail.Assignee != "" {
		t.Errorf("expected the story open and unassigned, got %q %q", detail.Status, detail.Assignee)
	}
}

// TestRetryHoldGivesBackTheClaimOfAStoryStoppedBeforeItsClaimWasGivenBack pins
// mw-gq6.597: a story mw next stopped before releaseOnStop gave its claim back
// stays in_progress, claimed by its host. mw retry --hold gives that claim back
// in one write that leaves the story held, never open, so no dispatch takes it.
func TestRetryHoldGivesBackTheClaimOfAStoryStoppedBeforeItsClaimWasGivenBack(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	landing := &fakeRetryLanding{NoWorktree: true}
	var out strings.Builder

	retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
	retry.Hold = true
	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
	}
	if !report.ClaimReleased || !report.Held {
		t.Errorf("expected the claim given back and the story held, got %+v", report)
	}
	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Held() || detail.Assignee != "" {
		t.Errorf("expected the story held and unassigned, got status %q assignee %q", detail.Status, detail.Assignee)
	}
	for _, call := range tracker.Asked() {
		if call == "ReleaseClaim" || call == "ReleaseClaimHeldBy" {
			t.Errorf("the story was opened on its way to held (%s): %v", call, tracker.Asked())
		}
	}
	if !strings.Contains(out.String(), "held, not open") {
		t.Errorf("expected the report to say the story is held, not open, got %q", out.String())
	}
	said := strings.Join(tracker.Comments("mw-gq6.1"), "\n")
	if !strings.Contains(said, "held, not open") || strings.Contains(said, "next dispatch tick takes it") {
		t.Errorf("expected a comment saying it is held, not open, and none that a dispatch takes it, got %q", said)
	}
}

// TestRetryHoldLeavesAHeldStoryWithNoClaimHeld is the retry run twice: the
// story is held and unclaimed already, and --hold leaves it so.
func TestRetryHoldLeavesAHeldStoryWithNoClaimHeld(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	landing := &fakeRetryLanding{NoWorktree: true}
	retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &strings.Builder{})
	retry.Hold = true
	for i := 0; i < 2; i++ {
		if _, err := retry.Run(context.Background(), "mw-gq6.1"); err != nil {
			t.Fatalf("run %d: expected the retry to succeed, got: %v", i+1, err)
		}
	}
	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Held() || detail.Assignee != "" {
		t.Errorf("expected the story held and unassigned, got status %q assignee %q", detail.Status, detail.Assignee)
	}
}

// aHeldUnclaimedRetryStory is a story an open-steps stop gave its claim back
// on, which the Mayor then held to amend it: deferred, no assignee (mw-gq6.611).
func aHeldUnclaimedRetryStory(t *testing.T, tracker *apptest.FakeTracker, id string) {
	t.Helper()
	aRetryStory(t, tracker, id)
	if err := tracker.HoldClaimHeldBy(context.Background(), id, ""); err != nil {
		t.Fatalf("holding %s: %v", id, err)
	}
}

// TestRetryOfAHeldStoryWithNoClaimOpensItAndSaysTheClaimWasAlreadyGivenBack pins
// mw-gq6.611: the claim was given back already (assignee empty), so the
// give-back guarded on mw@host mismatched on "" and the retry failed after it
// had written the RE-PATHED: comment, leaving the story held.
func TestRetryOfAHeldStoryWithNoClaimOpensItAndSaysTheClaimWasAlreadyGivenBack(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aHeldUnclaimedRetryStory(t, tracker, "mw-gq6.1")
	landing := &fakeRetryLanding{NoWorktree: true}
	var out strings.Builder

	retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
	retry.RePathed = "the acceptance criteria were amended"
	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
	}
	if !report.ClaimReleased || report.Held {
		t.Errorf("expected the retry to go on and leave the story open, got %+v", report)
	}
	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != application.StatusOpen || detail.Assignee != "" {
		t.Errorf("expected the story open and unassigned, got status %q assignee %q", detail.Status, detail.Assignee)
	}
	if !strings.Contains(out.String(), "the claim was already given back") {
		t.Errorf("expected the report to say the claim was already given back, got %q", out.String())
	}
	rePathed := 0
	for _, comment := range tracker.Comments("mw-gq6.1") {
		if strings.HasPrefix(comment, application.RePathedMark) {
			rePathed++
		}
	}
	if rePathed != 1 {
		t.Errorf("expected the RE-PATHED: comment once, got %d: %v", rePathed, tracker.Comments("mw-gq6.1"))
	}
}

// TestRetryHoldOfAHeldStoryWithNoClaimKeepsItHeldAndSaysSo is the --hold half.
func TestRetryHoldOfAHeldStoryWithNoClaimKeepsItHeldAndSaysSo(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aHeldUnclaimedRetryStory(t, tracker, "mw-gq6.1")
	landing := &fakeRetryLanding{NoWorktree: true}
	var out strings.Builder

	retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
	retry.Hold = true
	if _, err := retry.Run(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
	}
	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Held() || detail.Assignee != "" {
		t.Errorf("expected the story held and unassigned, got status %q assignee %q", detail.Status, detail.Assignee)
	}
	if !strings.Contains(out.String(), "the claim was already given back") {
		t.Errorf("expected the report to say the claim was already given back, got %q", out.String())
	}
}

// TestRetryOfAStoryAnotherAssigneeHoldsStillRefusesWhenTheGiveBackDoes: only an
// empty assignee is taken as given back; a story another assignee holds goes
// through the guarded give-back as before, and its refusal is the retry's.
func TestRetryOfAStoryAnotherAssigneeHoldsStillRefusesWhenTheGiveBackDoes(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aClaimedRetryStory(t, tracker, "mw-gq6.1", "mw@desktop", "vps")
	tracker.FailReleases(apptest.ReleaseFault{Err: errors.New(`assignee mismatch: held by "mw@other", expected "mw@desktop"`)})
	landing := &fakeRetryLanding{NoWorktree: true}
	var out strings.Builder

	retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
	retry.Reach = aCleanDesktop()
	_, err := retry.Run(context.Background(), "mw-gq6.1")
	if err == nil || !strings.Contains(err.Error(), "the claim could not be given back") {
		t.Fatalf("expected the give-back's refusal, got: %v", err)
	}
	if strings.Contains(out.String(), "already given back") {
		t.Errorf("a claimed story must not be taken as given back, got %q", out.String())
	}
}

func TestRetryThatFailsPartwayReportsOnlyWhatItDid(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	landing := &fakeRetryLanding{AheadCount: 1, BundleSHA: "deadbeef"}
	pushErr := errFailingPush
	files := &apptest.FakeVaultFiles{PushErr: pushErr}
	var out strings.Builder

	report, err := aRetry(tracker, landing, files, &out).Run(context.Background(), "mw-gq6.1")
	if err == nil {
		t.Fatalf("expected the retry to fail, got a report: %+v", report)
	}
	if report.Refused {
		t.Fatalf("expected the report to say it failed, not refused: %+v", report)
	}
	if !strings.Contains(err.Error(), "could not be pushed") {
		t.Errorf("expected the error to say the push failed, got %v", err)
	}

	if !report.Bundled {
		t.Errorf("expected the bundle to have completed, got %+v", report)
	}
	if report.VaultPushed || report.WorktreeGone || report.BranchGone || report.ClaimReleased {
		t.Errorf("expected nothing past the bundle to have happened, got %+v", report)
	}
	if landing.removed || landing.branchDeleted {
		t.Errorf("expected the worktree and branch to be left alone, got %+v", landing)
	}

	detail, err2 := tracker.ShowStory(context.Background(), "mw-gq6.1")
	if err2 != nil {
		t.Fatalf("reading the story back: %v", err2)
	}
	if detail.Assignee == "" {
		t.Errorf("expected the story to still be claimed, got %+v", detail)
	}

	printed := out.String()
	if !strings.Contains(printed, "\n  bundle") {
		t.Errorf("expected the printed report to name the bundle, got %q", printed)
	}
	for _, absent := range []string{"\n  vault", "\n  gone", "\n  open"} {
		if strings.Contains(printed, absent) {
			t.Errorf("expected the printed report to say nothing about %q, got %q", strings.TrimSpace(absent), printed)
		}
	}
}

// errFailingPush is a stand-in for a network fault Files.Push hits: the
// bundle is already committed in the vault's own clone, but pushing it to the
// vault's remote fails.
var errFailingPush = fakePushError{}

type fakePushError struct{}

func (fakePushError) Error() string { return "the vault's remote refused the connection" }

// TestRetryOfAHitlStoryGivesTheClaimBackAndNamesTheLabelAndTheCure pins
// mw-gq6.261: dispatch passes a story over while it carries hitl, so a retry
// that gave the claim back and said nothing left the story waiting for a tick
// that would never take it. The claim is still given back; the report says why
// the retry will not run and how to let it.
func TestRetryOfAHitlStoryGivesTheClaimBackAndNamesTheLabelAndTheCure(t *testing.T) {
	const cure = "hitl: dispatch passes this story over while it carries hitl; to let it run: bd update mw-gq6.1 --remove-label hitl"

	for _, elsewhere := range []bool{false, true} {
		name := "worked here"
		if elsewhere {
			name = "worked on another host"
		}
		t.Run(name, func(t *testing.T) {
			tracker := apptest.NewFakeTracker()
			aRetryStory(t, tracker, "mw-gq6.1")
			if err := tracker.SetLabels("mw-gq6.1", application.LabelHitl); err != nil {
				t.Fatalf("labelling the story: %v", err)
			}
			var out strings.Builder

			retry := aRetry(tracker, &fakeRetryLanding{AheadCount: 0}, &apptest.FakeVaultFiles{}, &out)
			if elsewhere {
				// The story is pathed to "vps", so a retry run elsewhere only hands it back.
				retry.Host = "desktop"
				retry.Reach = aCleanRemote("vps")
			}
			report, err := retry.Run(context.Background(), "mw-gq6.1")
			if err != nil {
				t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
			}
			if !report.ClaimReleased {
				t.Errorf("expected the claim given back, got %+v", report)
			}
			detail, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
			if err != nil {
				t.Fatalf("reading the story back: %v", err)
			}
			if detail.Status != application.StatusOpen || detail.Assignee != "" {
				t.Errorf("expected the story open and unassigned, got status %q assignee %q", detail.Status, detail.Assignee)
			}
			if !strings.Contains(out.String(), cure) {
				t.Errorf("expected the report to carry %q, got %q", cure, out.String())
			}
		})
	}
}

// TestRetryOfAStoryWithoutHitlPrintsNoHitlLine is the other half of
// mw-gq6.261: the warning is for the label alone.
func TestRetryOfAStoryWithoutHitlPrintsNoHitlLine(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	var out strings.Builder

	if _, err := aRetry(tracker, &fakeRetryLanding{AheadCount: 0}, &apptest.FakeVaultFiles{}, &out).Run(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "hitl") {
		t.Errorf("expected no line about hitl, got %q", out.String())
	}
}

// TestRetryPrintsTheAttemptTheNextDispatchWillRecord pins mw-gq6.336: the line
// "the next dispatch tick takes it as attempt N" is the story's attempts
// metadata plus one, the number the next dispatch records — not the bundled
// attempt's floor of 1 plus one. A Mayor's hand reset to attempts 0 leaves
// the next try attempt 1, however much was bundled before.
func TestRetryPrintsTheAttemptTheNextDispatchWillRecord(t *testing.T) {
	for _, tc := range []struct {
		attempts string
		want     string
	}{
		{"0", "attempt 1"},
		{"1", "attempt 2"},
		{"2", "attempt 3"},
	} {
		for _, ahead := range []int{0, 2} {
			tracker := apptest.NewFakeTracker()
			aRetryStory(t, tracker, "mw-gq6.1")
			if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.1", map[string]string{application.AttemptsField: tc.attempts}); err != nil {
				t.Fatal(err)
			}
			var out strings.Builder

			if _, err := aRetry(tracker, &fakeRetryLanding{AheadCount: ahead}, &apptest.FakeVaultFiles{}, &out).Run(context.Background(), "mw-gq6.1"); err != nil {
				t.Fatalf("attempts %s, ahead %d: expected the retry to succeed, got: %v\n%s", tc.attempts, ahead, err, out.String())
			}
			want := "takes it as " + tc.want
			if !strings.Contains(out.String(), want+"\n") {
				t.Errorf("attempts %s, ahead %d: expected the printed report to say %q, got %q", tc.attempts, ahead, want, out.String())
			}
			comments := strings.Join(tracker.Comments("mw-gq6.1"), "\n")
			if !strings.Contains(comments, want+" of ") {
				t.Errorf("attempts %s, ahead %d: expected the comment to say %q, got %q", tc.attempts, ahead, want, comments)
			}
		}
	}
}

// aClaimedRetryStory is aRetryStory with the claim held by holder instead of
// the fake's own actor, and the story pathed to pathHost, as a Mayor leaves it
// after changing the host key on an in-progress story (mw-gq6.364).
func aClaimedRetryStory(t *testing.T, tracker *apptest.FakeTracker, id, holder, pathHost string) {
	t.Helper()
	aRetryStory(t, tracker, id)
	if err := tracker.ClaimAs(id, holder, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("claiming %s as %s: %v", id, holder, err)
	}
	if err := tracker.SetStoryMetadata(context.Background(), id, map[string]string{"host": pathHost}); err != nil {
		t.Fatalf("pathing %s to %s: %v", id, pathHost, err)
	}
}

// TestRetryFromAHostThatDoesNotHoldTheClaimGivesItBackByItsActualAssignee pins
// the first half of mw-gq6.364: the host key names this host, but mw@desktop
// holds the claim, so the story was worked on the desktop. The retry hands the
// claim back guarded on the actual assignee and reads no worktree here.
func TestRetryFromAHostThatDoesNotHoldTheClaimGivesItBackByItsActualAssignee(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aClaimedRetryStory(t, tracker, "mw-gq6.1", "mw@desktop", "laptop")
	landing := &fakeRetryLanding{AheadCount: 2}
	var out strings.Builder

	retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
	retry.Host = "laptop"
	retry.Reach = aCleanDesktop()
	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
	}

	if report.Elsewhere != "desktop" || !report.ClaimReleased {
		t.Errorf("expected the claim given back with the story worked on desktop, got %+v", report)
	}
	if landing.bundled || landing.removed || landing.branchDeleted {
		t.Errorf("expected no worktree read or removed on laptop")
	}
	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != application.StatusOpen || detail.Assignee != "" {
		t.Errorf("expected the story open and unassigned, got status %q assignee %q", detail.Status, detail.Assignee)
	}
	if !strings.Contains(out.String(), "desktop") {
		t.Errorf("expected the report to name desktop, got %q", out.String())
	}
}

// TestRetryOnTheClaimingHostBundlesEvenWhenTheHostKeyNamesAnother is the other
// half of mw-gq6.364: mw@desktop holds the claim and the retry runs on the
// desktop, but the story's host key now says laptop. Where it was worked comes
// from the claim, so the desktop's worktree is bundled and cleared.
func TestRetryOnTheClaimingHostBundlesEvenWhenTheHostKeyNamesAnother(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aClaimedRetryStory(t, tracker, "mw-gq6.1", "mw@desktop", "laptop")
	landing := &fakeRetryLanding{AheadCount: 2, BundleSHA: "abc1234"}
	var out strings.Builder

	retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
	retry.Host = "desktop"
	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
	}

	if report.Elsewhere != "" {
		t.Errorf("expected the story read as worked here, got elsewhere %q", report.Elsewhere)
	}
	if !report.Bundled || !report.WorktreeGone || !report.BranchGone || !report.ClaimReleased {
		t.Errorf("expected the worktree bundled, taken away and the claim given back, got %+v", report)
	}
	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != application.StatusOpen || detail.Assignee != "" {
		t.Errorf("expected the story open and unassigned, got status %q assignee %q", detail.Status, detail.Assignee)
	}
}

// mw-1ljpo9.7: a story whose last attempt mw next refused at the gate or the
// merge, or capped, has the note that its next attempt continues the branch;
// mw retry keeps the branch for it and takes only the worktree away, and
// --from-main deletes the branch and clears the note, as before.
func TestRetryKeepsTheBranchTheNextAttemptContinuesUnlessFromMain(t *testing.T) {
	for _, fromMain := range []bool{false, true} {
		tracker := apptest.NewFakeTracker()
		aRetryStory(t, tracker, "mw-gq6.1")
		ctx := context.Background()
		if err := tracker.SetNote(ctx, application.ContinueKey("mw-gq6.1"), "1"); err != nil {
			t.Fatal(err)
		}
		landing := &fakeRetryLanding{AheadCount: 2, BundleSHA: "abc123"}
		var out strings.Builder
		retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
		retry.Notes, retry.FromMain = tracker, fromMain

		report, err := retry.Run(ctx, "mw-gq6.1")
		if err != nil {
			t.Fatalf("from-main=%v: expected the retry to succeed, got: %v\n%s", fromMain, err, out.String())
		}
		if !landing.removed || !report.WorktreeGone {
			t.Errorf("from-main=%v: expected the worktree taken away, got %+v", fromMain, report)
		}
		note, _ := tracker.Note(ctx, application.ContinueKey("mw-gq6.1"))
		if fromMain {
			if !landing.branchDeleted || report.BranchKept || note != "" {
				t.Errorf("--from-main: expected the branch deleted and the note cleared, got deleted=%v kept=%v note=%q",
					landing.branchDeleted, report.BranchKept, note)
			}
			continue
		}
		if landing.branchDeleted || !report.BranchKept || report.BranchGone || note != "1" {
			t.Errorf("expected the branch kept and the note left for the dispatch, got deleted=%v kept=%v note=%q",
				landing.branchDeleted, report.BranchKept, note)
		}
		if !strings.Contains(out.String(), "the next attempt continues it") {
			t.Errorf("expected the report to say the branch is kept for the next attempt, got:\n%s", out.String())
		}
		comments := tracker.Comments("mw-gq6.1")
		if len(comments) == 0 || !strings.Contains(comments[len(comments)-1], "mw retry --from-main starts it from main instead") {
			t.Errorf("expected the comment to name the branch kept and --from-main, got %+v", comments)
		}
	}
}

// Without the note, an attempt refused for what is in its commits themselves (a
// signed commit, a tracked binary) has its branch deleted as before: the next
// attempt must not carry that on.
func TestRetryOfAnAttemptRefusedForItsCommitsDeletesTheBranch(t *testing.T) {
	for _, reason := range []application.Reason{application.ReasonSignedCommit, application.ReasonTrackedBinary} {
		tracker := apptest.NewFakeTracker()
		aRetryStory(t, tracker, "mw-gq6.1")
		ctx := context.Background()
		aRefusalOf(t, tracker, "mw-gq6.1", reason)
		landing := &fakeRetryLanding{AheadCount: 2, BundleSHA: "abc123"}
		var out strings.Builder
		retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
		retry.Notes = tracker
		report, err := retry.Run(ctx, "mw-gq6.1")
		if err != nil {
			t.Fatalf("%s: expected the retry to succeed, got: %v\n%s", reason, err, out.String())
		}
		if !landing.branchDeleted || report.BranchKept {
			t.Errorf("%s: expected the branch deleted, got deleted=%v kept=%v", reason, landing.branchDeleted, report.BranchKept)
		}
		if note, _ := tracker.Note(ctx, application.ContinueKey("mw-gq6.1")); note != "" {
			t.Errorf("%s: expected no note that the next attempt continues, got %q", reason, note)
		}
	}
}

// aRefusalOf records on the story what mw next records when it refuses a
// landing for reason: the blocked run state and the comment naming the reason.
func aRefusalOf(t *testing.T, tracker *apptest.FakeTracker, id string, reason application.Reason) {
	t.Helper()
	ctx := context.Background()
	if err := tracker.CommentOnStory(ctx, id, "mw next on vps "+application.RefusedPhrase+string(reason)+"): a commit is signed\n\nNothing was merged."); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryState(ctx, id, application.RunState, application.RunBlocked, "("+string(reason)+") a commit is signed"); err != nil {
		t.Fatal(err)
	}
}

// mw-gq6.537: an attempt that ended with work left, its close-out killed or its
// session dead, wrote no note, but its work is most of the story: mw retry
// commits what the worktree holds, bundles it, keeps the branch and leaves the
// note the dispatch that takes the story again reads, as for a refused one.
func TestRetryKeepsTheBranchOfAnAttemptThatDiedWithWorkLeft(t *testing.T) {
	cases := []struct {
		name      string
		uncommit  []string
		ahead     int
		fromMain  bool
		notes     bool
		refusedAs application.Reason
		wantKept  bool
	}{
		{name: "uncommitted work, committed by the retry", uncommit: []string{"a.go"}, ahead: 1, notes: true, wantKept: true},
		{name: "commits ahead of the target", ahead: 3, notes: true, wantKept: true},
		{name: "refused as no-result with commits", ahead: 2, notes: true, refusedAs: application.ReasonNoResult, wantKept: true},
		{name: "nothing ahead", ahead: 0, notes: true, wantKept: false},
		{name: "--from-main", ahead: 3, notes: true, fromMain: true, wantKept: false},
		{name: "no notes to leave the hand-on in", ahead: 3, notes: false, wantKept: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker := apptest.NewFakeTracker()
			aRetryStory(t, tracker, "mw-gq6.1")
			ctx := context.Background()
			if tc.refusedAs != "" {
				aRefusalOf(t, tracker, "mw-gq6.1", tc.refusedAs)
			}
			landing := &fakeRetryLanding{AheadCount: tc.ahead, BundleSHA: "abc123", UncommittedPaths: tc.uncommit, LeftoversSHA: "def456"}
			var out strings.Builder
			retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
			retry.FromMain = tc.fromMain
			if tc.notes {
				retry.Notes = tracker
			}
			report, err := retry.Run(ctx, "mw-gq6.1")
			if err != nil {
				t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
			}
			if report.CommittedLeftovers != (len(tc.uncommit) > 0) {
				t.Errorf("CommittedLeftovers = %v with %v uncommitted", report.CommittedLeftovers, tc.uncommit)
			}
			if report.Bundled != (tc.ahead > 0) {
				t.Errorf("Bundled = %v with %d ahead", report.Bundled, tc.ahead)
			}
			if report.BranchKept != tc.wantKept || landing.branchDeleted == tc.wantKept || report.BranchGone == tc.wantKept {
				t.Errorf("want kept=%v, got kept=%v gone=%v deleted=%v", tc.wantKept, report.BranchKept, report.BranchGone, landing.branchDeleted)
			}
			note, _ := tracker.Note(ctx, application.ContinueKey("mw-gq6.1"))
			if tc.wantKept && note != "1" {
				t.Errorf("expected the note that the next attempt continues attempt 1, got %q", note)
			}
			if !tc.wantKept && note != "" {
				t.Errorf("expected no note, got %q", note)
			}
			if tc.wantKept && !strings.Contains(out.String(), "the next attempt continues it") {
				t.Errorf("expected the report to say the branch is kept for the next attempt, got:\n%s", out.String())
			}
		})
	}
}

// A note that cannot be left does not stop the retry or hide it: the branch is
// kept, and the report says the next attempt starts from the target.
func TestRetryKeepingABranchSaysSoWhenItCannotLeaveTheNote(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	landing := &fakeRetryLanding{AheadCount: 2, BundleSHA: "abc123"}
	var out strings.Builder
	retry := aRetry(tracker, landing, &apptest.FakeVaultFiles{}, &out)
	retry.Notes = failingSetNote{tracker}
	report, err := retry.Run(context.Background(), "mw-gq6.1")
	if err != nil {
		t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
	}
	if landing.branchDeleted || !report.ClaimReleased || report.NoteLeft == "" {
		t.Errorf("expected the branch kept, the claim given back and the note failure said, got %+v", report)
	}
	if !strings.Contains(out.String(), "could not be written") {
		t.Errorf("expected the report to say the note was not written, got:\n%s", out.String())
	}
}

// failingSetNote is a tracker whose SetNote always fails.
type failingSetNote struct{ *apptest.FakeTracker }

func (failingSetNote) SetNote(context.Context, string, string) error {
	return errors.New("the store is down")
}

func TestOnlyTheGateTheMergeAndTheTurnCapHandABranchOn(t *testing.T) {
	for reason, want := range map[application.Reason]bool{
		application.ReasonTestsFail: true, application.ReasonMergedTestsFail: true, application.ReasonMergeConflict: true,
		application.ReasonTurnCap: true, application.ReasonOpenSteps: true, application.ReasonNoClosingComment: true, application.ReasonLockfileSSH: true, application.ReasonTOCStale: true, application.ReasonGristExampleUnproved: true,
		application.ReasonUnmetCriterion: false, application.ReasonSignedCommit: false, application.ReasonTrackedBinary: false, application.ReasonNoCommits: false, application.ReasonUncommittedWork: false,
	} {
		if got := application.ContinuesFrom(reason); got != want {
			t.Errorf("ContinuesFrom(%s) = %v, want %v", reason, got, want)
		}
	}
}

// A --from-main retry refused for a session still running changes nothing: the
// note that the next attempt continues the branch is still there.
func TestRetryFromMainRefusedLeavesTheNote(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	ctx := context.Background()
	if err := tracker.SetNote(ctx, application.ContinueKey("mw-gq6.1"), "1"); err != nil {
		t.Fatal(err)
	}
	runner := apptest.NewFakeRunner()
	if err := runner.Start(ctx, application.SessionSpec{Name: application.SessionName("mw-gq6.1"), Dir: "/w", Command: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	retry := aRetry(tracker, &fakeRetryLanding{AheadCount: 2}, &apptest.FakeVaultFiles{}, &out)
	retry.Notes, retry.FromMain, retry.Runner = tracker, true, runner

	report, _ := retry.Run(ctx, "mw-gq6.1")
	if !report.Refused {
		t.Fatalf("expected the retry refused for a running session, got %+v", report)
	}
	if note, _ := tracker.Note(ctx, application.ContinueKey("mw-gq6.1")); note != "1" {
		t.Errorf("expected the note left as it was by a refusal, got %q", note)
	}
}

// closesAfterFirstRead is a tracker whose story is closed once the retry has
// read it, as the Mayor's bd close would between the retry's reading of the
// story and its giving the claim back (mw-gq6.854).
type closesAfterFirstRead struct {
	*apptest.FakeTracker
	shown int
}

func (c *closesAfterFirstRead) ShowStory(ctx context.Context, id string) (application.StoryDetail, error) {
	detail, err := c.FakeTracker.ShowStory(ctx, id)
	if c.shown++; c.shown == 1 && err == nil {
		err = c.FakeTracker.CloseStory(ctx, id, "Superseded: split by the Mayor")
	}
	return detail, err
}

// TestRetryOfAStoryClosedWhileItRunsLeavesItClosedAndSaysSo pins mw-gq6.854:
// the give-back sets a story open, so a story closed after the retry read it
// was reopened, and its comment said the next dispatch tick takes it again.
// Whichever way the retry reads the worktree, a closed story stays closed, with
// no claim given back and a comment that says so.
func TestRetryOfAStoryClosedWhileItRunsLeavesItClosedAndSaysSo(t *testing.T) {
	cases := []struct {
		name  string
		hold  bool
		where string // the host the story was worked on, "" for this one
		land  *fakeRetryLanding
	}{
		{name: "no worktree", land: &fakeRetryLanding{NoWorktree: true}},
		{name: "no worktree, held", hold: true, land: &fakeRetryLanding{NoWorktree: true}},
		{name: "nothing ahead", land: &fakeRetryLanding{}},
		{name: "another host", where: "desktop", land: &fakeRetryLanding{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := apptest.NewFakeTracker()
			holder := ""
			if tc.where != "" {
				// The holder's own close, the only one bd lets through.
				holder = "mw@" + tc.where
				fake.ActAs(holder)
				aClaimedRetryStory(t, fake, "mw-gq6.1", holder, "laptop")
			} else {
				aRetryStory(t, fake, "mw-gq6.1")
			}
			tracker := &closesAfterFirstRead{FakeTracker: fake}
			var out strings.Builder
			retry := aRetry(fake, tc.land, &apptest.FakeVaultFiles{}, &out)
			retry.Tracker = tracker
			retry.Hold = tc.hold
			if tc.where != "" {
				retry.Host = "laptop"
				retry.Reach = aCleanRemote(tc.where)
			}

			report, err := retry.Run(context.Background(), "mw-gq6.1")
			if err != nil {
				t.Fatalf("expected the retry to succeed, got: %v\n%s", err, out.String())
			}
			if !report.Closed || report.ClaimReleased {
				t.Errorf("expected Closed and no claim given back, got %+v", report)
			}
			detail, err := fake.ShowStory(context.Background(), "mw-gq6.1")
			if err != nil {
				t.Fatal(err)
			}
			if !detail.Closed() {
				t.Errorf("expected the story closed still, got %q", detail.Status)
			}
			comments := fake.Comments("mw-gq6.1")
			if len(comments) != 1 {
				t.Fatalf("expected one comment, got %q", comments)
			}
			for _, text := range []string{out.String(), comments[0]} {
				if !strings.Contains(text, "stays closed") || !strings.Contains(text, "no dispatch tick takes it") {
					t.Errorf("expected it to say the story stays closed and is not dispatched, got %q", text)
				}
				for _, wrong := range []string{"open again", "takes it as", "held, not open"} {
					if strings.Contains(text, wrong) {
						t.Errorf("expected no %q for a closed story, got %q", wrong, text)
					}
				}
			}
		})
	}
}
