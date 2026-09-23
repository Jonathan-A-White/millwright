package application_test

import (
	"context"
	"strings"
	"sync"
	"testing"

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

	UncommittedPaths []string
	UncommittedErr   error
	LeftoversSHA     string
	LeftoversErr     error

	BundleSHA string
	BundleErr error
	VerifyErr error

	RemoveErr error
	DeleteErr error

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

func (f *fakeRetryLanding) DeleteBranch(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	f.branchDeleted = true
	return nil
}

func (f *fakeRetryLanding) Ahead(context.Context, string, string, string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.AheadErr != nil {
		return 0, f.AheadErr
	}
	return f.AheadCount, nil
}

func (f *fakeRetryLanding) Uncommitted(context.Context, string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UncommittedErr != nil {
		return nil, f.UncommittedErr
	}
	return append([]string(nil), f.UncommittedPaths...), nil
}

func (f *fakeRetryLanding) CommitLeftovers(context.Context, string, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
func (f *fakeRetryLanding) Merge(context.Context, string, string) (application.Landed, error) {
	return application.Landed{}, nil
}
func (f *fakeRetryLanding) Push(context.Context, string, string, string) error { return nil }
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
