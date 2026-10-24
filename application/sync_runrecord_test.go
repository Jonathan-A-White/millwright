package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aRetriedStoryRunningHere is a story in progress on the sync's host, the way
// a retry leaves one: its first attempt's run record is committed and its new
// session has written over it (mw-vwveeq.1).
func aRetriedStoryRunningHere(t *testing.T, tracker *apptest.FakeTracker, id string) {
	t.Helper()
	tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Host: "vps"})
	tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "A retried story"})
	if err := tracker.ClaimStory(context.Background(), id); err != nil {
		t.Fatalf("claiming %s: %v", id, err)
	}
}

func TestSyncCommitsAsFoundTheRunRecordAnInProgressStoryRewrote(t *testing.T) {
	sync, files, _ := syncing(t)
	tracker := sync.Tracker.(*apptest.FakeTracker)
	aRetriedStoryRunningHere(t, tracker, "mw-gq6.739")
	files.Dirty = []string{"runs/mw-gq6.739/result.json"}

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("expected the rewritten run record of a story in progress here not to refuse the sync, got %v", err)
	}
	if len(report.Blocked) != 0 {
		t.Fatalf("expected nothing blocked, got %v", report.Blocked)
	}
	commits := files.Commits()
	if len(commits) != 1 || len(commits[0].Paths) != 1 || commits[0].Paths[0] != "runs/mw-gq6.739/result.json" {
		t.Fatalf("expected one commit of exactly the run record, got %+v", commits)
	}
	if !strings.Contains(commits[0].Message, "mw-gq6.739") {
		t.Fatalf("expected the commit to name the story, got %q", commits[0].Message)
	}
	if _, pulls, pushes := files.Moves(); pulls != 1 || pushes != 1 {
		t.Fatalf("expected the vault half to run after the commit, got %d pulls, %d pushes", pulls, pushes)
	}
}

func TestSyncCommitsTheNumberedRunRecordOfAnInProgressStoryToo(t *testing.T) {
	sync, files, _ := syncing(t)
	aRetriedStoryRunningHere(t, sync.Tracker.(*apptest.FakeTracker), "mw-gq6.739")
	files.Dirty = []string{"runs/mw-gq6.739/result-2.json"}

	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("expected the sync not to be refused, got %v", err)
	}
}

func TestSyncStillRefusesAnyOtherTrackedFileOfAnInProgressStory(t *testing.T) {
	sync, files, _ := syncing(t)
	aRetriedStoryRunningHere(t, sync.Tracker.(*apptest.FakeTracker), "mw-gq6.739")
	files.Dirty = []string{"runs/mw-gq6.739/result.json", "runs/mw-gq6.739/gate.log", "seats/mayor/ledger.md"}

	_, err := sync.Run(context.Background())
	blocked, stopped := application.Blocked(err)
	if !stopped {
		t.Fatalf("expected a blocked vault, got %v", err)
	}
	if got := strings.Join(blocked.Files, ","); got != "runs/mw-gq6.739/gate.log,seats/mayor/ledger.md" {
		t.Fatalf("expected only the files the sync does not commit to stay in the way, got %s", got)
	}
	for _, want := range []string{"uncommitted changes to runs/mw-gq6.739/gate.log, seats/mayor/ledger.md", "mw commits nobody's work for them"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected the refusal to say %q, got %q", want, err)
		}
	}
}

func TestSyncStillRefusesTheRunRecordOfAStoryNotInProgressHere(t *testing.T) {
	sync, files, tracker := syncing(t)
	tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Host: "laptop"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Not claimed"})
	files.Dirty = []string{"runs/mw-gq6.1/result.json", "runs/mw-gq6.3/result.json"}

	_, err := sync.Run(context.Background())
	blocked, stopped := application.Blocked(err)
	if !stopped || len(blocked.Files) != 2 {
		t.Fatalf("expected both run records to stay in the way, got %v", err)
	}
	if len(files.Commits()) != 0 {
		t.Fatalf("expected nothing committed, got %+v", files.Commits())
	}
}

func TestSyncRefusesOnTheRunRecordWhenItCannotCommitIt(t *testing.T) {
	sync, files, _ := syncing(t)
	aRetriedStoryRunningHere(t, sync.Tracker.(*apptest.FakeTracker), "mw-gq6.739")
	files.Dirty = []string{"runs/mw-gq6.739/result.json"}
	files.CommitErr = errors.New("git commit: index.lock exists")

	report, err := sync.Run(context.Background())
	if _, stopped := application.Blocked(err); !stopped {
		t.Fatalf("expected a blocked vault when the commit failed, got %v", err)
	}
	if len(report.Blocked) != 1 || !strings.Contains(report.String(), "run record") {
		t.Fatalf("expected the report to name the file and say why, got %+v / %s", report.Blocked, report.String())
	}
}
