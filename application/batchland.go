package application

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// BatchDir is where mw land keeps what each batch did: runs/batch/<rig>/<n>/,
// numbered from 1 per rig.
const BatchDir = "runs/batch"

// The files a batch's records hold: the order its branches were merged in and
// what became of each, the whole output of its one gate run, and its outcome,
// whose first word is "landed" or "held".
const (
	BatchOrderFile   = "merge-order.txt"
	BatchGateLogFile = "gate.log"
	BatchOutcomeFile = "outcome.txt"
	// BatchFixerFile is what became of the batch's fixer, when it had one:
	// why it was started, how it ended, and its word on what it touched.
	BatchFixerFile = "fixer.txt"
)

// The subjects of mw land's mail to the Mayor about a batch that did not land
// whole: Held when nothing of it landed, Set aside when the rest of it did.
// Each story that landed is mailed Landed, as mw next mails one.
const (
	MailHeld     = "Held"
	MailSetAside = "Set aside"
	// MailNotCommitted is the subject when a batch landed whole but its records
	// could not be committed in the vault (mw-gq6.554).
	MailNotCommitted = "Not committed"
)

// BatchRecords is the port a batch's records are kept through, in the vault
// under BatchDir.
type BatchRecords interface {
	// NewBatch makes the directory of the rig's next batch and reports its
	// number: one more than the highest already there, 1 for the first.
	NewBatch(ctx context.Context, rig string) (int, error)

	// PutBatchFile writes one file of a batch's records and reports its path
	// from the vault's root.
	PutBatchFile(ctx context.Context, rig string, number int, name, contents string) (string, error)
}

// BatchHistory reads the batch lines back, oldest first; none and no error when
// there are none.
type BatchHistory interface {
	Batches(ctx context.Context) ([]BatchMetric, error)
}

// BatchMetrics is where mw land keeps its batch lines, in the same appended
// file the daily snapshots are in. The real adapter is infrastructure/vault;
// the fake is apptest.FakeBatchMetrics.
type BatchMetrics interface {
	BatchHistory
	AppendBatch(ctx context.Context, metric BatchMetric) error
}

// BatchLand is mw land <rig> (plans/0117, Land Rush 3): it lands the stories
// mw next queued on a rig that lands in batches, several under one gate run.
//
// In a landing worktree detached at the target branch as the origin has it, it
// merges each queued story's branch as the origin has it (mw/<id>), oldest
// queued first, by the rig's union rules; a branch that does not merge is set
// aside with git's conflict list, and one with a commit signed as a machine's
// work (AIAttribution) is set aside, never merged, naming the commit. It
// raises the rig's version once for the batch, with one changelog entry per
// story; it runs the rig's tests once on the batch, under the host's gate lock and the calm rule; and only when that
// run passes does it push the target branch, which is a fast-forward of what
// the origin had, never forced. Each story merged is then closed run=landed,
// naming the batch's commit and its own merge commit, with a chain stamp and a
// ledger line naming the batch; the rig's checkout is brought up, and the
// after-landing command and the grist smoke run once. A set-aside story stays
// queued, and the Mayor is mailed its name and its conflict.
//
// A red gate, a batch of which nothing merged, and a push that fails push
// nothing: the batch is held, every story in it stays queued, and the Mayor is
// mailed each story and why. The fixer that mends a held batch is Land Rush 5's;
// a batch of which nothing merged only because every branch conflicts is given
// it too.
//
// What the batch did is kept under runs/batch/<rig>/<n>/ either way. A dry run
// reads the queue and prints the batch it would make, and does nothing else.
type BatchLand struct {
	// Next is the close-out mw next is, and is used for what a landing does the
	// same way for a story and for a batch: its ports, its fetch and push tried
	// again on passing faults, its gate under the gate lock and the calm rule,
	// its changelog entries, its chain stamps, its after-landing command and
	// smoke, and its mail.
	Next    Next
	Records BatchRecords
	// Metrics, when set, is given a line for the batch once it is over
	// (BatchMetric), whether it landed or was held. Nil keeps none.
	Metrics BatchMetrics
	DryRun  bool
	// Fix lets a batch that was set aside or red be given its one fixer
	// (Land Rush 5): a session of the seat, assembled by Next.Boot and run
	// through Next.Runner, in the batch's landing worktree. Without it such a
	// batch is landed or held as Land Rush 3 lands or holds it.
	Fix bool
	// One takes only the oldest queued story, whatever else is queued: how a
	// rig flipped back to landing = each lands what was left in its queue, one
	// story at a time (the follower's batch-land job, Land Rush 4).
	One bool
}

// BatchStory is one queued story as a batch took it, in the order it was
// merged.
type BatchStory struct {
	ID       string
	Title    string
	QueuedAt time.Time
	// Branch is the story's branch as the origin has it, which is what is
	// merged: a story queued on the other host has no branch here of its own.
	Branch  string
	Commits int
	// OnTarget says the branch held nothing beyond the target when the batch
	// merged it: another landing had put its work there already, and wrote its
	// What's new line, so the batch writes none for it (mw-gq6.514).
	OnTarget bool

	// Merge is the commit the batch was at once this story was merged into it,
	// FastForward that the merge simply moved onto its branch, and Unioned the
	// files the rig's union rules settled. Empty when it was set aside.
	Merge       string
	FastForward bool
	Unioned     []string

	// Why says why it was set aside, and Conflicts are git's conflict lines
	// when it was because the branch conflicts with the batch.
	Why       string
	Conflicts []string
	// ByFixer says the story was set aside and the batch's fixer merged it.
	ByFixer bool

	detail StoryDetail
	path   domain.Path
}

// BatchReport is what mw land did, or with DryRun what it would do.
type BatchReport struct {
	Rig    string
	Host   string
	Mode   domain.LandingMode
	Target string
	Number int
	DryRun bool

	// Base is the commit the target branch was at on the origin when the batch
	// began; Commit the batch's commit, pushed when Landed.
	Base   string
	Commit string

	// Queue is every story the batch took, oldest queued first; Merged and
	// SetAside are the two parts of it.
	Queue    []BatchStory
	Merged   []BatchStory
	SetAside []BatchStory

	Version string
	Tested  bool
	// Gate is the last run of the rig's tests on the batch: the one, or the
	// one mw made once the fixer had ended.
	Gate Checked
	// GateRuns are the runs of the rig's tests the batch made, and GateTook
	// how long they took together.
	GateRuns int
	GateTook time.Duration
	// RedAt is when the rig's tests first failed on the batch; zero when they
	// never did.
	RedAt time.Time
	// Fixer is the one fixer the batch was given (Land Rush 5), nil when it
	// was given none.
	Fixer *BatchFixing

	Landed bool
	// Held is why nothing of the batch landed, empty when it landed.
	Held string

	// Closed are the stories closed once landed; Checkout what became of the
	// rig's own checkout.
	Closed   []string
	Checkout Advanced
	RigDir   string

	Records   []string
	Committed []string
	Mailed    []string
	Notes     []string
}

// Label names the batch in a sentence: "batch 3 of lampas".
func (r BatchReport) Label() string {
	return fmt.Sprintf("batch %d of %s", r.Number, r.Rig)
}

// gateOnTheBatch is the one run of the rig's tests, as what it says names it.
const gateOnTheBatch = "the rig's tests on the batch"

// Run lands the queue of rig, or with DryRun says what it would land. The
// error is what stopped it, or the batch held; a batch of which some stories
// landed and some were set aside is no error.
func (b BatchLand) Run(ctx context.Context, rig string) (BatchReport, error) {
	n := b.Next
	// A batch is not a story: no close-out mark is written for it.
	n.CloseOuts = nil
	report := BatchReport{Rig: rig, Host: n.Host, DryRun: b.DryRun}
	switch {
	case n.Tracker == nil || n.Vault == nil || n.Worktrees == nil || n.Landing == nil || n.Checks == nil || n.Slot == nil || b.Records == nil:
		return report, fmt.Errorf("landing a batch: it needs a work tracker, a vault, worktrees, a landing, the rig's checks, a merge slot and the batch records")
	case n.Host == "":
		return report, fmt.Errorf("landing a batch: which host is this? set MW_HOST, or host in the config file")
	case strings.TrimSpace(rig) == "":
		return report, fmt.Errorf("landing a batch: of which rig?")
	}
	rigDir, checkedOut := n.Rigs[rig]
	if !checkedOut {
		return report, fmt.Errorf("landing a batch of %s: the rig is not checked out on %s: add it under [rigs] in the config file", rig, n.Host)
	}
	report.RigDir = rigDir

	var rules domain.EpicRequirements
	if n.Rules != nil {
		read, unknown, err := n.landingRequirements(ctx, rig)
		if err != nil {
			return report, fmt.Errorf("landing a batch of %s: the rig's file could not be read: %w", rig, err)
		}
		rules = read
		for _, key := range unknown {
			report.Notes = append(report.Notes, fmt.Sprintf("%s/%s.toml has a key this mw does not know: %s (rebuild mw on %s)", RigsDir, rig, key, n.Host))
		}
		for _, warning := range rules.Warnings {
			report.Notes = append(report.Notes, fmt.Sprintf("%s/%s.toml: %s", RigsDir, rig, warning))
		}
	}
	report.Mode = rules.LandingSettings().Mode

	// What the close-out's helpers note on the way, gathered into the report.
	var nr NextReport
	gather := func() {
		report.Notes = append(report.Notes, nr.Notes...)
		nr.Notes = nil
	}
	noted := len(report.Notes)
	if err := b.readQueue(ctx, rig, rigDir, &report, &nr); err != nil {
		gather()
		return report, err
	}
	if len(report.Queue) == 0 {
		n.print(report.String())
		return report, nil
	}

	if b.DryRun {
		b.dryRun(ctx, rigDir, &report)
		gather()
		n.print(report.String())
		return report, nil
	}

	// The rig's merge slot on this host, as a close-out takes it: no other
	// landing of the rig here while the batch is made, tested and pushed.
	holding, err := n.Slot.Take(ctx, rigDir, fmt.Sprintf("mw land on %s, landing a batch of %s", n.Host, rig))
	if err != nil {
		gather()
		return report, fmt.Errorf("landing %s: the merge slot could not be taken: %w", rig, err)
	}
	release := func() {
		if err := holding.Release(ctx); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the merge slot of %s could not be given back: %v", rig, err))
		}
	}
	// The queue and the target are read again now that the slot is ours. A
	// batch that waited for it while the batch before it was made, tested and
	// pushed read a queue that batch has since landed and a target it has since
	// moved: landing those stories again wrote their What's new lines a second
	// time, under the version the batch before had raised (mw-gq6.514).
	report.Notes = report.Notes[:noted]
	if err := b.readQueue(ctx, rig, rigDir, &report, &nr); err != nil {
		release()
		gather()
		return report, err
	}
	if len(report.Queue) == 0 {
		release()
		report.Notes = append(report.Notes, "the batch before this one landed the whole queue while this one waited for the merge slot")
		gather()
		n.print(report.String())
		return report, nil
	}
	if report.Number, err = b.Records.NewBatch(ctx, rig); err != nil {
		release()
		gather()
		return report, fmt.Errorf("landing %s: the batch's records could not be begun: %w", rig, err)
	}

	// The landing is cut at the very commit read as the base, never at the
	// branch's name: the version is raised from what the base holds, and a
	// landing cut later than its base would take the version the batch before
	// raised for one the batch raised itself.
	dir, err := n.Landing.OpenLanding(ctx, rigDir, report.Base)
	if err != nil {
		report.Held = fmt.Sprintf("a landing of %s at %s could not be opened: %v", rig, shortCommit(report.Base), err)
	} else {
		b.landIn(ctx, dir, rigDir, rules, &report, &nr)
		if err := n.Landing.CloseLanding(ctx, rigDir, dir); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the landing worktree %s could not be taken away: %v", dir, err))
		}
	}

	if report.Landed {
		b.recordLanded(ctx, rigDir, &report, &nr)
	}
	release()
	b.keepRecords(ctx, &report)
	b.keepMetric(ctx, &report)
	b.mailTheMayor(ctx, &report, &nr)
	b.commit(ctx, &report)
	gather()
	n.print(report.String())
	if report.Held != "" {
		return report, fmt.Errorf("%s was held: %s", report.Label(), firstLine(report.Held))
	}
	return report, nil
}

// readQueue reads the batch as it stands: the stories queued for it, then, when
// there are any, the rig brought up to date with the remote, the target's head
// as Base and each story's branch at the remote.
func (b BatchLand) readQueue(ctx context.Context, rig, rigDir string, report *BatchReport, nr *NextReport) error {
	n := b.Next
	queue, err := b.queued(ctx, rig, report)
	if err != nil {
		return err
	}
	report.Queue = queue
	if len(queue) == 0 {
		return nil
	}
	report.Target = queue[0].path.Branch
	if err := n.fetchRetrying(ctx, nr, rigDir); err != nil {
		return fmt.Errorf("landing %s: the rig could not be brought up to date with %s: %w", rig, n.remote(), err)
	}
	base := StartPoint(n.remote(), report.Target)
	if report.Base, err = n.Landing.RefHead(ctx, rigDir, base); err != nil {
		return fmt.Errorf("landing %s: %s could not be read: %w", rig, base, err)
	}
	for i := range report.Queue {
		s := &report.Queue[i]
		s.Branch = StartPoint(n.remote(), StoryBranch(s.ID))
	}
	return nil
}

// queued are the stories queued for batch landing on rig, on any host,
// oldest queued first, all targeting the branch the oldest targets.
func (b BatchLand) queued(ctx context.Context, rig string, report *BatchReport) ([]BatchStory, error) {
	n := b.Next
	work, err := readRetrying(ctx, n, nil, "the claimed stories", func() (WorkInHand, error) { return n.Tracker.WorkInHand(ctx) })
	if err != nil {
		return nil, fmt.Errorf("landing a batch of %s: the queued stories could not be read: %w", rig, err)
	}
	var queue []BatchStory
	for _, detail := range work.Running {
		if !QueuedForBatch(detail) {
			continue
		}
		path, err := detail.Path()
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s is queued, but its path cannot be read, so it is left out: %v", detail.Story.ID, err))
			continue
		}
		if path.Rig != rig {
			continue
		}
		queue = append(queue, BatchStory{ID: detail.Story.ID, Title: detail.Story.Title, QueuedAt: detail.QueuedAt, detail: detail, path: path})
	}
	SortOldestQueued(queue)
	if len(queue) == 0 {
		return nil, nil
	}
	target := queue[0].path.Branch
	kept := queue[:0]
	for _, s := range queue {
		if s.path.Branch != target {
			report.Notes = append(report.Notes, fmt.Sprintf("%s targets %s, not %s, so it waits for a batch of its own", s.ID, s.path.Branch, target))
			continue
		}
		kept = append(kept, s)
	}
	if b.One && len(kept) > 1 {
		kept = kept[:1]
	}
	return kept, nil
}

// SortOldestQueued puts a queue in the order a batch merges it: oldest queued
// first. A story with no queued time was queued by an mw that recorded none,
// before any that has one; ties go by when the story was first claimed, then
// by id, so that the order never depends on the tracker's.
func SortOldestQueued(queue []BatchStory) {
	sort.SliceStable(queue, func(i, j int) bool {
		a, b := queue[i], queue[j]
		if !a.QueuedAt.Equal(b.QueuedAt) {
			return a.QueuedAt.Before(b.QueuedAt)
		}
		if !a.detail.Started.Equal(b.detail.Started) {
			return a.detail.Started.Before(b.detail.Started)
		}
		return a.ID < b.ID
	})
}

// dryRun reads what the batch would merge: each story's branch at the origin,
// and how many commits it holds beyond the target. Nothing is merged.
func (b BatchLand) dryRun(ctx context.Context, rigDir string, report *BatchReport) {
	for i := range report.Queue {
		s := &report.Queue[i]
		if _, err := b.Next.Landing.RefHead(ctx, rigDir, s.Branch); err != nil {
			s.Why = fmt.Sprintf("its branch %s is not at the rig's origin", s.Branch)
			continue
		}
		if ahead, err := b.Next.Landing.Ahead(ctx, rigDir, s.Branch, report.Base); err == nil {
			s.Commits = ahead
		}
	}
}

// landIn makes the batch in the landing worktree dir: merges, the version, the
// one gate run and the push. It says Landed, or Held and why.
func (b BatchLand) landIn(ctx context.Context, dir, rigDir string, rules domain.EpicRequirements, report *BatchReport, nr *NextReport) {
	n := b.Next
	b.mergeAll(ctx, dir, rigDir, rules.UnionMergeFiles, report)
	// A branch set aside for its conflicts is the fixer's to merge, before the
	// version is raised and the tests are run: what it merges then gets its
	// changelog entry, and the batch is tested once, after it. A batch of which
	// every branch was set aside for its conflicts is given it too: only when
	// nothing is left for the fixer to merge, or none can start, is it held.
	settings := rules.LandingSettings()
	if b.mayFix() && report.conflicted() {
		why := fmt.Sprintf("%s set aside for conflicts", strings.Join(storyIDs(report.SetAside), ", "))
		if !b.fix(ctx, dir, rigDir, settings, report, why) {
			return
		}
	}
	if len(report.Merged) == 0 {
		report.Held = "no branch of the batch merged onto " + report.Target
		if report.Fixer != nil && report.Fixer.Started {
			report.Held += ", even after its fixer " + report.Fixer.Name + " had ended"
		}
		return
	}

	// A batch every branch of which was on the target already brings nothing
	// new: no version is raised for it and no What's new line written.
	if len(rules.VersionFiles) > 0 && !report.allOnTarget() {
		bump := VersionBump{Files: rules.VersionFiles, Before: report.Base, Branch: "HEAD", StoryID: report.Label()}
		for _, s := range report.Merged {
			// One story of the batch asking for the minor raises it for all.
			if s.path.Bump == domain.BumpMinor && !s.OnTarget {
				bump.Minor = true
			}
		}
		if len(rules.ChangelogFiles) > 0 {
			bump.Changelog = rules.ChangelogFiles
			for _, s := range report.Merged {
				if s.OnTarget {
					report.Notes = append(report.Notes, fmt.Sprintf("%s was on %s already, so its What's new line is not written again", s.ID, report.Target))
					continue
				}
				entry, err := n.changelogEntry(ctx, b.storyCloseOut(s, rigDir), nr)
				if err != nil {
					report.Held = fmt.Sprintf("the changelog entry of %s could not be read: %v", s.ID, err)
					return
				}
				if entry.Text != "" {
					bump.Entries = append(bump.Entries, entry)
				}
			}
		}
		bumped, err := n.Landing.BumpVersion(ctx, dir, bump)
		if err != nil {
			report.Held = fmt.Sprintf("the version of %s could not be raised: %v", report.Rig, err)
			return
		}
		report.Version = bumped.Version
		report.Notes = append(report.Notes, bumped.Tidied...)
	}

	gate := b.runGate(ctx, dir, rigDir, report, nr)
	// A red batch is given its one fixer, unless it had it already; once the
	// fixer has ended mw runs the tests again itself: its word is not the gate.
	if gate.Err == nil && !gate.Checked.NotRun && !gate.Checked.Passed && b.mayFix() && report.Fixer == nil {
		if !b.fix(ctx, dir, rigDir, settings, report, fmt.Sprintf("%s failed: `%s`", gateOnTheBatch, gate.Checked.Command)) {
			return
		}
		gate = b.runGate(ctx, dir, rigDir, report, nr)
	}
	after := ""
	if report.Fixer != nil && report.Fixer.Started {
		after = ", after its fixer " + report.Fixer.Name + " had ended, and no second fixer is started"
	}
	switch {
	case gate.Err != nil:
		report.Held = fmt.Sprintf("%s could not be run%s: %v", gateOnTheBatch, after, gate.Err)
		return
	case gate.Checked.NotRun:
		report.Held = fmt.Sprintf("%s could not be run%s: `%s` did not start", gateOnTheBatch, after, gate.Checked.Command)
		return
	case !gate.Checked.Passed:
		report.Held = fmt.Sprintf("%s failed%s: `%s`", gateOnTheBatch, after, gate.Checked.Command)
		if gate.Said != "" {
			report.Held += " (" + gate.Said + ")"
		}
		return
	}

	commit, err := n.Landing.RefHead(ctx, dir, "HEAD")
	if err != nil {
		report.Held = fmt.Sprintf("the batch's commit could not be read: %v", err)
		return
	}
	if err := n.pushRetrying(ctx, nr, dir, report.Target); err != nil {
		if Rejected(err) {
			report.Held = fmt.Sprintf("%s moved on at %s while the batch was made and tested, and mw forces nothing: %v", report.Target, n.remote(), firstLine(err.Error()))
		} else {
			report.Held = fmt.Sprintf("the push to %s failed: %v", report.Target, firstLine(err.Error()))
		}
		return
	}
	report.Commit, report.Landed = commit, true
}

// allOnTarget says every story the batch merged was on the target already.
func (r BatchReport) allOnTarget() bool {
	for _, s := range r.Merged {
		if !s.OnTarget {
			return false
		}
	}
	return true
}

// runGate is one run of the rig's tests on the batch, under the gate lock and
// the calm rule, counted with the others the batch made.
func (b BatchLand) runGate(ctx context.Context, dir, rigDir string, report *BatchReport, nr *NextReport) gated {
	n := b.Next
	began := n.now()
	gate := n.gate(ctx, b.batchCloseOut(report, rigDir), nr, dir, gateOnTheBatch)
	report.Tested, report.Gate = true, gate.Checked
	report.GateRuns++
	report.GateTook += n.now().Sub(began)
	if gate.Err == nil && !gate.Checked.NotRun && !gate.Checked.Passed && report.RedAt.IsZero() {
		report.RedAt = n.now()
	}
	return gate
}

// mergeAll merges the queue into the batch in order. A story whose branch is
// not at the origin, or does not merge, is set aside, and the merge of the next
// is made on the batch as it stood: a merge that conflicts is undone by the
// landing before it comes back.
func (b BatchLand) mergeAll(ctx context.Context, dir, rigDir string, union []string, report *BatchReport) {
	n := b.Next
	for _, s := range report.Queue {
		if _, err := n.Landing.RefHead(ctx, rigDir, s.Branch); err != nil {
			s.Why = fmt.Sprintf("its branch %s is not at the rig's origin", s.Branch)
			report.SetAside = append(report.SetAside, s)
			continue
		}
		if ahead, err := n.Landing.Ahead(ctx, rigDir, s.Branch, report.Base); err == nil {
			s.Commits, s.OnTarget = ahead, ahead == 0
		}
		// A branch whose commits sign the work as a machine's is refused here as
		// mw next refuses it: it is never merged, whatever else is in the batch.
		if why := b.signedWhy(ctx, rigDir, s.Branch, report.Base, "its branch "+s.Branch); why != "" {
			s.Why = why
			report.SetAside = append(report.SetAside, s)
			continue
		}
		var landed Landed
		var err error
		if len(union) > 0 {
			landed, err = n.Landing.MergeUnion(ctx, dir, s.Branch, union)
		} else {
			landed, err = n.Landing.Merge(ctx, dir, s.Branch)
		}
		if err != nil {
			if Conflicted(err) {
				s.Conflicts = ConflictLines(err.Error())
				s.Why = "it conflicts with the batch: " + strings.Join(s.Conflicts, "; ")
			} else {
				s.Why = "it could not be merged: " + firstLine(err.Error())
			}
			report.SetAside = append(report.SetAside, s)
			continue
		}
		s.Merge, s.FastForward, s.Unioned = landed.Commit, landed.FastForward, landed.Unioned
		report.Merged = append(report.Merged, s)
	}
}

// signedWhy reads the commits of branch beyond base and says, in a sentence that
// begins with whose they are, why they may not land: the first one that carries
// an AIAttribution, by its hash and the line, or that they could not be read —
// a close-out that cannot tell lands nothing. It is empty when every commit is
// clean.
func (b BatchLand) signedWhy(ctx context.Context, dir, branch, base, whose string) string {
	commits, err := b.Next.Landing.Commits(ctx, dir, branch, base)
	if err != nil {
		return fmt.Sprintf("the commit messages on %s could not be read: %v", branch, firstLine(err.Error()))
	}
	for _, commit := range commits {
		if line := AIAttribution(commit.Message); line != "" {
			return fmt.Sprintf("%s holds commit %s, signed as a machine's work, which this factory's commits never are: %s", whose, commit.Hash, line)
		}
	}
	return ""
}

// ConflictLines are git's own lines naming what a merge conflicted in
// ("CONFLICT (content): Merge conflict in x"), and what mw adds about a
// conflict the rig's union rules could not settle; the first line of what was
// said when git named none.
func ConflictLines(said string) []string {
	var lines []string
	for _, line := range strings.Split(said, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "CONFLICT"); i >= 0 {
			lines = append(lines, line[i:])
		}
	}
	if len(lines) == 0 {
		return []string{firstLine(said)}
	}
	return lines
}

// recordLanded is everything that follows the push, for each story of the
// batch and then once: none of it undoes the landing or stops the rest.
func (b BatchLand) recordLanded(ctx context.Context, rigDir string, report *BatchReport, nr *NextReport) {
	n := b.Next
	together := make([]string, len(report.Merged))
	for i, s := range report.Merged {
		together[i] = s.ID
	}
	var smokeApps []string
	// A story's mail is sent once the after-landing command has run or been
	// skipped, so that it can say first that the deploy did not happen.
	type mail struct {
		c    *closeOut
		sent *NextReport
	}
	var mails []mail
	for _, s := range report.Merged {
		c := b.storyCloseOut(s, rigDir)
		apps, notes := n.Smoke.Wants(ctx, report.Rig, rigDir, report.Base, s.Branch)
		nr.Notes = append(nr.Notes, notes...)
		smokeApps = appendNew(smokeApps, apps...)

		how := "its merge"
		if s.FastForward {
			how = "its fast-forward"
		}
		version := ""
		if report.Version != "" {
			version = ", version " + report.Version
		}
		outcome := fmt.Sprintf("landed on %s in %s (batch commit %s, %s %s%s), %d commits, the rig's tests on the batch: pass",
			report.Target, report.Label(), shortCommit(report.Commit), how, shortCommit(s.Merge), version, s.Commits)

		// Written first, as mw next writes it first: it is what tells a later
		// run that the story is landed.
		n.writeOrSay(ctx, c, nr, "the run state", fmt.Sprintf("%s could not be recorded as %s=%s", s.ID, RunState, RunLanded),
			func() error { return n.Tracker.SetStoryState(ctx, s.ID, RunState, RunLanded, outcome) })
		n.queueStamp(ctx, c, nr, Landed{Commit: s.Merge})

		line := LedgerLine{
			When: n.now(), StoryID: s.ID, Title: s.Title, Outcome: outcome, Path: s.path,
			// The session's fuel was charged by the line that queued it.
			FuelCharged: true,
			Notes:       append([]string{"landed by mw land on " + n.Host, report.Label() + ": " + strings.Join(together, ", ")}, report.fixerNotes(s.ID)...),
		}.String()
		if err := n.Vault.AppendToLedger(ctx, n.Seat, line); err != nil {
			nr.Notes = append(nr.Notes, fmt.Sprintf("the line for %s could not be appended to the %s seat's ledger: %v", s.ID, n.Seat, err))
		}
		// Committed as soon as it is written, before the worktree comes away,
		// the story is closed and the mail goes: a stop anywhere after leaves
		// the vault clean (mw-gq6.502).
		b.commitLedger(ctx, report)
		if err := n.Worktrees.Remove(ctx, rigDir, c.worktree, c.branch); err != nil {
			nr.Notes = append(nr.Notes, fmt.Sprintf("the worktree %s could not be taken away: %v", c.worktree, err))
		}
		closed := false
		notes, err := n.closeStory(ctx, s.ID, outcome)
		nr.Notes = append(nr.Notes, notes...)
		if err != nil {
			nr.Notes = append(nr.Notes, fmt.Sprintf("%s is landed but could not be closed: %v", s.ID, err))
		} else {
			closed = true
			report.Closed = append(report.Closed, s.ID)
		}

		mailed := NextReport{
			StoryID: s.ID, Host: n.Host, Title: s.Title, Target: report.Target, Commits: s.Commits, Pushes: 1,
			Landed: true, How: Landed{Commit: s.Merge, FastForward: s.FastForward, Version: report.Version, Unioned: s.Unioned},
			Ledger: line, Closed: closed, NotClosed: "see the batch's report",
			Notes: append([]string{fmt.Sprintf("landed by mw land in %s (%s), whose commit %s is on %s", report.Label(), strings.Join(together, ", "),
				shortCommit(report.Commit), report.Target)}, report.fixerNotes(s.ID)...),
		}
		mails = append(mails, mail{c: c, sent: &mailed})
	}

	// The ledger lines were committed as each was written, so that the vault is
	// not left dirty while the checkout, the after-landing command and the
	// smoke run for many minutes (mw-gq6.493, mw-gq6.502). The batch's records
	// are written and committed after.

	// Once for the batch: the rig's checkout, the after-landing command and the
	// smoke, as the newest story's.
	last := b.storyCloseOut(report.Merged[len(report.Merged)-1], rigDir)
	last.smokeApps = smokeApps
	once := NextReport{StoryID: report.Label(), Host: n.Host, Target: report.Target, How: Landed{Commit: report.Commit}}
	n.advanceRig(ctx, last, &once)
	report.Checkout = once.Rig
	n.afterLanding(ctx, last, &once)
	if record := runFilePath(last.id, last.afterLog); last.afterLog != "" && record != "" {
		report.Records = append(report.Records, record)
	}
	n.smoke(ctx, last, &once)
	nr.Notes = append(nr.Notes, once.Notes...)

	for _, m := range mails {
		m.sent.AfterLandingSkipped = once.AfterLandingSkipped
		n.mailTheMayor(ctx, m.c, m.sent, MailLanded, n.closingProposals(ctx, m.c))
		report.Mailed = append(report.Mailed, MailLanded+": "+m.c.id)
	}
}

// appendNew adds to list what it does not hold yet.
func appendNew(list []string, more ...string) []string {
	for _, item := range more {
		found := false
		for _, had := range list {
			if had == item {
				found = true
				break
			}
		}
		if !found {
			list = append(list, item)
		}
	}
	return list
}

// storyCloseOut is a story of the batch as the close-out's helpers take one.
func (b BatchLand) storyCloseOut(s BatchStory, rigDir string) *closeOut {
	return &closeOut{
		id: s.ID, detail: s.detail, path: s.path, rigDir: rigDir,
		worktree: WorktreeDir(rigDir, s.ID), branch: StoryBranch(s.ID), target: s.path.Branch,
		began: b.Next.now(),
	}
}

// batchCloseOut is the batch itself as the gate takes it: named for the
// batch, in the rig, on its target.
func (b BatchLand) batchCloseOut(report *BatchReport, rigDir string) *closeOut {
	return &closeOut{
		id: report.Label(), path: domain.Path{Rig: report.Rig, Branch: report.Target}, rigDir: rigDir,
		target: report.Target, began: b.Next.now(),
	}
}

// keepRecords writes the batch's three records. One that cannot be written is
// a note: the batch is landed, or held, as it is.
func (b BatchLand) keepRecords(ctx context.Context, report *BatchReport) {
	gateLog := "not run: " + report.Held + "\n"
	if report.Tested {
		gateLog = fmt.Sprintf("$ %s\n%s", report.Gate.Command, report.Gate.Output)
	}
	if f := report.Fixer; f != nil && f.Tested && report.GateRuns > 1 {
		gateLog = fmt.Sprintf("$ %s\n%s\n# the fixer %s was given the batch; once it had ended mw ran the tests again:\n%s",
			f.Gate.Command, f.Gate.Output, f.Name, gateLog)
	}
	outcome := fmt.Sprintf("landed %s on %s, %d stories: %s\n", report.Commit, report.Target, len(report.Merged), strings.Join(storyIDs(report.Merged), ", "))
	if !report.Landed {
		outcome = fmt.Sprintf("held: %s\nnothing was pushed; %s stays at %s, and every story in the batch stays queued\n", report.Held, report.Target, report.Base)
	}
	if len(report.SetAside) > 0 {
		outcome += fmt.Sprintf("set aside, still queued: %s\n", strings.Join(storyIDs(report.SetAside), ", "))
	}
	files := []struct{ name, contents string }{
		{BatchOrderFile, report.mergeOrder()},
		{BatchGateLogFile, gateLog},
		{BatchOutcomeFile, outcome},
	}
	if report.Fixer != nil {
		files = append(files, struct{ name, contents string }{BatchFixerFile, report.fixerRecord()})
	}
	for _, file := range files {
		kept, err := b.Records.PutBatchFile(ctx, report.Rig, report.Number, file.name, file.contents)
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the %s of %s could not be kept: %v", file.name, report.Label(), err))
			continue
		}
		report.Records = append(report.Records, kept)
	}
}

// keepMetric appends the batch's line to metrics/factory.jsonl. One that cannot
// be written is a note: the batch is landed, or held, as it is.
func (b BatchLand) keepMetric(ctx context.Context, report *BatchReport) {
	if b.Metrics == nil {
		return
	}
	if err := b.Metrics.AppendBatch(ctx, report.Metric(b.Next.now())); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the metrics line of %s could not be kept: %v", report.Label(), err))
	}
}

func storyIDs(stories []BatchStory) []string {
	ids := make([]string, len(stories))
	for i, s := range stories {
		ids[i] = s.ID
	}
	return ids
}

// mergeOrder is the merge-order record: the batch's base, then each story in
// the order it was taken and what became of it.
func (r BatchReport) mergeOrder() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s onto %s at %s, on %s\n", r.Label(), r.Target, r.Base, r.Host)
	for i, s := range r.Queue {
		fmt.Fprintf(&b, "%d. %s %s", i+1, s.ID, s.Branch)
		if !s.QueuedAt.IsZero() {
			fmt.Fprintf(&b, " queued %s", s.QueuedAt.UTC().Format(time.RFC3339))
		}
		if merged, ok := r.merged(s.ID); ok {
			how := "merged at"
			switch {
			case merged.ByFixer:
				how = "set aside, then merged by the fixer, at"
			case merged.FastForward:
				how = "fast-forwarded to"
			}
			fmt.Fprintf(&b, " %s %s", how, merged.Merge)
			if len(merged.Unioned) > 0 {
				fmt.Fprintf(&b, " (trim-merged %s)", strings.Join(merged.Unioned, ", "))
			}
			b.WriteString("\n")
			continue
		}
		for _, aside := range r.SetAside {
			if aside.ID == s.ID {
				fmt.Fprintf(&b, " set aside: %s", aside.Why)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (r BatchReport) merged(id string) (BatchStory, bool) {
	for _, s := range r.Merged {
		if s.ID == id {
			return s, true
		}
	}
	return BatchStory{}, false
}

// mailTheMayor sends the one mail about a batch that did not land whole: Held,
// naming every story in it, or Set aside, naming those that were. A mail that
// cannot be sent is a note.
func (b BatchLand) mailTheMayor(ctx context.Context, report *BatchReport, nr *NextReport) {
	n := b.Next
	notCommitted := report.notCommitted()
	if n.Mailbox == nil || (report.Landed && len(report.SetAside) == 0 && len(notCommitted) == 0) {
		return
	}
	var body strings.Builder
	for _, line := range notCommitted {
		body.WriteString(line + "\n")
	}
	verdict := MailHeld
	if report.Landed && len(report.SetAside) == 0 {
		verdict = MailNotCommitted
		fmt.Fprintf(&body, "mw land on %s landed %s on %s at %s, but could not commit its records in the vault.\n\n", n.Host, report.Label(), report.Target, shortCommit(report.Commit))
	} else if report.Landed {
		verdict = MailSetAside
		fmt.Fprintf(&body, "mw land on %s landed %s on %s at %s, but set these aside; each stays queued (run=queued, open and claimed) for a later batch:\n\n",
			n.Host, report.Label(), report.Target, shortCommit(report.Commit))
	} else {
		fmt.Fprintf(&body, "mw land on %s held %s: %s.\nNothing was pushed: %s stays at %s on %s. Every story below stays queued (run=queued, open and claimed).\n\n",
			n.Host, report.Label(), firstLine(report.Held), report.Target, shortCommit(report.Base), n.remote())
	}
	listed := report.SetAside
	if !report.Landed {
		listed = report.Queue
	}
	for _, s := range listed {
		why := s.Why
		if aside, ok := report.setAside(s.ID); ok {
			why = "set aside: " + aside.Why
		} else if _, ok := report.merged(s.ID); ok {
			why = "merged into the batch, which was held"
		}
		fmt.Fprintf(&body, "- %s %s: %s\n", s.ID, strings.Join(strings.Fields(s.Title), " "), why)
	}
	if f := report.Fixer; f != nil && !report.Landed {
		fmt.Fprintf(&body, "\n%s", report.fixerMail())
	}
	if !report.Landed && report.Tested && !report.Gate.Passed {
		fmt.Fprintf(&body, "\n%s, `%s`, ended:\n\n```\n%s\n```\n", gateOnTheBatch, report.Gate.Command, report.Gate.Tail(CheckLines))
	}
	fmt.Fprintf(&body, "\nThe batch's records: %s/%s/%d/\n", BatchDir, report.Rig, report.Number)
	subject := fmt.Sprintf("%s: %s, %s", verdict, report.Label(), countStories(len(listed)))
	if verdict == MailNotCommitted {
		subject = fmt.Sprintf("%s: %s", verdict, report.Label())
	}
	mail := NewMessage{From: SeatIdentity(MwSeat, n.Host), To: MayorMailbox, Subject: subject, Body: body.String()}
	err := writeRetrying(ctx, n, nr, "the mail to the Mayor", func() error {
		_, err := n.Mailbox.Send(ctx, mail)
		return err
	})
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the mail to %s about %s could not be sent: %v", MayorMailbox, report.Label(), err))
		return
	}
	report.Mailed = append(report.Mailed, subject)
}

func (r BatchReport) setAside(id string) (BatchStory, bool) {
	for _, s := range r.SetAside {
		if s.ID == id {
			return s, true
		}
	}
	return BatchStory{}, false
}

func countStories(count int) string {
	if count == 1 {
		return "1 story"
	}
	return strconv.Itoa(count) + " stories"
}

// commitLedger commits the seat's ledger on its own, right after a landed
// story's line is appended to it: it is the one tracked file the batch has
// written by the time its long steps begin. A commit that cannot be made is a
// note, as it is for the commit that ends the batch.
func (b BatchLand) commitLedger(ctx context.Context, report *BatchReport) {
	b.commitPaths(ctx, report, []string{path.Join(SeatsDir, b.Next.Seat, LedgerFileName)})
}

// commit records in the vault exactly what the batch wrote there: the seat's
// ledger and the batch's records, by path. A commit that cannot be made is a
// note, as it is for a close-out. The ledger was committed already when the
// batch landed; naming it again is no more than a safeguard, for a path that
// has not changed is left out.
func (b BatchLand) commit(ctx context.Context, report *BatchReport) {
	paths := append([]string(nil), report.Records...)
	if report.Landed {
		paths = append(paths, path.Join(SeatsDir, b.Next.Seat, LedgerFileName))
	}
	b.commitPaths(ctx, report, paths)
}

func (b BatchLand) commitPaths(ctx context.Context, report *BatchReport, paths []string) {
	n := b.Next
	if n.Files == nil || len(paths) == 0 {
		return
	}
	verb := "Land"
	if !report.Landed {
		verb = "Hold"
	}
	committed, err := commitWritten(ctx, n.Files, fmt.Sprintf("%s %s: %s", verb, report.Label(), strings.Join(storyIDs(report.Queue), ", ")), paths)
	if err != nil {
		report.Notes = append(report.Notes, NotCommittedLine(paths, err))
		return
	}
	report.Committed = append(report.Committed, committed...)
}

// notCommitted is the lines of the batch's notes that say files were written in
// the vault and not committed (NotCommittedLine).
func (r BatchReport) notCommitted() []string {
	var lines []string
	for _, note := range r.Notes {
		if strings.HasPrefix(note, notCommittedPrefix) {
			lines = append(lines, note)
		}
	}
	return lines
}

// String is the batch as a person reads it.
func (r BatchReport) String() string {
	var b strings.Builder
	mode := string(r.Mode)
	if mode == "" {
		mode = string(domain.LandingEach)
	}
	fmt.Fprintf(&b, "land on %s: %s (landing = %q)\n", r.Host, r.Rig, mode)
	if len(r.Queue) == 0 {
		b.WriteString("  queue   nothing is queued for batch landing\n")
		writeNotes(&b, r.Notes)
		return b.String()
	}
	if r.DryRun {
		fmt.Fprintf(&b, "  dry run would merge onto %s at %s, oldest queued first:\n", r.Target, shortCommit(r.Base))
		for i, s := range r.Queue {
			fmt.Fprintf(&b, "    %d. %s  %s", i+1, s.ID, s.Branch)
			if s.Why != "" {
				fmt.Fprintf(&b, "  WOULD BE SET ASIDE: %s", s.Why)
			} else {
				fmt.Fprintf(&b, "  %d commits", s.Commits)
			}
			if !s.QueuedAt.IsZero() {
				fmt.Fprintf(&b, "  queued %s", s.QueuedAt.UTC().Format(time.RFC3339))
			}
			fmt.Fprintf(&b, "  %s\n", strings.Join(strings.Fields(s.Title), " "))
		}
		fmt.Fprintf(&b, "          then run the rig's tests once on the batch, and push %s only if they pass\n", r.Target)
		b.WriteString("          nothing was merged, tested, pushed, recorded or mailed\n")
		writeNotes(&b, r.Notes)
		return b.String()
	}
	fmt.Fprintf(&b, "  batch   %s onto %s at %s\n", r.Label(), r.Target, shortCommit(r.Base))
	for i, s := range r.Queue {
		if merged, ok := r.merged(s.ID); ok {
			fmt.Fprintf(&b, "    %d. %s  merged at %s\n", i+1, s.ID, shortCommit(merged.Merge))
		} else if aside, ok := r.setAside(s.ID); ok {
			fmt.Fprintf(&b, "    %d. %s  SET ASIDE %s\n", i+1, s.ID, aside.Why)
		}
	}
	if r.Version != "" {
		fmt.Fprintf(&b, "  version %s, once for the batch\n", r.Version)
	}
	if f := r.Fixer; f != nil {
		if f.Started {
			fmt.Fprintf(&b, "  fixer   %s, for %s: ended %s", f.Name, f.Why, f.Ended.State)
			if len(f.Merged) > 0 {
				fmt.Fprintf(&b, ", merged %s", strings.Join(f.Merged, ", "))
			}
			b.WriteString("\n")
		} else {
			fmt.Fprintf(&b, "  fixer   %s %s\n", f.Name, f.Trouble)
		}
	}
	if r.Tested {
		verdict := "pass"
		if !r.Gate.Passed {
			verdict = "FAIL"
		}
		times := "once"
		if r.GateRuns > 1 {
			times = fmt.Sprintf("%d times, the last after the fixer", r.GateRuns)
		}
		fmt.Fprintf(&b, "  tested  %s, %s: %s\n", gateOnTheBatch, times, verdict)
	}
	if r.Landed {
		fmt.Fprintf(&b, "  landed  %s at %s, one push, a fast-forward: %s\n", r.Target, shortCommit(r.Commit), strings.Join(storyIDs(r.Merged), ", "))
		if len(r.Closed) > 0 {
			fmt.Fprintf(&b, "  closed  %s\n", strings.Join(r.Closed, ", "))
		}
	} else {
		fmt.Fprintf(&b, "  HELD    %s\n", firstLine(r.Held))
		fmt.Fprintf(&b, "          nothing was pushed; every story in the batch stays queued\n")
	}
	switch {
	case r.Checkout.Moved:
		fmt.Fprintf(&b, "  rig     %s fast-forwarded to %s on %s\n", r.RigDir, shortCommit(r.Commit), r.Target)
	case r.Checkout.Left != "":
		fmt.Fprintf(&b, "  rig     %s left as it was, not brought up to %s: %s\n", r.RigDir, r.Target, r.Checkout.Left)
	case r.Checkout.Level && r.Checkout.Held == "":
		fmt.Fprintf(&b, "  rig     %s was already at %s on %s\n", r.RigDir, shortCommit(r.Commit), r.Target)
	case r.Checkout.Held != "":
		fmt.Fprintf(&b, "  rig     %s is not a place to build %s from: %s\n", r.RigDir, r.Target, r.Checkout.Held)
	}
	for _, kept := range r.Records {
		fmt.Fprintf(&b, "  record  %s\n", kept)
	}
	for _, mailed := range r.Mailed {
		fmt.Fprintf(&b, "  mailed  %s\n", mailed)
	}
	writeNotes(&b, r.Notes)
	return b.String()
}

func writeNotes(b *strings.Builder, notes []string) {
	for _, note := range notes {
		fmt.Fprintf(b, "  note    %s\n", note)
	}
}

// BatchLandJobName is the name of the follower's job that lands the queues of
// the rigs that land in batches: its events' actor is batch-land@<host>.
const BatchLandJobName = "batch-land"

// BatchDue says whether a queue of queued stories, the oldest queued at oldest,
// is to be landed at now: BatchMax queued is due at once, and BatchMin queued
// is due once the oldest has waited BatchEvery. An oldest with no time was
// queued by an mw that recorded none, before any that has one, so it has waited
// long enough. An empty queue is never due.
func BatchDue(s domain.LandingSettings, queued int, oldest, now time.Time) bool {
	switch {
	case queued <= 0:
		return false
	case queued >= s.BatchMax:
		return true
	case queued < s.BatchMin:
		return false
	}
	return oldest.IsZero() || now.Sub(oldest) >= s.BatchEvery
}

// BatchQueues is the port the batch-land job reads the queues through: the
// stories in hand, among which are those run=queued.
type BatchQueues interface {
	WorkInHand(ctx context.Context) (WorkInHand, error)
}

// BatchLandings is the follower's job that runs mw land on every rig of this
// host whose queue is due, on the home only (plans/0117, Land Rush 4).
//
// A rig that lands in batches is due by BatchDue, under its own keys. A rig
// whose file says landing = each is due as soon as a story is queued on it: it
// was flipped back, and what it had queued is landed one story at a time, the
// oldest first, until none remains. The passes run the due rigs one after the
// other, so two landings never run together here, and the follower keeps the
// job in flight at most once; mw land takes the rig's merge slot besides.
//
// A rig whose landing failed (a held batch, which the Mayor is mailed, or a
// story landed that left the queue as it was) is not tried again until
// BatchEvery has passed, so a red batch is not run again every pass, nor while
// the harness holds this host for a limit of the factory's: a batch held for
// its fixer's harness stop waits for the stop's time (mw-gq6.472).
type BatchLandings struct {
	// Rigs are the rigs checked out on this host, by name.
	Rigs map[string]string
	// Rules is where each rig's landing keys are read from.
	Rules EpicRules
	// Tracker is where the queued stories are read from.
	Tracker BatchQueues
	// Home says whether this host is the home: only the home lands.
	Home func(ctx context.Context) (bool, error)
	// Land does mw land <rig>, or with one mw land <rig> --one.
	Land func(ctx context.Context, rig string, one bool) error
	// Pending, when set, names the rigs whose landing runs on apart from the
	// follower, or ended with no follower told how (DetachedLand.Pending):
	// each pass waits for those first, due or not, so that a landing a
	// follower before this one began is told once.
	Pending func(ctx context.Context) ([]string, error)
	// Notes and Host are where this host's hold after a harness stop is read
	// (HarnessHoldKey); nil Notes reads none.
	Notes SweepNotes
	Host  string
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	mu     sync.Mutex
	failed map[string]time.Time // rig -> when its landing last failed
}

func (b *BatchLandings) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

// Job is the follower's job: no event springs it; every pass asks whether a
// queue is due.
func (b *BatchLandings) Job() SpringJob {
	return SpringJob{
		Name: BatchLandJobName,
		Probe: func(ctx context.Context) (string, bool) {
			due, _ := b.due(ctx)
			if len(due) == 0 {
				return "", false
			}
			parts := make([]string, len(due))
			for i, d := range due {
				parts[i] = d.String()
			}
			return "landing of " + strings.Join(parts, ", "), true
		},
		Run: b.run,
	}
}

// batchDue is a rig whose queue is to be landed now.
type batchDue struct {
	rig    string
	each   bool
	queued int
}

func (d batchDue) String() string {
	if d.each {
		return fmt.Sprintf("%s (%d queued, one at a time)", d.rig, d.queued)
	}
	return fmt.Sprintf("%s (%d queued)", d.rig, d.queued)
}

// batchQueue is what a rig has queued: how many, and when the oldest was.
type batchQueue struct {
	queued int
	oldest time.Time
}

// queues reads the queue of every rig of this host that has one.
func (b *BatchLandings) queues(ctx context.Context) (map[string]batchQueue, error) {
	work, err := b.Tracker.WorkInHand(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the queued stories: %w", err)
	}
	queues := map[string]batchQueue{}
	for _, detail := range work.Running {
		if !QueuedForBatch(detail) {
			continue
		}
		path, err := detail.Path()
		if err != nil {
			continue
		}
		if _, here := b.Rigs[path.Rig]; !here {
			continue
		}
		q := queues[path.Rig]
		if q.queued == 0 || detail.QueuedAt.Before(q.oldest) {
			q.oldest = detail.QueuedAt
		}
		q.queued++
		queues[path.Rig] = q
	}
	return queues, nil
}

// due is the rigs whose queue is to be landed now, in name order. A rig whose
// rules cannot be read is left out and the error comes back beside the rest.
func (b *BatchLandings) due(ctx context.Context) ([]batchDue, error) {
	// Home first: it reads one file in the vault, and the queues are read
	// from the tracker.
	if at, err := b.Home(ctx); err != nil || !at {
		return nil, err
	}
	queues, err := b.queues(ctx)
	if err != nil || len(queues) == 0 {
		return nil, err
	}
	names := make([]string, 0, len(queues))
	for name := range queues {
		names = append(names, name)
	}
	sort.Strings(names)

	now := b.now()
	hold, held := readHarnessHold(ctx, b.Notes, b.Host)
	held = held && hold.Until.After(now)
	b.mu.Lock()
	defer b.mu.Unlock()
	var due []batchDue
	var errs []error
	for _, name := range names {
		rules, err := b.requirements(ctx, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("reading the rules of %s: %w", name, err))
			continue
		}
		settings := rules.LandingSettings()
		if failed, was := b.failed[name]; was && (held || now.Sub(failed) < settings.BatchEvery) {
			continue
		}
		q := queues[name]
		if settings.Mode == domain.LandingEach {
			due = append(due, batchDue{rig: name, each: true, queued: q.queued})
		} else if BatchDue(settings, q.queued, q.oldest, now) {
			due = append(due, batchDue{rig: name, queued: q.queued})
		}
	}
	return due, errors.Join(errs...)
}

// requirements reads a rig's file as a landing does: a key this mw does not
// know does not stop it.
func (b *BatchLandings) requirements(ctx context.Context, rig string) (domain.EpicRequirements, error) {
	if lenient, ok := b.Rules.(LandingRules); ok {
		rules, _, err := lenient.LandingRequirements(ctx, rig)
		return rules, err
	}
	return b.Rules.EpicRequirements(ctx, rig)
}

// run lands each due rig, one at a time. A rig whose landing failed does not
// stop the next; the pass then fails, naming each.
func (b *BatchLandings) run(ctx context.Context) error {
	due, err := b.due(ctx)
	due, perr := b.pending(ctx, due)
	if len(due) == 0 {
		return errors.Join(err, perr)
	}
	errs := []error{err, perr}
	for _, d := range due {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		err := b.landRig(ctx, d)
		if errors.Is(err, ErrLeftRunning) {
			// The landing goes on apart: the follower that comes back tells it.
			errs = append(errs, fmt.Errorf("landing %s: %w", d.rig, err))
			break
		}
		if err != nil {
			b.mu.Lock()
			if b.failed == nil {
				b.failed = map[string]time.Time{}
			}
			b.failed[d.rig] = b.now()
			b.mu.Unlock()
			errs = append(errs, fmt.Errorf("landing %s: %w", d.rig, err))
		}
	}
	return errors.Join(errs...)
}

// pending puts first in due each rig of this host whose landing is pending
// (Pending) and is not due already, on the home only: a landing begun by a
// follower before this one, waited for whether or not its queue is due now.
func (b *BatchLandings) pending(ctx context.Context, due []batchDue) ([]batchDue, error) {
	if b.Pending == nil {
		return due, nil
	}
	names, err := b.Pending(ctx)
	if err != nil {
		return due, fmt.Errorf("reading the landings in flight: %w", err)
	}
	var adopt []batchDue
	for _, name := range names {
		if _, here := b.Rigs[name]; !here || slices.ContainsFunc(due, func(d batchDue) bool { return d.rig == name }) {
			continue
		}
		adopt = append(adopt, batchDue{rig: name})
	}
	if len(adopt) == 0 {
		return due, nil
	}
	if at, err := b.Home(ctx); err != nil || !at {
		return due, err
	}
	return append(adopt, due...), nil
}

// landRig lands the queue of one rig: all of it at once, or on a rig that lands
// each story by itself, one story at a time until none is left. A landing that
// leaves the queue as long as it was stops the drain, so that it cannot go on
// for ever.
func (b *BatchLandings) landRig(ctx context.Context, d batchDue) error {
	if !d.each {
		return b.Land(ctx, d.rig, false)
	}
	for left := d.queued; left > 0; {
		if err := b.Land(ctx, d.rig, true); err != nil {
			return err
		}
		queues, err := b.queues(ctx)
		if err != nil {
			return err
		}
		remaining := queues[d.rig].queued
		if remaining >= left {
			return fmt.Errorf("a story was landed by itself and %d are still queued", remaining)
		}
		left = remaining
	}
	return nil
}

// LandProcesses is the port the follower's landings are begun and waited for
// through. Each runs in a process apart from the follower: a follower that
// stops, as it does after every landing of the factory rig, leaves it running,
// and the one that comes back waits for it (mw-gq6.470). The real adapter is
// infrastructure/apartproc; the fake is apptest.FakeLandProcesses.
type LandProcesses interface {
	// Begin starts mw land <rig>, or with one mw land <rig> --one, unless a
	// landing of rig begun earlier, by this follower or another, is still
	// running or ended with no follower told how: that one is the landing.
	Begin(ctx context.Context, rig string, one bool) error
	// Wait returns when the landing of rig ends: nil if it landed, an error if
	// it failed or ended without saying. Once told, it is no longer pending. A
	// ctx that ends stops the wait, not the landing.
	Wait(ctx context.Context, rig string) error
	// Pending names the rigs whose landing is running or ended with no
	// follower told how, in name order.
	Pending(ctx context.Context) ([]string, error)
}

// DetachedLand is BatchLandings' Land and Pending for landings run apart from
// the follower: a landing a follower before this one began is found and
// waited for, not begun again beside it.
type DetachedLand struct {
	Processes LandProcesses
}

// Land begins the landing of rig, or finds the one running, and waits for it.
// A wait the follower stopping ends is ErrLeftRunning: the landing goes on.
func (d DetachedLand) Land(ctx context.Context, rig string, one bool) error {
	if err := d.Processes.Begin(ctx, rig, one); err != nil {
		return LeftRunning(ctx, fmt.Errorf("beginning mw land %s: %w", rig, err))
	}
	return LeftRunning(ctx, d.Processes.Wait(ctx, rig))
}

// Pending is the Processes' Pending.
func (d DetachedLand) Pending(ctx context.Context) ([]string, error) {
	return d.Processes.Pending(ctx)
}

// BatchFixerName is the name the fixer of a batch is run under, and its run's
// directory under runs/: batch-<rig>-<n>.
func BatchFixerName(rig string, number int) string {
	return fmt.Sprintf("batch-%s-%d", rig, number)
}
