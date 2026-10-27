package application_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// heldParts is a rig's gate in two parts whose commands are run by the test: each
// run says it has started, then stays until the test lets the parts go.
type heldParts struct {
	mu      sync.Mutex
	started chan string
	release chan struct{}
	ran     []string
}

func newHeldParts() *heldParts {
	return &heldParts{started: make(chan string, 8), release: make(chan struct{})}
}

func (h *heldParts) Parts(string) []application.GatePart {
	return []application.GatePart{{Name: "lint", Command: "lint"}, {Name: "unit", Command: "unit"}}
}

func (h *heldParts) RunLine(_ context.Context, _, dir, command string) (application.Checked, error) {
	h.mu.Lock()
	h.ran = append(h.ran, filepath.Base(dir)+" "+command)
	h.mu.Unlock()
	h.started <- filepath.Base(dir)
	<-h.release
	return application.Checked{Command: command, Passed: true}, nil
}

type mapRecords struct {
	mu    sync.Mutex
	files map[string]string
}

func (m *mapRecords) PutRunFile(_ context.Context, story, name, contents string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		m.files = map[string]string{}
	}
	m.files[story+"/"+name] = contents
	return name, nil
}

func (m *mapRecords) ReadRunFile(_ context.Context, story, name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if text, ok := m.files[story+"/"+name]; ok {
		return text, nil
	}
	return "", os.ErrNotExist
}

// twoStoriesOnOneRig is a rig with two stories being checked and the real merge
// slot of a rig checkout under a temporary directory.
func twoStoriesOnOneRig(t *testing.T, parts *heldParts, wait time.Duration) (func(id string, part int) error, *rig.Slots, string) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.2", Title: "Two"})
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	slots := rig.NewSlots(rig.WithSlotPoll(5 * time.Millisecond))
	records := &mapRecords{}
	check := func(id string, part int) error {
		_, err := application.Check{
			Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: &scriptedChecks{passes: []bool{true}},
			Slot: slots, Parts: parts, Records: records, Part: part, PartSlotWait: wait,
			Host: "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": rigDir},
			Out:  io.Discard,
		}.Run(context.Background(), id)
		return err
	}
	return check, slots, rigDir
}

func TestPartsOfTwoStoriesRunAtTheSameTimeAndALandingWaitsForThemAndTheyForIt(t *testing.T) {
	parts := newHeldParts()
	check, slots, rigDir := twoStoriesOnOneRig(t, parts, time.Minute)

	done := make(chan error, 2)
	for _, id := range []string{"mw-x.1", "mw-x.2"} {
		go func() { done <- check(id, 1) }()
	}
	// Both parts are running at once: neither has finished, and both started.
	for range 2 {
		select {
		case <-parts.started:
		case <-time.After(5 * time.Second):
			t.Fatal("expected the parts of both stories to run at the same time, the second never started")
		}
	}

	// A landing needs the slot alone, so it waits for both running parts.
	waiting, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if landing, err := slots.Take(waiting, rigDir, "mw land on laptop, landing a batch of millwright"); err == nil {
		landing.Release(context.Background())
		t.Fatal("expected a landing to wait while two parts were running")
	} else if !strings.Contains(err.Error(), "checking mw-x.") {
		t.Errorf("expected the landing to be told which checks it waits for, got %v", err)
	}

	close(parts.release)
	for range 2 {
		if err := <-done; err != nil {
			t.Errorf("expected the part to pass, got %v", err)
		}
	}

	// With the checks done the landing has the slot. (A part waiting for a
	// landing is the next test.)
	landing, err := slots.Take(context.Background(), rigDir, "mw land on laptop, landing a batch of millwright")
	if err != nil {
		t.Fatalf("expected the landing to get the slot once the parts were done: %v", err)
	}
	landing.Release(context.Background())
}

func TestAPartThatCannotGetTheSlotInItsWaitEndsWithOneLineNamingTheHolderAndTheNextCommand(t *testing.T) {
	parts := newHeldParts()
	check, slots, rigDir := twoStoriesOnOneRig(t, parts, 150*time.Millisecond)

	landing, err := slots.Take(context.Background(), rigDir, "mw land on laptop, landing a batch of millwright")
	if err != nil {
		t.Fatalf("taking the slot as a landing: %v", err)
	}
	defer landing.Release(context.Background())

	started := time.Now()
	err = check("mw-x.1", 2)
	if err == nil {
		t.Fatal("expected the part not to run while a landing held the slot")
	}
	if time.Since(started) > 5*time.Second {
		t.Errorf("expected the part to give up at its wait, it took %s", time.Since(started))
	}
	said := err.Error()
	for _, want := range []string{"part 2 of 2 (unit)", "did not start", "mw land on laptop, landing a batch of millwright", "since ", "nothing was run", "`mw check mw-x.1 --part 2`"} {
		if !strings.Contains(said, want) {
			t.Errorf("expected the line to say %q, got %q", want, said)
		}
	}
	if strings.Contains(said, "\n") {
		t.Errorf("expected one line, got %q", said)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		// The line is the whole answer; callers need not see a bare timeout.
		t.Logf("the line wraps the deadline: %v", err)
	}
	select {
	case <-parts.started:
		t.Error("expected no part to have run")
	default:
	}
}

// headedLanding is a landing whose story branch has a tip.
type headedLanding struct {
	*fakeRetryLanding
	head string
}

func (h *headedLanding) RefHead(context.Context, string, string) (string, error) { return h.head, nil }

// A story whose formula is left with steps open names, in the refusal mw next
// mails the Mayor, each part of the rig's split gate that has not been run at
// the tip and the command that runs it (mw-6zgnz5y).
func TestOpenStepsOfAStoryWithAnUnrunSplitGateNameTheUnrunPartsAndTheCommand(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	tracker.AddFormula("tdd-feature", application.FormulaStep{ID: "s-1", Title: "Run the full build, tests and vet"})
	poured, err := tracker.PourFormula(context.Background(), "tdd-feature", "mw-x.1", "One", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryMetadata(context.Background(), "mw-x.1", map[string]string{application.MoleculeField: poured.RootID}); err != nil {
		t.Fatal(err)
	}
	records := &mapRecords{}
	// Part 1 passed at the tip; part 2 was never run.
	tip := "abc1234abc1234abc1234abc1234abc1234abc12"
	landing := &headedLanding{fakeRetryLanding: &fakeRetryLanding{AheadCount: 1}, head: tip}
	if _, err := records.PutRunFile(context.Background(), "mw-x.1", application.CheckPartFileName(1, 1), "tip "+tip+"\npassed\nok\n"); err != nil {
		t.Fatal(err)
	}

	report, _ := application.Check{
		Tracker: tracker, Landing: landing, Checks: &scriptedChecks{passes: []bool{true}},
		Parts: newHeldParts(), Records: records,
		Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard,
	}.Run(context.Background(), "mw-x.1")

	var open *application.Refusal
	for i := range report.Refusals {
		if report.Refusals[i].Reason == application.ReasonOpenSteps {
			open = &report.Refusals[i]
		}
	}
	if open == nil {
		t.Fatalf("expected an open-steps refusal, got %+v", report.Refusals)
	}
	if !strings.Contains(open.Said, "part 2 (unit) has not been run") || !strings.Contains(open.Said, "mw check mw-x.1 --part 2") {
		t.Errorf("expected the refusal to name the unrun part and the command, got:\n%s", open.Said)
	}
	if strings.Contains(open.Said, "part 1 (lint)") {
		t.Errorf("expected the part that passed not to be named, got:\n%s", open.Said)
	}
}
