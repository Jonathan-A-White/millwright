package application

import (
	"context"
	"fmt"
	"strings"
)

// The landed tail is every step a landing takes of a story once the work is on
// the target branch, and the reads of the story's branch those steps need. A
// single landing (Next.land, and Next.finish for a close-out run again) and a
// batch landing (BatchLand.recordLanded) both walk the one table below, each in
// its own order. Before it, each path kept a copy of its own, and five stories
// found a step one copy took and the other did not (mw-gq6.961, .962, .963,
// .966, .969; mw-gq6.971). A new step is a row of landedTail and a name in
// both orders, or a row that says which path alone takes it and why.

// tailScope is how often a batch takes a step: a single landing has one story,
// so takes every step once.
type tailScope int

const (
	// perStory is a step taken for each story the batch landed.
	perStory tailScope = iota
	// onceForBatch is a step taken once for the batch, as its newest story's.
	onceForBatch
)

// tailPath names the landing that walks the tail.
type tailPath string

const (
	singleLanding tailPath = "single"
	batchLanding  tailPath = "batch"
)

// tailStep is one row of the landed tail.
type tailStep struct {
	name  string
	scope tailScope
	// only is the one path that takes the step, empty when both do; why says
	// why, and, for a step both take, what one path does that the other does not.
	only tailPath
	why  string
	run  func(t *tail, s *tailStory)
}

// The steps of the landed tail, by name.
const (
	stepReadBackend    = "read the backend"
	stepReadGrist      = "read the grist"
	stepReadFiles      = "read the files outside the key"
	stepAdvanceRig     = "advance the rig's checkout"
	stepRunState       = "write the run state"
	stepChainStamp     = "queue the chain stamp"
	stepSayFiles       = "say the files outside the key"
	stepAfterLanding   = "run the after-landing command"
	stepLetTheSlotGo   = "let the merge slot go"
	stepGristSmoke     = "make the grist smoke"
	stepStageBackend   = "stage the backend"
	stepSpringTester   = "spring the Tester"
	stepRemoveWorktree = "take the worktree away"
	stepBenchmark      = "record the benchmark"
	stepLedger         = "write the ledger line"
	stepCommit         = "commit the vault"
	stepClose          = "close the story"
	stepCloseFormula   = "close the formula"
	stepLandedMail     = "mail the Mayor"
	stepAbandoned      = "look for abandoned stories"
	stepCarryOn        = "carry on"
)

// landedTail names every step, in the order a single landing takes them.
var landedTail = []tailStep{
	{name: stepReadBackend, scope: perStory,
		why: "A single landing takes the three reads before it merges, against the target; a batch takes them after its push, " +
			"against the base it merged on, since the target holds the story by then (mw-gq6.963).",
		run: func(t *tail, s *tailStory) { t.n.readBackend(t.ctx, s.c, s.report, s.base, s.branch) }},
	{name: stepReadGrist, scope: perStory,
		why: "A batch smokes once, for every app any of its stories changed the grist of.",
		run: func(t *tail, s *tailStory) {
			apps, notes := t.n.Smoke.Wants(t.ctx, s.c.path.Rig, s.c.rigDir, s.base, s.branch)
			s.report.Notes = append(s.report.Notes, notes...)
			s.c.smokeApps = apps
			t.n.readSmokeElsewhere(t.ctx, s.c, s.base, s.branch)
			if t.batch != nil {
				t.batch.smokeApps = appendNew(t.batch.smokeApps, apps...)
				t.batch.unhosted = t.batch.unhosted || s.c.smokeUnhosted
			}
		}},
	{name: stepReadFiles, scope: perStory,
		run: func(t *tail, s *tailStory) { t.n.readFilesOutsideTheKey(t.ctx, s.c, s.report, s.base, s.branch) }},
	{name: stepAdvanceRig, scope: onceForBatch,
		run: func(t *tail, _ *tailStory) {
			t.n.advanceRig(t.ctx, t.last, t.once)
			if t.batch != nil {
				t.batch.report.Checkout = t.once.Rig
			}
		}},
	{name: stepRunState, scope: perStory,
		run: func(t *tail, s *tailStory) {
			t.n.writeOrSay(t.ctx, s.c, s.report, "the run state", fmt.Sprintf("%s could not be recorded as %s=%s", s.c.id, RunState, RunLanded),
				func() error { return t.n.Tracker.SetStoryState(t.ctx, s.c.id, RunState, RunLanded, s.outcome) })
		}},
	{name: stepChainStamp, scope: perStory,
		why: "A batch stamps each story at its own merge into the batch, a single landing at the commit it pushed, and " +
			"before the after-landing command, which may be killed with the session it runs in.",
		run: func(t *tail, s *tailStory) { t.n.queueStamp(t.ctx, s.c, s.report, Landed{Commit: s.merge}) }},
	{name: stepSayFiles, scope: perStory,
		run: func(t *tail, s *tailStory) { t.n.sayFilesOutsideTheKey(t.ctx, s.c, s.report) }},
	{name: stepAfterLanding, scope: onceForBatch,
		why: "A batch keeps the command's log with the batch's records, for it commits no story's run.",
		run: func(t *tail, _ *tailStory) {
			t.n.afterLanding(t.ctx, t.last, t.once)
			if t.batch == nil {
				return
			}
			b := t.batch
			if record := runFilePath(t.last.id, t.last.afterLog); t.last.afterLog != "" && record != "" {
				b.report.Records = append(b.report.Records, record)
			}
			b.land.say("after landing done: the rig's checkout is brought up and its command run or skipped")
			b.nr.Notes = append(b.nr.Notes, t.once.Notes...)
		}},
	{name: stepLetTheSlotGo, scope: onceForBatch, only: batchLanding,
		why: "A single landing gives the rig's merge slot back as soon as it has pushed. A batch holds it through the rig's checkout, " +
			"the after-landing command and the Landed mails, since another landing of the rig would move the checkout under them, " +
			"and lets it go before the grist smoke, which is minutes for each example of each app (mw-gq6.626).",
		run: func(t *tail, _ *tailStory) { t.batch.release() }},
	{name: stepGristSmoke, scope: onceForBatch,
		why: "A smoke that fails after a batch also mails the Mayor, naming the batch.",
		run: func(t *tail, _ *tailStory) {
			if t.batch != nil {
				t.batch.land.smoke(t.ctx, t.last, t.once, t.batch.report, t.batch.nr)
				return
			}
			t.n.smoke(t.ctx, t.last, t.once)
		}},
	{name: stepStageBackend, scope: perStory,
		why: "Both stage at the commit the target branch holds: the batch's commit, for a batch; two stories that changed " +
			"the backend are then one swap (BackendStage.stage files one bead for a step already filed).",
		run: func(t *tail, s *tailStory) {
			if t.batch == nil {
				t.n.stageBackend(t.ctx, s.c, s.report, s.atTarget)
				return
			}
			var staged NextReport
			t.n.stageBackend(t.ctx, s.c, &staged, s.atTarget)
			for _, note := range staged.Notes {
				t.batch.land.say("%s", note)
			}
			s.report.Notes = append(s.report.Notes, staged.Notes...)
		}},
	{name: stepSpringTester, scope: perStory,
		why: "Both file the Tester at the commit the target branch holds: what the Tester drives of a batch is its result (mw-gq6.961).",
		run: func(t *tail, s *tailStory) {
			if t.batch == nil {
				t.n.springTester(t.ctx, s.c, s.report, Landed{Commit: s.atTarget})
				return
			}
			var springed NextReport
			t.n.springTester(t.ctx, s.c, &springed, Landed{Commit: s.atTarget})
			s.report.Notes = append(s.report.Notes, springed.Notes...)
			s.testerFiled = springed.TesterFiled
			if springed.TesterFiled != "" {
				t.batch.land.say("filed the Tester story %s for %s", springed.TesterFiled, s.c.id)
			}
		}},
	{name: stepRemoveWorktree, scope: perStory,
		run: func(t *tail, s *tailStory) {
			if err := t.n.Worktrees.Remove(t.ctx, s.c.rigDir, s.c.worktree, s.c.branch); err != nil {
				s.report.Notes = append(s.report.Notes, fmt.Sprintf("the worktree %s could not be taken away: %v", s.c.worktree, err))
			}
		}},
	{name: stepBenchmark, scope: perStory,
		why: "A story a batch landed records the batch's gate, under which it landed (mw-gq6.966); a single landing run again " +
			"writes neither the benchmark nor the ledger line when the ledger holds the line already.",
		run: func(t *tail, s *tailStory) {
			if s.again {
				held, err := t.n.ledgered(t.ctx, s.c.id)
				if err != nil {
					s.report.Notes = append(s.report.Notes, err.Error())
				}
				s.ledgered = held
			}
			if s.ledgered {
				return
			}
			if t.batch == nil {
				t.n.recordBenchmark(t.ctx, s.c, s.report, true)
				return
			}
			s.c.gate = t.batch.report.gate
			var benched NextReport
			if t.n.recordBenchmark(t.ctx, s.c, &benched, true) {
				if record := RunRecordForAttempt(s.c.id, s.c.attempt()); record != "" {
					s.written = append(s.written, record)
				}
			}
			s.report.Notes = append(s.report.Notes, benched.Notes...)
		}},
	{name: stepLedger, scope: perStory,
		why: "A batch's line names the batch and the stories landed with it, and charges no fuel: the line that queued the story did.",
		run: func(t *tail, s *tailStory) {
			if s.ledgered {
				return
			}
			if t.batch == nil {
				if err := t.n.ledger(t.ctx, s.c, s.report, s.outcome); err != nil {
					s.report.Notes = append(s.report.Notes, err.Error())
				}
				return
			}
			b := t.batch
			s.line = LedgerLine{
				When: t.n.now(), StoryID: s.story.ID, Title: s.story.Title, Outcome: s.outcome, Path: s.story.path,
				// The session's fuel was charged by the line that queued it.
				FuelCharged: true,
				Notes:       append([]string{"landed by mw land on " + t.n.Host, b.report.Label() + ": " + strings.Join(b.together, ", ")}, b.report.fixerNotes(s.story.ID)...),
			}.String()
			if err := t.n.Vault.AppendToLedger(t.ctx, t.n.Seat, s.line); err != nil {
				s.report.Notes = append(s.report.Notes, fmt.Sprintf("the line for %s could not be appended to the %s seat's ledger: %v", s.story.ID, t.n.Seat, err))
			}
		}},
	{name: stepCommit, scope: perStory,
		why: "A batch commits the ledger and the story's result file at once, before the worktree comes away, the story is " +
			"closed and the mail goes, so that a stop anywhere after leaves the vault clean (mw-gq6.502); its records are committed after.",
		run: func(t *tail, s *tailStory) {
			if t.batch == nil {
				// Committed whether this run wrote the line or an earlier one did:
				// a line written by a run that could not commit it is still standing
				// in the way of every sync until somebody commits it, and this is
				// the run that can.
				t.n.commit(t.ctx, s.c, s.report)
				return
			}
			t.batch.land.commitLedger(t.ctx, t.batch.report, s.written...)
		}},
	{name: stepClose, scope: perStory,
		why: "A demo closes on the Governor's \"Looks good\" alone: it is left open for him, not closed (mw-gq6.616). " +
			"A batch lists on its report what it closed and held.",
		run: func(t *tail, s *tailStory) {
			c := s.c
			if isDemo(c.detail) {
				s.closeErr = t.n.holdDemo(t.ctx, c, s.report, s.outcome)
				s.heldDemo = s.closeErr == nil
			} else {
				notes, err := t.n.closeStory(t.ctx, c.id, s.outcome)
				s.report.Notes = append(s.report.Notes, notes...)
				s.closeErr, s.closed = err, err == nil
			}
			if t.batch == nil {
				switch {
				case s.closeErr != nil:
					s.report.NotClosed = s.closeErr.Error()
				case s.heldDemo:
					s.report.DemoHeld = true
				default:
					s.report.Closed = true
				}
				return
			}
			b := t.batch.report
			switch {
			case s.closeErr != nil && isDemo(c.detail):
				s.report.Notes = append(s.report.Notes, fmt.Sprintf("%s is landed but the demo could not be left waiting for the Governor: %v", c.id, s.closeErr))
			case s.closeErr != nil:
				s.report.Notes = append(s.report.Notes, fmt.Sprintf("%s is landed but could not be closed: %v", c.id, s.closeErr))
			case s.heldDemo:
				b.DemoHeld = append(b.DemoHeld, c.id)
			default:
				b.Closed = append(b.Closed, c.id)
			}
		}},
	{name: stepCloseFormula, scope: perStory,
		why: "Both close the formula of a story closed and of a demo left waiting for the Governor (mw-gq6.972); " +
			"only the reason differs: a single landing's says \"was tested\" for a story the Tester tested, a batch's says \"landed\".",
		run: func(t *tail, s *tailStory) {
			if t.batch == nil {
				if s.closeErr == nil {
					what := "landed"
					if s.c.tested != "" {
						what = "was tested"
					}
					t.n.closeFormula(t.ctx, s.c, s.report, what)
				}
				return
			}
			if s.closed || s.heldDemo {
				var shut NextReport
				t.n.closeFormula(t.ctx, s.c, &shut, "landed")
				s.report.Notes = append(s.report.Notes, shut.Notes...)
			}
			if s.closed || s.heldDemo {
				t.batch.land.say("closed %s", s.c.id)
			}
		}},
	{name: stepLandedMail, scope: perStory,
		why: "Told once: a single landing run again only to close the story has nothing new for the Mayor. A batch's mail " +
			"names the batch, and says first that the after-landing command did not run when it did not.",
		run: func(t *tail, s *tailStory) {
			if t.batch == nil {
				verdict, said := t.n.closingMail(t.ctx, s.c, s.report, s.again)
				if !s.again {
					t.n.mailTheMayor(t.ctx, s.c, s.report, verdict, said)
				}
				return
			}
			b, report := t.batch, t.batch.report
			mailed := NextReport{
				StoryID: s.story.ID, Host: t.n.Host, Title: s.story.Title, Target: report.Target, Commits: s.story.Commits, Pushes: 1,
				Tags:   report.tags.ForStory(s.story.detail.Acceptance),
				Landed: true, How: Landed{Commit: s.story.Merge, FastForward: s.story.FastForward, Version: report.Version, Unioned: s.story.Unioned},
				TesterFiled: s.testerFiled,
				Ledger:      s.line, Closed: s.closed, DemoHeld: s.heldDemo, NotClosed: "see the batch's report",
				Notes: append([]string{fmt.Sprintf("landed by mw land in %s (%s), whose commit %s is on %s", report.Label(), strings.Join(b.together, ", "),
					shortCommit(report.Commit), report.Target)}, report.fixerNotes(s.story.ID)...),
				AfterLandingSkipped: t.once.AfterLandingSkipped,
			}
			verdict, said := t.n.closingMail(t.ctx, s.c, &mailed, false)
			t.n.mailTheMayor(t.ctx, s.c, &mailed, verdict, said)
			report.Mailed = append(report.Mailed, MailLanded+": "+s.c.id)
			b.land.say("mailed %s: %s", MailLanded, s.c.id)
		}},
	{name: stepAbandoned, scope: perStory, only: singleLanding,
		why: "Per landing: mw next looks at what else runs on the host once its story is closed; mw land lands no session's work.",
		run: func(t *tail, s *tailStory) {
			if s.closeErr != nil {
				s.err = fmt.Errorf("closing out %s: it %s, but the story could not be closed, so it is landed and still open: %w",
					s.c.id, s.outcome, s.closeErr)
				return
			}
			s.report.Abandoned = t.n.abandoned(t.ctx, s.c.id, s.report)
		}},
	{name: stepCarryOn, scope: perStory, only: singleLanding,
		why: "Per landing: mw next brings the hosts level and dispatches once its story is closed; mw land's caller does that for a batch.",
		run: func(t *tail, s *tailStory) { _, s.err = t.n.carryOn(t.ctx, s.c, s.report) }},
}

// singleReads, singleAfterPush and singleFinish are the order a single landing
// walks the tail in. The reads come before its merge slot is taken, against
// the target its branch was cut from.
// After the push, the rig's checkout is brought up first and the after-landing
// command, the smoke and the stage made before the story is closed; the run
// state is written before them, because it is what tells a later run that the
// story is landed, and a command killed with the session it runs in leaves a
// story a later run can tell is landed. finish is the rest, which a close-out
// run again on a landed story takes too.
var (
	singleReads     = []string{stepReadBackend, stepReadGrist, stepReadFiles}
	singleAfterPush = []string{stepAdvanceRig, stepRunState, stepChainStamp, stepSayFiles, stepAfterLanding, stepGristSmoke, stepStageBackend, stepSpringTester}
	singleFinish    = []string{stepRemoveWorktree, stepBenchmark, stepLedger, stepCommit, stepClose, stepCloseFormula, stepLandedMail, stepAbandoned, stepCarryOn}
)

// batchOrder is the order a batch landing walks the tail in, each group taken
// for each story in turn, or once for the batch. Each story is recorded, its
// ledger line committed and the story closed, before anything long runs, so
// that the vault is not left dirty while the checkout, the after-landing
// command and the smoke run for many minutes (mw-gq6.493, mw-gq6.502). The
// backend is staged once the after-landing command has run, as a story's is,
// and a story's mail goes once that command has run or been skipped, so that
// it can say first that the deploy did not happen; the mails go before the
// smoke, so that the Mayor is told of a landing when it is made (mw-gq6.626).
var batchOrder = [][]string{
	{stepReadGrist, stepReadBackend, stepReadFiles, stepRunState, stepChainStamp, stepSayFiles, stepBenchmark, stepLedger,
		stepCommit, stepRemoveWorktree, stepClose, stepCloseFormula, stepSpringTester},
	{stepAdvanceRig, stepAfterLanding},
	{stepStageBackend},
	{stepLandedMail},
	{stepLetTheSlotGo, stepGristSmoke},
}

// landedTailStep is the row of the tail named name.
func landedTailStep(name string) tailStep {
	for _, step := range landedTail {
		if step.name == name {
			return step
		}
	}
	panic("mw: the landed tail has no step " + name)
}

// tail is one walk of the landed tail.
type tail struct {
	n   Next
	ctx context.Context
	// last and once are what a step taken once for the batch is taken on: the
	// story itself and its report, on a single landing; the batch's newest
	// story and a report of the batch's own, on a batch.
	last  *closeOut
	once  *NextReport
	batch *batchTail
}

// tailStory is one story the tail is walked for, and what its steps leave for
// the steps after them.
type tailStory struct {
	c *closeOut
	// report is where the steps' notes go: the close-out's own report on a
	// single landing, the notes the batch gathers on a batch.
	report  *NextReport
	outcome string
	// again is a close-out run again on a story an earlier one landed.
	again bool
	// base and branch are what the reads compare.
	base, branch string
	// merge is the commit that put the story on the target: the pushed commit,
	// or the story's merge into the batch. atTarget is the commit the target
	// branch holds: the pushed commit, or the batch's.
	merge, atTarget string

	ledgered         bool
	written          []string
	line             string
	closeErr         error
	closed, heldDemo bool
	testerFiled      string
	// err ends the walk: a story that could not be closed, or carryOn's.
	err error

	story BatchStory
}

// batchTail is what a batch's walk of the tail holds besides a single
// landing's.
type batchTail struct {
	land     BatchLand
	report   *BatchReport
	nr       *NextReport
	rigDir   string
	together []string
	release  func()
	stories  []*tailStory

	smokeApps []string
	// unhosted says some story changed grist this host cannot smoke, because it
	// names no [grist-apps]: the batch says who smokes it, as a story does.
	unhosted bool
}

// walk takes the steps named, in order, for s; a step taken once for a batch
// is taken on the tail's last and once. It stops at a step that ends the walk.
func (t *tail) walk(steps []string, s *tailStory) {
	for _, name := range steps {
		if s != nil && s.err != nil {
			return
		}
		t.take(landedTailStep(name), s)
	}
}

// take runs one step, naming it to the trace first: with the story's id when
// it is taken for each story.
func (t *tail) take(step tailStep, s *tailStory) {
	if t.n.tailTrace != nil {
		said := step.name
		if step.scope == perStory && s != nil {
			said += " " + s.c.id
		}
		t.n.tailTrace(said)
	}
	step.run(t, s)
}

// singleTail is the walk of a single landing of c.
func (n Next) singleTail(ctx context.Context, c *closeOut, report *NextReport) (*tail, *tailStory) {
	return &tail{n: n, ctx: ctx, last: c, once: report}, &tailStory{c: c, report: report}
}

// walkFinish takes the steps of finish, and ends the close-out as the last of
// them says.
func (t *tail) walkFinish(s *tailStory) (NextReport, error) {
	t.walk(singleFinish, s)
	return *s.report, s.err
}

// walkBatch takes the batch's groups in order.
func (t *tail) walkBatch() {
	b := t.batch
	for _, group := range batchOrder {
		if landedTailStep(group[0]).scope == onceForBatch {
			t.onceForBatch()
			for _, name := range group {
				t.take(landedTailStep(name), nil)
			}
			continue
		}
		for i := range b.report.Merged {
			s := b.story(i)
			for _, name := range group {
				t.take(landedTailStep(name), s)
			}
		}
	}
}

// story is the i'th story the batch merged, as the tail walks it, made the
// first time it is asked for.
func (b *batchTail) story(i int) *tailStory {
	for len(b.stories) <= i {
		b.stories = append(b.stories, nil)
	}
	if b.stories[i] != nil {
		return b.stories[i]
	}
	s, report := b.report.Merged[i], b.report
	how := "its merge"
	if s.FastForward {
		how = "its fast-forward"
	}
	version := ""
	if report.Version != "" {
		version = ", version " + report.Version
	}
	b.stories[i] = &tailStory{
		c: b.land.storyCloseOut(s, b.rigDir), report: b.nr,
		outcome: fmt.Sprintf("landed on %s in %s (batch commit %s, %s %s%s), %d commits, the rig's tests on the batch: pass",
			report.Target, report.Label(), shortCommit(report.Commit), how, shortCommit(s.Merge), version, s.Commits),
		base: report.Base, branch: s.Branch, merge: s.Merge, atTarget: report.Commit, story: s,
	}
	return b.stories[i]
}

// onceForBatch makes, the first time a step is taken once, what it is taken
// on: the batch's newest story, holding every app the batch's stories changed
// the grist of, and a report of the batch's own at its commit.
func (t *tail) onceForBatch() {
	if t.last != nil {
		return
	}
	b := t.batch
	t.last = b.land.storyCloseOut(b.report.Merged[len(b.report.Merged)-1], b.rigDir)
	t.last.smokeApps, t.last.smokeUnhosted = b.smokeApps, b.unhosted
	t.once = &NextReport{StoryID: b.report.Label(), Host: t.n.Host, Target: b.report.Target, How: Landed{Commit: b.report.Commit}}
}
