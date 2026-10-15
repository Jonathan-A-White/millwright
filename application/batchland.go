package application

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
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
)

// The subjects of mw land's mail to the Mayor about a batch that did not land
// whole: Held when nothing of it landed, Set aside when the rest of it did.
// Each story that landed is mailed Landed, as mw next mails one.
const (
	MailHeld     = "Held"
	MailSetAside = "Set aside"
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

// BatchLand is mw land <rig> (plans/0117, Land Rush 3): it lands the stories
// mw next queued on a rig that lands in batches, several under one gate run.
//
// In a landing worktree detached at the target branch as the origin has it, it
// merges each queued story's branch as the origin has it (mw/<id>), oldest
// queued first, by the rig's union rules; a branch that does not merge is set
// aside with git's conflict list. It raises the rig's version once for the
// batch, with one changelog entry per story; it runs the rig's tests once on
// the batch, under the host's gate lock and the calm rule; and only when that
// run passes does it push the target branch, which is a fast-forward of what
// the origin had, never forced. Each story merged is then closed run=landed,
// naming the batch's commit and its own merge commit, with a chain stamp and a
// ledger line naming the batch; the rig's checkout is brought up, and the
// after-landing command and the grist smoke run once. A set-aside story stays
// queued, and the Mayor is mailed its name and its conflict.
//
// A red gate, a batch of which nothing merged, and a push that fails push
// nothing: the batch is held, every story in it stays queued, and the Mayor is
// mailed each story and why. The fixer that mends a held batch is Land Rush 5's.
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
	DryRun  bool
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
	Gate    Checked

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

	queue, err := b.queued(ctx, rig, &report)
	if err != nil {
		return report, err
	}
	report.Queue = queue
	if len(queue) == 0 {
		n.print(report.String())
		return report, nil
	}
	report.Target = queue[0].path.Branch

	// What the close-out's helpers note on the way, gathered into the report.
	var nr NextReport
	gather := func() {
		report.Notes = append(report.Notes, nr.Notes...)
		nr.Notes = nil
	}
	if err := n.fetchRetrying(ctx, &nr, rigDir); err != nil {
		gather()
		return report, fmt.Errorf("landing %s: the rig could not be brought up to date with %s: %w", rig, n.remote(), err)
	}
	base := StartPoint(n.remote(), report.Target)
	if report.Base, err = n.Landing.RefHead(ctx, rigDir, base); err != nil {
		return report, fmt.Errorf("landing %s: %s could not be read: %w", rig, base, err)
	}
	for i := range report.Queue {
		s := &report.Queue[i]
		s.Branch = StartPoint(n.remote(), StoryBranch(s.ID))
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
	if report.Number, err = b.Records.NewBatch(ctx, rig); err != nil {
		release()
		gather()
		return report, fmt.Errorf("landing %s: the batch's records could not be begun: %w", rig, err)
	}

	dir, err := n.Landing.OpenLanding(ctx, rigDir, base)
	if err != nil {
		report.Held = fmt.Sprintf("a landing of %s at %s could not be opened: %v", rig, base, err)
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
	b.mailTheMayor(ctx, &report, &nr)
	b.commit(ctx, &report)
	gather()
	n.print(report.String())
	if report.Held != "" {
		return report, fmt.Errorf("%s was held: %s", report.Label(), firstLine(report.Held))
	}
	return report, nil
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
	if len(report.Merged) == 0 {
		report.Held = "no branch of the batch merged onto " + report.Target
		return
	}

	if len(rules.VersionFiles) > 0 {
		bump := VersionBump{Files: rules.VersionFiles, Before: report.Base, Branch: "HEAD", StoryID: report.Label()}
		if len(rules.ChangelogFiles) > 0 {
			bump.Changelog = rules.ChangelogFiles
			for _, s := range report.Merged {
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
	}

	gate := n.gate(ctx, b.batchCloseOut(report, rigDir), nr, dir, gateOnTheBatch)
	report.Tested, report.Gate = true, gate.Checked
	switch {
	case gate.Err != nil:
		report.Held = fmt.Sprintf("%s could not be run: %v", gateOnTheBatch, gate.Err)
		return
	case gate.Checked.NotRun:
		report.Held = fmt.Sprintf("%s could not be run: `%s` did not start", gateOnTheBatch, gate.Checked.Command)
		return
	case !gate.Checked.Passed:
		report.Held = fmt.Sprintf("%s failed: `%s`", gateOnTheBatch, gate.Checked.Command)
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
			s.Commits = ahead
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
			Notes:       []string{"landed by mw land on " + n.Host, report.Label() + ": " + strings.Join(together, ", ")},
		}.String()
		if err := n.Vault.AppendToLedger(ctx, n.Seat, line); err != nil {
			nr.Notes = append(nr.Notes, fmt.Sprintf("the line for %s could not be appended to the %s seat's ledger: %v", s.ID, n.Seat, err))
		}
		if err := n.Worktrees.Remove(ctx, rigDir, c.worktree, c.branch); err != nil {
			nr.Notes = append(nr.Notes, fmt.Sprintf("the worktree %s could not be taken away: %v", c.worktree, err))
		}
		closed := false
		if err := n.closeStory(ctx, s.ID, outcome); err != nil {
			nr.Notes = append(nr.Notes, fmt.Sprintf("%s is landed but could not be closed: %v", s.ID, err))
		} else {
			closed = true
			report.Closed = append(report.Closed, s.ID)
		}

		mailed := NextReport{
			StoryID: s.ID, Host: n.Host, Title: s.Title, Target: report.Target, Commits: s.Commits, Pushes: 1,
			Landed: true, How: Landed{Commit: s.Merge, FastForward: s.FastForward, Version: report.Version, Unioned: s.Unioned},
			Ledger: line, Closed: closed, NotClosed: "see the batch's report",
			Notes: []string{fmt.Sprintf("landed by mw land in %s (%s), whose commit %s is on %s", report.Label(), strings.Join(together, ", "),
				shortCommit(report.Commit), report.Target)},
		}
		n.mailTheMayor(ctx, c, &mailed, MailLanded, n.closingProposals(ctx, c))
		report.Mailed = append(report.Mailed, MailLanded+": "+s.ID)
	}

	// Once for the batch: the rig's checkout, the after-landing command and the
	// smoke, as the newest story's.
	last := b.storyCloseOut(report.Merged[len(report.Merged)-1], rigDir)
	last.smokeApps = smokeApps
	once := NextReport{StoryID: report.Label(), Host: n.Host, Target: report.Target, How: Landed{Commit: report.Commit}}
	n.advanceRig(ctx, last, &once)
	report.Checkout = once.Rig
	n.afterLanding(ctx, last, &once)
	n.smoke(ctx, last, &once)
	nr.Notes = append(nr.Notes, once.Notes...)
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
	outcome := fmt.Sprintf("landed %s on %s, %d stories: %s\n", report.Commit, report.Target, len(report.Merged), strings.Join(storyIDs(report.Merged), ", "))
	if !report.Landed {
		outcome = fmt.Sprintf("held: %s\nnothing was pushed; %s stays at %s, and every story in the batch stays queued\n", report.Held, report.Target, report.Base)
	}
	if len(report.SetAside) > 0 {
		outcome += fmt.Sprintf("set aside, still queued: %s\n", strings.Join(storyIDs(report.SetAside), ", "))
	}
	for _, file := range []struct{ name, contents string }{
		{BatchOrderFile, report.mergeOrder()},
		{BatchGateLogFile, gateLog},
		{BatchOutcomeFile, outcome},
	} {
		kept, err := b.Records.PutBatchFile(ctx, report.Rig, report.Number, file.name, file.contents)
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the %s of %s could not be kept: %v", file.name, report.Label(), err))
			continue
		}
		report.Records = append(report.Records, kept)
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
			if merged.FastForward {
				how = "fast-forwarded to"
			}
			fmt.Fprintf(&b, " %s %s", how, merged.Merge)
			if len(merged.Unioned) > 0 {
				fmt.Fprintf(&b, " (union-merged %s)", strings.Join(merged.Unioned, ", "))
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
	if n.Mailbox == nil || (report.Landed && len(report.SetAside) == 0) {
		return
	}
	var body strings.Builder
	verdict := MailHeld
	if report.Landed {
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
	if !report.Landed && report.Tested && !report.Gate.Passed {
		fmt.Fprintf(&body, "\n%s, `%s`, ended:\n\n```\n%s\n```\n", gateOnTheBatch, report.Gate.Command, report.Gate.Tail(CheckLines))
	}
	fmt.Fprintf(&body, "\nThe batch's records: %s/%s/%d/\n", BatchDir, report.Rig, report.Number)
	subject := fmt.Sprintf("%s: %s, %s", verdict, report.Label(), countStories(len(listed)))
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

// commit records in the vault exactly what the batch wrote there: the seat's
// ledger and the batch's records, by path. A commit that cannot be made is a
// note, as it is for a close-out.
func (b BatchLand) commit(ctx context.Context, report *BatchReport) {
	n := b.Next
	if n.Files == nil {
		return
	}
	paths := append([]string(nil), report.Records...)
	if report.Landed {
		paths = append(paths, path.Join(SeatsDir, n.Seat, LedgerFileName))
	}
	if len(paths) == 0 {
		return
	}
	verb := "Land"
	if !report.Landed {
		verb = "Hold"
	}
	committed, err := n.Files.Commit(ctx, fmt.Sprintf("%s %s: %s", verb, report.Label(), strings.Join(storyIDs(report.Queue), ", ")), paths)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s could not be committed in the vault, so the next sync will refuse until somebody does: %v", strings.Join(paths, " and "), err))
		return
	}
	report.Committed = append(report.Committed, committed...)
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
	if r.Tested {
		verdict := "pass"
		if !r.Gate.Passed {
			verdict = "FAIL"
		}
		fmt.Fprintf(&b, "  tested  %s, once: %s\n", gateOnTheBatch, verdict)
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
