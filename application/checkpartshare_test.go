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

// namedParts is a rig's gate in the parts a test names, run as heldParts' are.
type namedParts struct {
	*heldParts
	parts []application.GatePart
}

func (n *namedParts) Parts(string) []application.GatePart { return n.parts }

// checkWith is twoStoriesOnOneRig's check, for a rig whose parts the test names
// and whose records the test reads.
func checkWith(t *testing.T, parts *namedParts, wait time.Duration) (func(id string, part int) error, *rig.Slots, string, *mapRecords) {
	t.Helper()
	_, slots, rigDir := twoStoriesOnOneRig(t, parts.heldParts, wait)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
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
	return check, slots, rigDir, records
}

// The lampas gate as the home's config names it: every part but the shots runs
// a tool in the story's worktree at the branch's tip and never touches the
// merge target.
func lampasParts() *namedParts {
	return &namedParts{heldParts: newHeldParts(), parts: []application.GatePart{
		{Name: "install", Command: "npm ci --no-audit --no-fund"},
		{Name: "vitest 1/6", Command: "TZ=UTC npx vitest run --shard 1/6"},
		{Name: "typecheck", Command: "npm run typecheck"},
		{Name: "lint", Command: "npm run lint"},
		{Name: "build", Command: "npm run build"},
		{Name: "server", Command: "cd server && go build ./... && go test ./..."},
		{Name: "shots", Command: "npm run gate:shots"},
		{Name: "deploy", Command: "git merge origin/main && npm test"},
	}}
}

func TestAPartThatOnlyRunsToolsInTheWorktreeTakesNoMergeSlotAndRunsWhileALandingHoldsIt(t *testing.T) {
	parts := lampasParts()
	close(parts.release)
	check, slots, rigDir, _ := checkWith(t, parts, 100*time.Millisecond)
	landing, err := slots.Take(context.Background(), rigDir, "mw land on laptop, landing a batch of millwright")
	if err != nil {
		t.Fatal(err)
	}
	defer landing.Release(context.Background())

	for n := 1; n <= 6; n++ {
		started := time.Now()
		if err := check("mw-x.1", n); err != nil {
			t.Errorf("expected part %d (%s) to run with a landing holding the slot, got %v", n, parts.parts[n-1].Name, err)
		}
		if time.Since(started) > 80*time.Millisecond {
			t.Errorf("expected part %d to start at once, it waited %s", n, time.Since(started))
		}
	}
}

func TestAPartThatCouldTouchTheMergeTargetStillWaitsForALandingsSlot(t *testing.T) {
	for _, n := range []int{7, 8} {
		parts := lampasParts()
		close(parts.release)
		check, slots, rigDir, _ := checkWith(t, parts, 100*time.Millisecond)
		landing, err := slots.Take(context.Background(), rigDir, "mw land on laptop, landing a batch of millwright")
		if err != nil {
			t.Fatal(err)
		}
		err = check("mw-x.1", n)
		landing.Release(context.Background())
		if err == nil || !strings.Contains(err.Error(), "merge slot") {
			t.Errorf("expected part %d (%s) to wait on the landing's slot, got %v", n, parts.parts[n-1].Name, err)
		}
	}
}

func TestPartNeedsSlotListsTheLampasGateAsNeedingItForNoPartButTheShots(t *testing.T) {
	needs := map[string]bool{}
	for _, part := range lampasParts().parts {
		needs[part.Name] = application.PartNeedsSlot(part)
	}
	want := map[string]bool{"install": false, "vitest 1/6": false, "typecheck": false, "lint": false, "build": false, "server": false, "shots": true, "deploy": true}
	for name, wanted := range want {
		if needs[name] != wanted {
			t.Errorf("part %s: want needs-slot %v, got %v", name, wanted, needs[name])
		}
	}
	for _, command := range []string{"npm run lint:fix", "npm run lint | tee x", "npm ci; rm -rf x", "npm test > /tmp/x", "TZ=UTC npx vitest $(ls)", "make test"} {
		if !application.PartNeedsSlot(application.GatePart{Command: command}) {
			t.Errorf("expected %q, a command this rule does not know to leave the target alone, to keep the slot", command)
		}
	}
}

func TestAPartWaitsAboutAsLongAsAForegroundCallAllowsAndEndsInWords(t *testing.T) {
	if application.CheckPartSlotWait != 9*time.Minute {
		t.Errorf("expected a part to wait 9 minutes for the slot, got %s", application.CheckPartSlotWait)
	}
	if application.CheckPartSlotWait >= 590*time.Second {
		t.Errorf("expected the wait to end before a Builder's 590 second foreground call does, got %s", application.CheckPartSlotWait)
	}
}

func TestAPartHandedBackTwiceOnTheSlotInOneAttemptIsOneWaitToldTheSameWayBothTimes(t *testing.T) {
	parts := lampasParts()
	close(parts.release)
	check, slots, rigDir, records := checkWith(t, parts, 60*time.Millisecond)
	landing, err := slots.Take(context.Background(), rigDir, "mw land on laptop, landing a batch of millwright")
	if err != nil {
		t.Fatal(err)
	}
	defer landing.Release(context.Background())

	first := check("mw-x.1", 7)
	second := check("mw-x.1", 7)
	for i, err := range []error{first, second} {
		if err == nil {
			t.Fatalf("hand-back %d: expected the part not to run while a landing held the slot", i+1)
		}
		for _, want := range []string{"`mw check mw-x.1 --part 7`", "the same part", "not a failure of the part"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("hand-back %d: expected %q, got %q", i+1, want, err)
			}
		}
	}
	if !strings.Contains(second.Error(), "again") || !strings.Contains(second.Error(), "counted once") {
		t.Errorf("expected the repeat to say it counts once, got %q", second)
	}
	// One record of the wait for the part of the attempt, however often it was told.
	var held []string
	for name := range records.files {
		if strings.Contains(name, "part-7") {
			held = append(held, name)
		}
	}
	if len(held) != 1 {
		t.Errorf("expected one record of part 7's waits in the attempt, got %v", held)
	}
}

func TestAnOpenStepsRefusalNamesAPartThatWaitedOnTheSlotAsHeldNotFailed(t *testing.T) {
	parts := lampasParts()
	close(parts.release)
	check, slots, rigDir, records := checkWith(t, parts, 60*time.Millisecond)
	landing, err := slots.Take(context.Background(), rigDir, "mw land on laptop, landing a batch of millwright")
	if err != nil {
		t.Fatal(err)
	}
	defer landing.Release(context.Background())
	_ = check("mw-x.1", 7)

	tip := "abc1234abc1234abc1234abc1234abc1234abc12"
	gate, _ := application.RecordedGateForTest(records, "mw-x.1", 1, parts.parts, tip).Run(context.Background(), "", "")
	if gate.Passed {
		t.Fatal("expected the gate not to pass with its parts unrun")
	}
	var waited, others []string
	for _, line := range strings.Split(gate.Output, "\n") {
		if strings.HasPrefix(line, "part 7 (shots)") {
			waited = append(waited, line)
		} else if strings.Contains(line, "waited on the merge slot") {
			others = append(others, line)
		}
	}
	if len(waited) != 1 {
		t.Fatalf("expected one line for part 7, got:\n%s", gate.Output)
	}
	for _, want := range []string{"has not been run at this commit", "waited on the merge slot", "mw land on laptop", "not a failure", "mw check mw-x.1 --part 7"} {
		if !strings.Contains(waited[0], want) {
			t.Errorf("expected the unrun part to say %q, got %q", want, waited[0])
		}
	}
	if len(others) != 0 {
		t.Errorf("expected only the part that waited to say so, got %q", others)
	}
}
