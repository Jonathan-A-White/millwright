package application_test

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// The landed tail (mw-gq6.971): what follows the push of a single landing and
// of a batch landing. Five stories found a step one path took and the other
// did not; the tail is now one table both walk.

// portLog is the calls a landing made to the ports it was given, in order, as
// one line each: what the port was asked, and of which story.
type portLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *portLog) add(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, fmt.Sprintf(format, args...))
}

func (l *portLog) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

type loggedTracker struct {
	application.WorkTracker
	log *portLog
}

func (t loggedTracker) SetStoryState(ctx context.Context, id, dimension, value, reason string) error {
	t.log.add("run state %s", id)
	return t.WorkTracker.SetStoryState(ctx, id, dimension, value, reason)
}

func (t loggedTracker) CloseStory(ctx context.Context, id, reason string) error {
	t.log.add("close %s", id)
	return t.WorkTracker.CloseStory(ctx, id, reason)
}

type loggedVault struct {
	*fakeVault
	log *portLog
}

func (v loggedVault) AppendToLedger(ctx context.Context, seat, line string) error {
	v.log.add("ledger")
	return v.fakeVault.AppendToLedger(ctx, seat, line)
}

func (v loggedVault) PutRunFile(ctx context.Context, story, name, contents string) (string, error) {
	v.log.add("run file %s/%s", story, name)
	return v.fakeVault.PutRunFile(ctx, story, name, contents)
}

type loggedLanding struct {
	*aLandingLanding
	log *portLog
	// moved makes Advance say the rig's checkout moved, so that the
	// after-landing command and the smoke are run.
	moved bool
}

func (l loggedLanding) Advance(ctx context.Context, rigDir, target, commit string) (application.Advanced, error) {
	l.log.add("advance the rig to %s", commit)
	if l.moved {
		return application.Advanced{Moved: true}, nil
	}
	return l.aLandingLanding.Advance(ctx, rigDir, target, commit)
}

func (l loggedLanding) Remove(ctx context.Context, rigDir, worktree, branch string) error {
	l.log.add("remove worktree %s", branch)
	return l.aLandingLanding.Remove(ctx, rigDir, worktree, branch)
}

type loggedMailbox struct {
	application.Mailbox
	log *portLog
}

func (m loggedMailbox) Send(ctx context.Context, message application.NewMessage) (string, error) {
	m.log.add("mail %s", message.Subject)
	return m.Mailbox.Send(ctx, message)
}

type loggedStamps struct {
	application.StampQueue
	log *portLog
}

func (s loggedStamps) Append(ctx context.Context, stamp domain.Stamp) error {
	s.log.add("stamp %s at %s", stamp.Story, stamp.Commit)
	return s.StampQueue.Append(ctx, stamp)
}

// logged wraps the ports of n that a landing's tail calls, so that log holds
// what it asked of them in order.
func logged(n application.Next, lands *aLandingLanding, log *portLog) application.Next {
	n.Tracker = loggedTracker{WorkTracker: n.Tracker, log: log}
	n.Vault = loggedVault{fakeVault: n.Vault.(*fakeVault), log: log}
	l := loggedLanding{aLandingLanding: lands, log: log}
	n.Landing, n.Worktrees = l, l
	n.Mailbox = loggedMailbox{Mailbox: apptest.NewFakeMailbox(), log: log}
	n.Stamps = loggedStamps{StampQueue: apptest.NewFakeStampQueue(), log: log}
	return n
}

// aSingleLandingLogged lands mw-x.1 alone, through the whole close-out, and
// returns what its tail asked of the ports from the push on.
func aSingleLandingLogged(t *testing.T, trace func(string)) []string {
	t.Helper()
	log := &portLog{}
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	n := aCloseOutOn("desktop", "mw-x.1", aSlot{}, application.CrossSlot{}, lands, &scriptedChecks{passes: []bool{true}})
	n = logged(n, lands, log)
	if trace != nil {
		n = n.WithTailTrace(trace)
	}
	report, err := n.Run(context.Background(), "mw-x.1")
	if err != nil || !report.Landed {
		t.Fatalf("expected mw-x.1 to land, got %v and %+v", err, report)
	}
	return fromAdvance(log.seen())
}

// fromAdvance is what was asked from the rig's checkout's advance on, which on
// both paths comes after the push.
func fromAdvance(calls []string) []string {
	for i, call := range calls {
		if strings.HasPrefix(call, "advance the rig") {
			return calls[i:]
		}
	}
	return calls
}

// aBatchLandingLogged records as landed a batch of mw-x.1 and mw-x.2, wired
// further as wire says when it is not nil, and returns what its tail asked of
// the ports.
func aBatchLandingLogged(t *testing.T, trace func(string), release func(), wire func(*application.Next, *portLog)) []string {
	t.Helper()
	ctx := context.Background()
	log := &portLog{}
	tracker := apptest.NewFakeTracker()
	path := domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh}
	tracker.AddEpic("mw-x", path)
	report := &application.BatchReport{Rig: "millwright", Number: 3, Target: "main", Base: "origin/main", Commit: "batch0123456789"}
	for _, id := range []string{"mw-x.1", "mw-x.2"} {
		tracker.AddStory("mw-x", domain.Story{ID: id, Title: "The story " + id})
		mustDo(t, tracker.ClaimStory(ctx, id))
		detail, err := tracker.ShowStory(ctx, id)
		mustDo(t, err)
		report.Merged = append(report.Merged, application.MergedStory(id, "merge-"+id, detail, path))
	}
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	var out bytes.Buffer
	n := logged(application.Next{
		Tracker: tracker, Vault: newFakeVault(),
		Now:  func() time.Time { return time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC) },
		Seat: "builder", Host: "desktop", Out: &out, Err: &out,
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
	}, lands, log)
	if trace != nil {
		n = n.WithTailTrace(trace)
	}
	if wire != nil {
		wire(&n, log)
	}
	if release == nil {
		release = func() {}
	}
	application.BatchLand{Next: n}.RecordLanded(ctx, "/rigs/millwright", report, &application.NextReport{}, release)
	return log.seen()
}

// What the tail asks of the ports is what it asked on main before the tail was
// one table (mw-gq6.971): read off a run of this test on main at 79f5325c.
func TestTheLandedTailAsksThePortsWhatItAskedBeforeItWasOneTable(t *testing.T) {
	single := aSingleLandingLogged(t, nil)
	wantSingle := []string{
		"advance the rig to 0123456789abcdef",
		"run state mw-x.1",
		"stamp mw-x.1 at 0123456789abcdef",
		"remove worktree mw/mw-x.1",
		"run file mw-x.1/" + application.ResultFileNameForAttempt(1),
		"ledger",
		"close mw-x.1",
		"mail " + application.MailLanded + ": A story",
	}
	if !reflect.DeepEqual(single, wantSingle) {
		t.Errorf("a single landing's tail asked the ports\n  %s\nexpected\n  %s", strings.Join(single, "\n  "), strings.Join(wantSingle, "\n  "))
	}

	batch := aBatchLandingLogged(t, nil, nil, nil)
	wantBatch := []string{
		"run state mw-x.1",
		"stamp mw-x.1 at merge-mw-x.1",
		"ledger",
		"remove worktree mw/mw-x.1",
		"close mw-x.1",
		"run state mw-x.2",
		"stamp mw-x.2 at merge-mw-x.2",
		"ledger",
		"remove worktree mw/mw-x.2",
		"close mw-x.2",
		"advance the rig to batch0123456789",
		"mail " + application.MailLanded + ": The story mw-x.1",
		"mail " + application.MailLanded + ": The story mw-x.2",
	}
	if !reflect.DeepEqual(batch, wantBatch) {
		t.Errorf("a batch landing's tail asked the ports\n  %s\nexpected\n  %s", strings.Join(batch, "\n  "), strings.Join(wantBatch, "\n  "))
	}
}

// tailTrace gathers the steps of the landed tail a landing takes.
type tailTrace struct {
	mu    sync.Mutex
	steps []string
}

func (r *tailTrace) add(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
}

func (r *tailTrace) taken() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.steps...)
}

// The order of the steps is main's before the tail was one table: read off
// Next.land and Next.finish, and off BatchLand.recordLanded and the smoke it
// left for BatchLand.Run, at 79f5325c, call by call.
func TestEachLandingTakesTheStepsOfTheTailInTheOrderMainTookThem(t *testing.T) {
	single := &tailTrace{}
	aSingleLandingLogged(t, single.add)
	wantSingle := []string{
		"read the backend mw-x.1", "read the grist mw-x.1", "read the files outside the key mw-x.1",
		"advance the rig's checkout", "write the run state mw-x.1", "queue the chain stamp mw-x.1",
		"say the files outside the key mw-x.1", "run the after-landing command", "make the grist smoke",
		"stage the backend mw-x.1", "spring the Tester mw-x.1",
		"take the worktree away mw-x.1", "record the benchmark mw-x.1", "write the ledger line mw-x.1",
		"commit the vault mw-x.1", "close the story mw-x.1", "close the formula mw-x.1", "mail the Mayor mw-x.1",
		"look for abandoned stories mw-x.1", "carry on mw-x.1",
	}
	if got := single.taken(); !reflect.DeepEqual(got, wantSingle) {
		t.Errorf("a single landing took\n  %s\nexpected\n  %s", strings.Join(got, "\n  "), strings.Join(wantSingle, "\n  "))
	}

	batch := &tailTrace{}
	aBatchLandingLogged(t, batch.add, nil, nil)
	var wantBatch []string
	for _, id := range []string{"mw-x.1", "mw-x.2"} {
		for _, step := range []string{"read the grist", "read the backend", "read the files outside the key",
			"write the run state", "queue the chain stamp", "say the files outside the key", "record the benchmark",
			"write the ledger line", "commit the vault", "take the worktree away", "close the story", "close the formula",
			"spring the Tester"} {
			wantBatch = append(wantBatch, step+" "+id)
		}
	}
	wantBatch = append(wantBatch, "advance the rig's checkout", "run the after-landing command",
		"stage the backend mw-x.1", "stage the backend mw-x.2", "mail the Mayor mw-x.1", "mail the Mayor mw-x.2",
		"let the merge slot go", "make the grist smoke")
	if got := batch.taken(); !reflect.DeepEqual(got, wantBatch) {
		t.Errorf("a batch landing took\n  %s\nexpected\n  %s", strings.Join(got, "\n  "), strings.Join(wantBatch, "\n  "))
	}
}

// stepNames is the steps taken, each once, without the story they were taken for.
func stepNames(taken []string) map[string]int {
	names := map[string]int{}
	for _, step := range taken {
		for _, id := range []string{" mw-x.1", " mw-x.2"} {
			step = strings.TrimSuffix(step, id)
		}
		names[step]++
	}
	return names
}

// A step one landing takes and the other does not is a row of the table that
// says so, and why: a step added to one path alone fails here (mw-gq6.971).
func TestASingleLandingAndABatchTakeTheSameStepsButWhereTheTableSaysTheyDiffer(t *testing.T) {
	single, batch := &tailTrace{}, &tailTrace{}
	aSingleLandingLogged(t, single.add)
	aBatchLandingLogged(t, batch.add, nil, nil)
	ranSingle, ranBatch := stepNames(single.taken()), stepNames(batch.taken())

	steps, _, order := application.LandedTail()
	rows := map[string]application.LandedTailStep{}
	for _, step := range steps {
		rows[step.Name] = step
		if step.Only != "" && strings.TrimSpace(step.Why) == "" {
			t.Errorf("the step %q is taken by the %s landing alone and the table does not say why", step.Name, step.Only)
		}
		inSingle, inBatch := ranSingle[step.Name] > 0, ranBatch[step.Name] > 0
		switch step.Only {
		case "":
			if !inSingle || !inBatch {
				t.Errorf("the step %q is the table's for both landings, but the single landing took it %d times and the batch %d",
					step.Name, ranSingle[step.Name], ranBatch[step.Name])
			}
		case "single":
			if !inSingle || inBatch {
				t.Errorf("the step %q is the single landing's alone, but the single landing took it %d times and the batch %d",
					step.Name, ranSingle[step.Name], ranBatch[step.Name])
			}
		case "batch":
			if inSingle || !inBatch {
				t.Errorf("the step %q is the batch's alone, but the single landing took it %d times and the batch %d",
					step.Name, ranSingle[step.Name], ranBatch[step.Name])
			}
		default:
			t.Errorf("the step %q is said to be taken only by %q, which is no landing", step.Name, step.Only)
		}
		if ranSingle[step.Name] > 1 {
			t.Errorf("a single landing took the step %q %d times", step.Name, ranSingle[step.Name])
		}
		if want := map[bool]int{true: 1, false: 2}[step.OnceForBatch]; inBatch && ranBatch[step.Name] != want {
			t.Errorf("a batch of two stories took the step %q %d times, expected %d", step.Name, ranBatch[step.Name], want)
		}
	}
	for name := range ranSingle {
		if _, ok := rows[name]; !ok {
			t.Errorf("a single landing took %q, which is no row of the table", name)
		}
	}
	for name := range ranBatch {
		if _, ok := rows[name]; !ok {
			t.Errorf("a batch landing took %q, which is no row of the table", name)
		}
	}
	// A group of the batch's order is taken for each story or once: never both.
	for _, group := range order {
		for _, name := range group {
			if rows[name].OnceForBatch != rows[group[0]].OnceForBatch {
				t.Errorf("the batch's group %q mixes steps taken once with steps taken for each story", group)
			}
		}
	}
}

// The batch's slot rule (mw-gq6.626, mw-gq6.971): the merge slot is held
// through the rig's checkout, its after-landing command and the Landed mails,
// and let go before the grist smoke.

type deployWatch struct {
	held *bool
	log  *portLog
}

func (deployWatch) Command(string) string { return "make deploy" }

func (d deployWatch) Run(context.Context, string, string) (application.Ran, error) {
	d.log.add("after-landing ran, slot held %v", *d.held)
	return application.Ran{Command: "make deploy"}, nil
}

type smokeWatchHeld struct {
	held *bool
	log  *portLog
}

func (s smokeWatchHeld) Run(_ context.Context, app, _ string) (application.GristSmokeReport, error) {
	s.log.add("smoke of %s ran, slot held %v", app, *s.held)
	return application.GristSmokeReport{App: app, Examples: 1}, nil
}

type touchedAll struct{}

func (touchedAll) Changed(context.Context, string, string, string, string) (bool, error) {
	return true, nil
}

func TestABatchHoldsTheMergeSlotThroughTheCheckoutTheAfterLandingCommandAndTheMailsAndNotTheSmoke(t *testing.T) {
	held := true
	var log *portLog
	calls := aBatchLandingLogged(t, nil, func() {
		log.add("let the merge slot go")
		held = false
	}, func(n *application.Next, ports *portLog) {
		log = ports
		l := n.Landing.(loggedLanding)
		l.moved = true
		n.Landing, n.Worktrees = l, l
		n.AfterLanding = deployWatch{held: &held, log: ports}
		n.Smoke = application.GristSmokeAfter{
			Touches: touchedAll{}, Smoke: smokeWatchHeld{held: &held, log: ports},
			Book: application.GristSmokeBook{Notes: apptest.NewFakeTracker(), Host: "desktop"},
			Apps: map[string]string{"millwright": "/rigs/millwright"}, Rigs: map[string]string{"millwright": "/rigs/millwright"},
		}
	})

	var got []string
	for _, call := range calls {
		for _, prefix := range []string{"advance the rig", "after-landing ran", "mail " + application.MailLanded + ":", "let the merge slot go", "smoke of"} {
			if strings.HasPrefix(call, prefix) {
				got = append(got, call)
			}
		}
	}
	want := []string{
		"advance the rig to batch0123456789",
		"after-landing ran, slot held true",
		"mail " + application.MailLanded + ": The story mw-x.1",
		"mail " + application.MailLanded + ": The story mw-x.2",
		"let the merge slot go",
		"smoke of millwright ran, slot held false",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected the checkout, the after-landing command and the mails under the merge slot, and the smoke after it was let go, got\n  %s",
			strings.Join(got, "\n  "))
	}
}

// publishTags is taken straight after the push, in the directory the landing
// was made in, which the batch's landing takes away before its tail is walked:
// it is no row of the table, and the table says why (mw-gq6.973).
func TestTheTableSaysWhyPublishingTheTagsIsNoRowOfTheTail(t *testing.T) {
	steps, _, _ := application.LandedTail()
	rows := map[string]bool{}
	for _, step := range steps {
		rows[step.Name] = true
	}
	var found bool
	for _, outside := range application.OutsideTheTail() {
		if rows[outside.Name] {
			t.Errorf("the step %q is both a row of the tail and said to be outside it", outside.Name)
		}
		if strings.TrimSpace(outside.Why) == "" {
			t.Errorf("the step %q is outside the tail and the table does not say why", outside.Name)
		}
		found = found || outside.Name == "publish the release tags"
	}
	if !found {
		t.Errorf("the table does not name the step that publishes the release tags as outside the tail")
	}
}
