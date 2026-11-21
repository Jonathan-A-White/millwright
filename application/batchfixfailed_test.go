package application_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// mw-qqxlwm3.1: batch 595 of millwright had a fixer that ended error_max_turns and
// left about thirty files uncommitted. A fixer that does not end in success is a
// failed fixer: what it left uncommitted is discarded from the landing and kept
// as a diff in the batch's records, and the outcome and the Held mail say how it
// ended and name the story that was set aside as the cause.

// endedRunner is a runner whose sessions end as it is told: Wait says how, at once.
type endedRunner struct {
	*apptest.FakeRunner
	ended application.SessionStatus
}

func (r endedRunner) Wait(_ context.Context, name string) (application.SessionStatus, error) {
	status := r.ended
	status.Name = name
	return status, nil
}

// leftoversLanding is a landing whose worktree holds what a fixer left
// uncommitted, until KeepLeftovers discards it.
type leftoversLanding struct {
	fakeRetryLanding
	mu2    sync.Mutex
	kept   []string // the dest of each KeepLeftovers
	keepIn []string // the dir of each
	err    error
}

func (l *leftoversLanding) KeepLeftovers(_ context.Context, dir, dest string) error {
	l.mu2.Lock()
	defer l.mu2.Unlock()
	if l.err != nil {
		return l.err
	}
	l.kept = append(l.kept, dest)
	l.keepIn = append(l.keepIn, dir)
	l.UncommittedPaths = nil
	return nil
}

// diffRecords is where a batch's records say its files are.
type diffRecords struct{ noRecords }

func (diffRecords) BatchFilePath(rig string, number int, name string) string {
	return "/vault/runs/batch/" + rig + "/4/" + name
}

type aFailedFixer struct {
	land    application.BatchLand
	landing *leftoversLanding
	report  *application.BatchReport
	go_     bool
}

// aFixerThatEnded runs the batch's fixer step on a batch with mw-r9xo1pj.4 set
// aside for conflicts; the fixer ended as ended says and wrote result (empty:
// none), leaving two files uncommitted.
func aFixerThatEnded(t *testing.T, ended application.SessionStatus, result string) aFailedFixer {
	t.Helper()
	vault := newFakeVault()
	if result != "" {
		vault.written["batch-millwright-4/"+application.ResultFileName] = result
	}
	landing := &leftoversLanding{fakeRetryLanding: fakeRetryLanding{UncommittedPaths: []string{"src/a.ts", "scripts/b.json"}}}
	land := application.BatchLand{
		Next: application.Next{
			Landing: landing, Runner: endedRunner{apptest.NewFakeRunner(), ended},
			Boot: application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: "laptop"},
			Vault: vault, Host: "laptop", Seat: "builder", Out: io.Discard, Err: io.Discard,
		},
		Records: diffRecords{}, Fix: true,
	}
	report := &application.BatchReport{
		Rig: "millwright", Target: "main", Number: 4, Base: "base0123", Tested: false,
		Queue:    []application.BatchStory{{ID: "mw-r9xo1pj.4", Title: "The set-aside story"}},
		SetAside: []application.BatchStory{{ID: "mw-r9xo1pj.4", Title: "The set-aside story", Why: "conflicts in two unit test files", Conflicts: []string{"CONFLICT (content): a_test.ts"}}},
	}
	ok := land.FixTheBatch(context.Background(), "/rigs/millwright/.landing-1", report)
	return aFailedFixer{land: land, landing: landing, report: report, go_: ok}
}

func TestAFixerThatRanOutOfTurnsIsAFailedFixerAndItsUncommittedFilesAreKeptAsADiff(t *testing.T) {
	got := aFixerThatEnded(t, application.SessionStatus{State: application.StateExited},
		`{"subtype":"error_max_turns","is_error":true}`)

	if got.go_ {
		t.Fatalf("expected the batch held, not carried on to be tested: %+v / %v", got.report.Fixer, got.report.Notes)
	}
	if want := []string{"/vault/runs/batch/millwright/4/" + application.BatchFixerLeftoversFile}; strings.Join(got.landing.kept, ",") != strings.Join(want, ",") {
		t.Fatalf("expected the uncommitted changes kept at %v, got %v", want, got.landing.kept)
	}
	if got.landing.keepIn[0] != "/rigs/millwright/.landing-1" {
		t.Fatalf("expected the landing worktree cleared, got %v", got.landing.keepIn)
	}
	if left, _ := got.landing.Uncommitted(context.Background(), ""); len(left) != 0 {
		t.Fatalf("expected the worktree left clean, got %v", left)
	}
	if !strings.Contains(got.report.Held, "the fixer ran out of turns") {
		t.Errorf("expected the hold to say the fixer ran out of turns, got %q", got.report.Held)
	}
	if strings.Contains(got.report.Held, "left changes uncommitted") {
		t.Errorf("expected no hold for uncommitted changes: they are discarded, got %q", got.report.Held)
	}
	outcome, mail := got.land.FixerReads(got.report)
	for name, said := range map[string]string{"outcome": outcome, "mail": mail} {
		for _, want := range []string{"runs/batch/millwright/4/" + application.BatchFixerLeftoversFile, "src/a.ts", "scripts/b.json"} {
			if !strings.Contains(said, want) {
				t.Errorf("expected the %s to hold %q, got:\n%s", name, want, said)
			}
		}
	}
	for _, want := range []string{"The cause", "mw-r9xo1pj.4", "conflicts in two unit test files", "ran out of turns", "discarded"} {
		if !strings.Contains(mail, want) {
			t.Errorf("expected the Held mail to hold %q, got:\n%s", want, mail)
		}
	}
	if strings.Contains(mail, "ran the rig's tests on it again") {
		t.Errorf("mw did not test what a failed fixer left, and the mail must not say it did:\n%s", mail)
	}
}

// Each end that is not a success is the same: the files discarded and kept, the
// end said, the set-aside story named.
func TestAFixerThatDidNotEndInSuccessIsAFailedFixerWhateverTheEnd(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ended  application.SessionStatus
		result string
		want   string
	}{
		{"out of turns", application.SessionStatus{State: application.StateExited}, `{"subtype":"error_max_turns","is_error":true}`, "the fixer ran out of turns"},
		{"an error end", application.SessionStatus{State: application.StateExited}, `{"subtype":"error_during_execution","is_error":true}`, "the fixer ended as error_during_execution"},
		{"an error flagged on a success subtype", application.SessionStatus{State: application.StateExited}, `{"subtype":"success","is_error":true,"result":"the model refused the request"}`, "the fixer ended in error"},
		{"a killed session with no result", application.SessionStatus{State: application.StateGone}, "", "the fixer was killed"},
		{"a session whose exit nobody could read", application.SessionStatus{State: application.StateExitUnknown}, "", "the fixer was killed"},
		{"an exit with a status and no result", application.SessionStatus{State: application.StateExited, ExitCode: 137}, "", "the fixer was killed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := aFixerThatEnded(t, tc.ended, tc.result)
			if got.go_ {
				t.Fatalf("expected the batch held")
			}
			if len(got.landing.kept) != 1 {
				t.Fatalf("expected the uncommitted changes kept once, got %v", got.landing.kept)
			}
			if !strings.Contains(got.report.Held, tc.want) {
				t.Errorf("expected the hold to say %q, got %q", tc.want, got.report.Held)
			}
			outcome, mail := got.land.FixerReads(got.report)
			if !strings.Contains(outcome, application.BatchFixerLeftoversFile) || !strings.Contains(mail, "mw-r9xo1pj.4") || !strings.Contains(mail, "The cause") {
				t.Errorf("expected the outcome to keep the diff and the mail to name the story as the cause, got:\n%s\n---\n%s", outcome, mail)
			}
		})
	}
}

// A fixer that ended well is judged as before, and one that left no file
// uncommitted has nothing to keep.
func TestAFixerThatSucceededKeepsNothingAndAFailedOneThatLeftNothingKeepsNoDiff(t *testing.T) {
	got := aFixerThatEnded(t, application.SessionStatus{State: application.StateExited}, `{"subtype":"success","result":"Mended."}`)
	if len(got.landing.kept) != 0 {
		t.Fatalf("expected a fixer that succeeded to have nothing discarded, got %v", got.landing.kept)
	}
	if !strings.Contains(got.report.Held, "left changes uncommitted") {
		t.Errorf("expected a successful fixer's uncommitted files held as before, got %q", got.report.Held)
	}

	failed := aFixerThatEnded(t, application.SessionStatus{State: application.StateExited}, `{"subtype":"error_max_turns","is_error":true}`)
	failed.landing.UncommittedPaths = nil
	failed.landing.kept = nil
	failed.report.Fixer, failed.report.Held = nil, ""
	failed.land.FixTheBatch(context.Background(), "/rigs/millwright/.landing-1", failed.report)
	if len(failed.landing.kept) != 0 || !strings.Contains(failed.report.Held, "the fixer ran out of turns") {
		t.Errorf("expected a failed fixer with nothing uncommitted to keep no diff and still be held, got %v / %q", failed.landing.kept, failed.report.Held)
	}
}

// A diff that could not be kept is said, and the batch is held all the same.
func TestAFailedFixerWhoseDiffCouldNotBeKeptSaysSo(t *testing.T) {
	vault := newFakeVault()
	vault.written["batch-millwright-4/"+application.ResultFileName] = `{"subtype":"error_max_turns","is_error":true}`
	landing := &leftoversLanding{fakeRetryLanding: fakeRetryLanding{UncommittedPaths: []string{"src/a.ts"}}, err: errors.New("disk full")}
	land := application.BatchLand{
		Next: application.Next{
			Landing: landing, Runner: endedRunner{apptest.NewFakeRunner(), application.SessionStatus{State: application.StateExited}},
			Boot:  application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: "laptop"},
			Vault: vault, Host: "laptop", Seat: "builder", Out: io.Discard, Err: io.Discard,
		},
		Records: diffRecords{}, Fix: true,
	}
	report := &application.BatchReport{Rig: "millwright", Target: "main", Number: 4, Base: "base0123",
		SetAside: []application.BatchStory{{ID: "mw-r9xo1pj.4", Why: "conflicts"}}}
	if land.FixTheBatch(context.Background(), "/rigs/millwright/.landing-1", report) {
		t.Fatalf("expected the batch held")
	}
	_, mail := land.FixerReads(report)
	if !strings.Contains(mail, "disk full") || !strings.Contains(mail, "src/a.ts") {
		t.Errorf("expected the mail to say why the diff was not kept and what was left, got:\n%s", mail)
	}
}
