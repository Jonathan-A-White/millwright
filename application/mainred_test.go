package application_test

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestTestNamesOfNamesTheTopLevelTestsThatFailed(t *testing.T) {
	cases := map[string]struct {
		output string
		want   []string
	}{
		"go test, a subtest and its parent once": {
			"--- FAIL: TestScorerScoresTheFixtureClips (21.93s)\n    --- FAIL: TestScorerScoresTheFixtureClips/clip_1 (1.00s)\n--- FAIL: TestOther (0.00s)\nFAIL\tgithub.com/x/contrib\t64.042s\n",
			[]string{"TestScorerScoresTheFixtureClips", "TestOther"},
		},
		"none": {"ok  \tgithub.com/x/y\t1.2s\n", nil},
		// mw-gq6.934: a name the rig's line can run alone on the target.
		"playwright list reporter, project and column left out": {
			"  \u2718  1 [shots] \u203a tests/e2e/keyboard-composer.spec.ts:106:7 \u203a composer keeps focus (31.0s)\n",
			[]string{"tests/e2e/keyboard-composer.spec.ts:106"},
		},
		"playwright retries and the summary name a test once": {
			"  \u2718  1 [shots] \u203a tests/e2e/a.spec.ts:12:5 \u203a one (1.0s)\n  \u2718  2 [shots] \u203a tests/e2e/a.spec.ts:12:5 \u203a one (retry #1) (1.0s)\n  \u2718  3 [shots] \u203a tests/e2e/b.spec.ts:7:3 \u203a two (2.0s)\n",
			[]string{"tests/e2e/a.spec.ts:12", "tests/e2e/b.spec.ts:7"},
		},
		"vitest, two tests of one file run as the file": {
			"FAIL  src/a.test.ts > does a thing\nFAIL  src/a.test.ts > another > nested\nFAIL  src/b.test.tsx > b\n",
			[]string{"src/a.test.ts", "src/b.test.tsx"},
		},
		// mw-gq6.1569: vitest's `×` line names the test by its title, which a
		// re-run took as an argument; `--list` in a title was read as an option.
		"vitest, the × line and the FAIL line of one test name the file only": {
			"  × lists Genesis 27 with 'Peradventure', as lxx:align-choose --list --book gen --chapter 27 prints it 12ms\n" +
				"FAIL  tests/unit/x.test.ts > lxx > lists Genesis 27 with 'Peradventure', as lxx:align-choose --list --book gen --chapter 27 prints it\n",
			[]string{"tests/unit/x.test.ts"},
		},
		"a × or ✘ line alone is not a name a command can take": {
			"  × lists --list 12ms\n  ✘ a title --x\n--- FAIL: TestKept (0.00s)\n",
			[]string{"TestKept"},
		},
		"a name that starts with a dash is not one a command can take": {
			"FAIL  --list\nFAIL  src/a.test.ts > one\n",
			[]string{"src/a.test.ts"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := application.TestNamesOf(c.output); !reflect.DeepEqual(got, c.want) {
				t.Errorf("TestNamesOf = %q, want %q", got, c.want)
			}
		})
	}
}

// The gates of mw-gq6.858 and mw-gq6.859 failed the mail-notify tests of
// contrib/ though neither story touched it (mw-gq6.861): a close-out does not
// put such a failure down to the story.
func TestPackagesOutsideTheStoryAreThoseThatHoldNoChangedFile(t *testing.T) {
	failed := "--- FAIL: TestALineThatNeverGoes (62.07s)\nFAIL\n" +
		"FAIL\tgithub.com/x/millwright/contrib\t93.086s\n" +
		"FAIL\tgithub.com/x/millwright/infrastructure/beads\t1.0s\n"
	cases := map[string]struct {
		changed []string
		want    []string
	}{
		"the story changes other packages": {
			[]string{"application/next.go", "docs/a.md", "mw-gq6.858.md"},
			[]string{"github.com/x/millwright/contrib", "github.com/x/millwright/infrastructure/beads"},
		},
		"a failing package holds a changed file": {[]string{"application/next.go", "contrib/mail-notify"}, nil},
		"a package of the same name deeper down is not it": {
			[]string{"infrastructure/beads/sub/x.go"},
			[]string{"github.com/x/millwright/contrib", "github.com/x/millwright/infrastructure/beads"},
		},
		"a Go file at the root is read by every package": {[]string{"main.go"}, nil},
		"go.mod is read by every package":                {[]string{"go.mod"}, nil},
		"the story changed nothing":                      {nil, []string{"github.com/x/millwright/contrib", "github.com/x/millwright/infrastructure/beads"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := application.PackagesOutsideTheStory(failed, c.changed); !reflect.DeepEqual(got, c.want) {
				t.Errorf("PackagesOutsideTheStory = %q, want %q", got, c.want)
			}
		})
	}
}

func TestNoPackageIsOutsideTheStoryWhenTheOutputNamesNone(t *testing.T) {
	if got := application.PackagesOutsideTheStory("--- FAIL: TestX (0.01s)\n", []string{"application/next.go"}); got != nil {
		t.Errorf("PackagesOutsideTheStory = %q, want none: with no package named the failure may be the story's", got)
	}
}

// mw-gq6.1412: the wait for room before the run on main alone judges the host by
// the Next's own Room, so dispatch.min_drive_free_gb = 0 (NoDriveGuard) turns the
// Windows-drive guard off there as it does for a story's start.
func TestMainRedWaitHonoursTheDriveGuardOff(t *testing.T) {
	for _, tc := range []struct {
		name    string
		room    application.RoomLimits
		wantRun bool
	}{
		{"the guard on, the drive under 10 GB", application.RoomLimits{}, false},
		{"the guard off, the drive under 10 GB", application.RoomLimits{NoDriveGuard: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			named := &namedTestsThat{run: failingOnMain("--- FAIL: TestA (0.01s)\nFAIL\n")}
			load := &apptest.FakeHostLoad{Reading: aDriveWith(10 << 30)}
			tracker := apptest.NewFakeTracker()
			tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
			tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
			vault := newFakeVault()
			vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
			landing := &aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}
			mailbox := apptest.NewFakeMailbox()
			report, err := application.Next{
				Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
				Checks: twoFailingTests{}, Slot: aSlot{}, Mailbox: mailbox,
				NamedChecks: named, Load: load, Room: tc.room,
				LoadWait: func(context.Context, time.Duration) error { return nil },
				Seat:     "builder", Host: "laptop",
				Rigs: map[string]string{"millwright": "/rigs/millwright"},
				Out:  io.Discard, Err: io.Discard,
			}.Run(context.Background(), "mw-x.1")
			if report.Reason != application.ReasonTestsFail && report.Reason != application.ReasonMainRed {
				t.Fatalf("expected a stop for the failing tests, got %q (%v)", report.Reason, err)
			}
			inbox, _ := mailbox.Inbox(context.Background(), application.MayorMailbox)
			var mail string
			for _, m := range inbox {
				mail += m.Body + "\n"
			}
			if ran := len(named.asked) > 0; ran != tc.wantRun {
				t.Errorf("run on main alone made = %v, want %v (asked %q)", ran, tc.wantRun, named.asked)
			}
			if said := strings.Contains(mail, "D: has 10.0 GB free"); said == tc.wantRun {
				t.Errorf("mail says the drive has no room = %v, want %v:\n%s", said, !tc.wantRun, mail)
			}
		})
	}
}

// A bug that carries main's red runs on the factory's default route, not on
// the finder's (mw-gq6.1550, folded into mw-gq6.1640).
func TestAMainRedBugGetsItsOwnModelAndEffortNotTheFinders(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelFable, Effort: domain.EffortXHigh, Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", theSourceStory())
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	landing := &aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}
	named := &namedTestsThat{run: failingOnMain("--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n")}
	before := tracker.Stories()
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
		Checks: twoFailingTests{}, Slot: aSlot{}, Mailbox: apptest.NewFakeMailbox(),
		NamedChecks: named, MainAloneBound: 0,
		LoadWait: func(context.Context, time.Duration) error { return nil },
		Seat:     "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	if report.Reason != application.ReasonMainRed {
		t.Fatalf("expected the story held on main's red, got %q (%v)", report.Reason, err)
	}
	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 || !strings.Contains(filed[0].Story.Title, "main is red") {
		t.Fatalf("expected 1 main-red bug filed, got %d: %+v", len(filed), filed)
	}
	if got := filed[0].Merged(); got.Model != domain.ModelSonnet || got.Effort != domain.EffortHigh {
		t.Errorf("expected the main-red bug on sonnet at high, got %s at %s", got.Model, got.Effort)
	}
}

// aLandingAtACommit answers HEAD of the landing worktree with a commit, as git does.
type aLandingAtACommit struct {
	*aMainLanding
	sha string
}

func (l aLandingAtACommit) RefHead(ctx context.Context, dir, ref string) (string, error) {
	if ref == "HEAD" {
		return l.sha, nil
	}
	return l.aMainLanding.RefHead(ctx, dir, ref)
}

// mw-gq6.1750: the bug mw next files for main's red named its tests and no
// "Main's sha:", so a dispatch that saw its check pass at the very commit it
// failed at closed it as fixed instead of holding it as a flake.
func TestAMainRedBugNamesTheCommitItFailedAt(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", theSourceStory())
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	const sha = "0123456789abcdef0123456789abcdef01234567"
	landing := aLandingAtACommit{aMainLanding: &aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}, sha: sha}
	named := &namedTestsThat{run: failingOnMain("--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n")}
	before := tracker.Stories()
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
		Checks: twoFailingTests{}, Slot: aSlot{}, Mailbox: apptest.NewFakeMailbox(),
		NamedChecks: named, MainAloneBound: 0,
		LoadWait: func(context.Context, time.Duration) error { return nil },
		Seat:     "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	if report.Reason != application.ReasonMainRed {
		t.Fatalf("expected the story held on main's red, got %q (%v)", report.Reason, err)
	}
	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 {
		t.Fatalf("expected 1 main-red bug filed, got %d: %+v", len(filed), filed)
	}
	if want := "Main's sha: " + sha; !strings.Contains(filed[0].Description, want) {
		t.Errorf("expected the bug to name the commit it failed at (%q), got:\n%s", want, filed[0].Description)
	}
}
