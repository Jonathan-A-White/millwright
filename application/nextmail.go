package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// What mw next says when a close-out settles: the mail to the Mayor (the
// closing mail, a lost write's mail, the events-log line) and the report
// the close-out prints and the mail carries.

// closingMail is the verdict a landing's mail goes under and the block it
// carries, from the story's closing comments, read once: for a landing whose
// comment says an acceptance criterion is not met, the criteria marked in the
// verdict and the bug filed for them, the bugs filed for the findings it did not
// fix (mw-t5shinh.24), then its common errors (mw-gq6.420) and
// its rig-memory proposals, a blank line apart. When the comments cannot be
// read the block is empty, and a landing is never refused for it. A close-out run again (again) files the bug an earlier one could not,
// and sends nothing.
func (n Next) closingMail(ctx context.Context, c *closeOut, report *NextReport, again bool) (verdict, said string) {
	verdict = MailLanded
	if c.tested != "" {
		verdict = MailTested
	}
	comments := c.comments
	if comments == nil {
		var err error
		if comments, err = n.Tracker.StoryComments(ctx, c.id); err != nil {
			return verdict, ""
		}
	}
	var unmet, found string
	if c.tested == "" {
		verdict, unmet = n.landedWithoutACs(ctx, c, report, again, comments)
		unmet = joinBlocks(unmet, n.landedWithMayorToRun(ctx, c, report, comments))
		found = n.filedFromFoundNotFixed(ctx, c, report, again, comments)
	}
	return verdict, joinBlocks(unmet, found, n.commonErrors(ctx, c, comments), n.rigMemoryProposals(ctx, c, comments), closingDocs(comments).MailLine())
}

// rigMemoryProposals is the block from the story's newest closing comment whose
// 'For the rig memory:' section holds a line: the proposals as mw memory lines
// to paste, or for a flat-file rig the lines to place by hand. A newer comment
// whose section says nothing, or holds no line (a sent-back attempt that changed
// no code), does not hide an older comment's lines (mw-gq6.1258); when no
// comment holds a line the newest section is the block. It is empty when no
// comment has the section; a line that does not parse is flagged in the block.
func (n Next) rigMemoryProposals(ctx context.Context, c *closeOut, comments []Comment) string {
	var newest *FactProposals
	for i := len(comments) - 1; i >= 0; i-- {
		proposals := ParseFactProposals(comments[i].Text)
		if !proposals.Found {
			continue
		}
		if proposals.saysSomething() {
			return proposals.RigMemoryBlock(c.path.Rig, c.id, n.flatRigFile(ctx, c.path.Rig))
		}
		if newest == nil {
			newest = &proposals
		}
	}
	if newest == nil {
		return ""
	}
	return newest.RigMemoryBlock(c.path.Rig, c.id, n.flatRigFile(ctx, c.path.Rig))
}

// saysSomething is whether the section holds a line that is not 'nothing':
// a proposal, or a line that does not parse (which is flagged, not dropped).
func (p FactProposals) saysSomething() bool {
	for _, line := range p.Lines {
		if line.Malformed != "" || line.Proposal.Verb != ProposeNothing {
			return true
		}
	}
	return false
}

// flatRigFile is the rig's flat memory file when the seat keeps no facts folder
// for it, else empty; a rig whose folder cannot be read is read as a facts rig.
func (n Next) flatRigFile(ctx context.Context, rig string) string {
	if n.RigFacts == nil {
		return ""
	}
	if has, err := HasFactsFolder(ctx, n.RigFacts, n.Seat, rig); err == nil && !has {
		return FlatRigFile(n.Seat, rig)
	}
	return ""
}

// commonErrors is the block from the story's newest closing comment that has a
// 'For common errors:' section, as mw errors lines to paste, or, for a bug
// whose comments have none, the line that says so (mw-gq6.420). A register
// that cannot be read reads every class as new.
func (n Next) commonErrors(ctx context.Context, c *closeOut, comments []Comment) string {
	// The newest section that names a class wins; a newer one that says nothing
	// does not hide it (mw-gq6.1258), and stands only when no section names one.
	var proposals CommonErrorProposals
	for i := len(comments) - 1; i >= 0; i-- {
		got := ParseCommonErrors(comments[i].Text)
		if !got.Found {
			continue
		}
		if !proposals.Found {
			proposals = got
		}
		if got.saysSomething() {
			proposals = got
			break
		}
	}
	known, kinds := map[string]bool{}, RigKinds{}
	if n.Errors != nil && proposals.Found {
		if files, kindsText, err := n.Errors.ReadErrorRegister(ctx, n.Seat); err == nil {
			for slug := range files {
				known[slug] = true
			}
			kinds, _ = ParseRigKinds(kindsText)
		}
	}
	return proposals.Block(c.path.Rig, c.id, isBugStory(c.detail), known, kinds[c.path.Rig])
}

// saysSomething is whether the section holds a line that is not 'nothing': a
// class, or a line that does not parse (which is flagged, not dropped).
func (p CommonErrorProposals) saysSomething() bool {
	for _, line := range p.Lines {
		if !line.Nothing {
			return true
		}
	}
	return false
}

// joinBlocks is the blocks that say something, a blank line apart.
func joinBlocks(blocks ...string) string {
	var said []string
	for _, block := range blocks {
		if block != "" {
			said = append(said, block)
		}
	}
	return strings.Join(said, "\n\n")
}

// mailLostWrite tells the Mayor of a write to the tracker that was lost for good
// so that it is not only a note in the close-out that nobody reads (mw-gq6.538),
// without saying how often it was tried: see mailLostWriteAfter.
func (n Next) mailLostWrite(ctx context.Context, c *closeOut, report *NextReport, what, said string) {
	n.mailLostWriteAfter(ctx, c, report, what, said, 0)
}

// mailLostWriteAfter is mailLostWrite for a write tried tries times (0 when the
// caller does not know). The mail names the story, what was lost and the command
// that failed, in the tracker's own words, and what the loss costs, and says
// "tried again" only of a write that was (mw-gq6.990). A mail that cannot be
// sent is said on stderr and changes nothing else.
func (n Next) mailLostWriteAfter(ctx context.Context, c *closeOut, report *NextReport, what, said string, tries int) {
	n.mailLostWriteBy(ctx, "mw next", "closing out", c.id, report, what, said, tries)
}

// mailLostWriteBy is mailLostWriteAfter for command, which was doing (closing
// out, retrying) the story when it lost the write.
func (n Next) mailLostWriteBy(ctx context.Context, command, doing, storyID string, report *NextReport, what, said string, tries int) {
	if n.Mailbox == nil {
		return
	}
	n.mailLostWriteVia(ctx, n.Mailbox.Send, command, doing, storyID, report, what, said, tries)
}

// mailLostWriteVia is mailLostWriteBy through send, the one way to a mailbox
// that mw tidy has (mw-gq6.1132).
func (n Next) mailLostWriteVia(ctx context.Context, send func(context.Context, NewMessage) (string, error), command, doing, storyID string, report *NextReport, what, said string, tries int) {
	CloseRetry{Host: n.Host, PushWait: n.PushWait, RetryWait: n.RetryWait, Err: n.Err}.
		mailLostVia(ctx, send, command, doing, storyID, report, what, said, tries)
}

// mailTheMayor sends the one mail that says what became of the story: from mw
// on this host, to the Mayor, titled by the verdict and the story, and holding
// the report mw next prints — the commit and the fuel line of a landing, the
// reason and its detail of a story that landed nothing. It runs once the
// outcome is settled and before the hosts are synced, so that the mail travels
// with the sync that follows.
//
// A mail that cannot be sent is said on stderr and changes nothing: the story
// is landed, or stopped, exactly as it was, and the Mayor still has the rig log
// and the ledger to read.
func (n Next) mailTheMayor(ctx context.Context, c *closeOut, report *NextReport, verdict, said string) {
	if n.Mailbox == nil {
		return
	}
	if err := n.sendMayorMail(ctx, c, report, n.mayorMail(c, report, verdict, said), verdict); err != nil {
		n.saidInEvents(ctx, report, c, fmt.Sprintf("the mail to %s about %s (%s) could not be sent: %v", MayorMailbox, c.id, strings.ToLower(verdict), err))
	}
}

// mayorMail is the mail mailTheMayor sends: what it says is settled by the
// report as it stands.
func (n Next) mayorMail(c *closeOut, report *NextReport, verdict, said string) NewMessage {
	title := strings.Join(strings.Fields(c.detail.Story.Title), " ")
	if title == "" {
		title = c.id
	}
	if verdict == MailTested {
		title = c.tested + ": " + c.findings.Counted()
	}
	slim := verdict != MailRefused && verdict != MailBlocked && verdict != MailHandedBack
	body := report.String()
	if slim {
		body = report.mailBody()
	}
	if report.AfterLandingStopped > 0 {
		body = AfterLandingStoppedLine + "\n" + body
	}
	if report.Landed && report.AfterLandingSkipped != "" {
		body = AfterLandingNotRunLine + ": " + strings.TrimPrefix(report.AfterLandingSkipped, "after landing: ") + "\n" + body
	}
	if len(report.UnitRestartFailed) > 0 {
		body = UnitRestartFailedLine + "\n" + body
	}
	if said != "" {
		body += "\n" + said + "\n"
	}
	if verdict == MailRefused || verdict == MailBlocked {
		body = strings.TrimRight(body, "\n") + "\n\n" + CauseLine(c.id) + "\n"
	}
	if slim {
		body = strings.TrimRight(body, "\n") + "\n" + whereTheWholeReportIs(c.id, report.RunFolder) + "\n"
	}
	return NewMessage{
		From:    SeatIdentity(MwSeat, n.Host),
		To:      MayorMailbox,
		Subject: verdict + ": " + title,
		Body:    body,
	}
}

// sendMayorMail sends the mail, trying it again on a passing fault of the
// network. A mail that cannot be sent is said on stderr and changes nothing
// else: its error is the caller's to keep what it needs (mw-gq6.375).
func (n Next) sendMayorMail(ctx context.Context, c *closeOut, report *NextReport, mail NewMessage, verdict string) error {
	if n.Mailbox == nil {
		return nil
	}
	err := writeRetrying(ctx, n, report, "the mail to the Mayor", func() error {
		_, err := n.Mailbox.Send(ctx, mail)
		return err
	})
	if err == nil {
		return nil
	}
	if n.Err != nil {
		fmt.Fprintf(n.Err, "mw next: the mail to %s about %s (%s) could not be sent: %v\n",
			MayorMailbox, c.id, strings.ToLower(verdict), err)
	}
	return err
}

// writeOrSay writes one record of a close-out that is already past the point of
// stopping — a send-back's, a landing's — trying again on a passing fault of
// the network; a write that fails all the same is a note on the report and a
// line in the events log, never only the pane's stderr.
func (n Next) writeOrSay(ctx context.Context, c *closeOut, report *NextReport, what, failure string, write func() error) {
	if err := writeRetrying(ctx, n, report, what, write); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: %v", failure, err))
		n.saidInEvents(ctx, report, c, fmt.Sprintf("%s: %v", failure, err))
	}
}

// saidInEvents writes a failed close-out write to the events log, as a job
// that failed. A log that cannot be written is a note.
func (n Next) saidInEvents(ctx context.Context, report *NextReport, c *closeOut, detail string) {
	if n.Events == nil {
		return
	}
	if _, err := (EventEmit{
		Log: n.Events, Now: n.now,
		Event: events.Event{
			Kind: events.KindJob, Actor: JobCloseOutUnrecorded + "@" + n.Host,
			From: events.JobRunning, To: events.JobFailed,
			Detail: events.CutDetail(detail),
		},
	}).Run(ctx); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the job event for %s could not be written: %v", c.id, err))
	}
}

// String is the close-out as a person reads it.
func (r NextReport) String() string { return r.text(false) }

// mailBody is the report as a Landed or Tested mail carries it: String without
// the lines the ledger, the vault's log and the run folder keep (the ledger line
// cut to its model and effort and its fuel, the vault commit, the plain closed
// line, a bench line that is no flag), so the mail holds what its reader acts on
// and the reader is told where the whole report is (whereTheWholeReportIs).
func (r NextReport) mailBody() string { return r.text(true) }

// whereTheWholeReportIs is the last line of a Landed or Tested mail: the folder
// of the run that keeps the whole report, and the ledger. runFolder is a
// batch's; a story's is runs/<id>/, which a story id that is no name for a
// directory has not.
func whereTheWholeReportIs(storyID, runFolder string) string {
	if runFolder == "" {
		runFolder = runFilePath(storyID, "")
	}
	if runFolder == "" {
		return "The whole report is in the seat's ledger."
	}
	return "The whole report is in " + strings.TrimSuffix(runFolder, "/") + "/ and in the seat's ledger."
}

// fuelCells is a ledger line cut to its model and effort cell and its fuel
// cell, " | " apart, or the line as it is when it has no such cells.
func fuelCells(line string) string {
	cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), " | ")
	if len(cells) < 5 {
		return line
	}
	return strings.TrimSpace(cells[3]) + " | " + strings.TrimSpace(cells[4])
}

func (r NextReport) text(forMail bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "next on %s: %s\n", r.Host, r.StoryID)
	switch {
	case r.DelegatedTo != "":
		fmt.Fprintf(&b, "  there   the story was worked on %s: mw next ran there, and printed:\n", r.DelegatedTo)
		for _, line := range strings.Split(strings.TrimRight(r.Delegated, "\n"), "\n") {
			fmt.Fprintf(&b, "            %s\n", line)
		}
	case r.LandedEarlier:
		fmt.Fprintf(&b, "  landed  on %s by an earlier run of mw next: nothing was merged, tested or pushed again\n", r.Target)
	case r.Queued:
		fmt.Fprintf(&b, "  queued  for batch landing as %s (%d commits); %s is not touched until its batch lands\n", r.QueuedAs, r.Commits, r.Target)
		if r.Trusted != "" {
			fmt.Fprintf(&b, "          tested by mw check at %s: the rig's tests were not run again\n", shortCommit(r.Trusted))
		}
		if r.TestedParts != "" {
			fmt.Fprintf(&b, "          %s\n", r.TestedParts)
		}
	case r.FixedOnMain != nil:
		fmt.Fprintf(&b, "  fixed   %s: nothing to land; the story is closed, its worktree and branch taken away\n", r.FixedOnMain.reason())
	case r.Landed:
		fmt.Fprintf(&b, "  landed  %s on %s (%d commits, %d push(es))\n", r.How.LandedAs(r.Target), r.Target, r.Commits, r.Pushes)
		if len(r.How.Unioned) > 0 {
			fmt.Fprintf(&b, "  trim-merged: %s (conflicts in files the rig lists, settled by mw's trim-safe driver: both sides' added lines kept, removed ones left out, tested together)\n", strings.Join(r.How.Unioned, ", "))
		}
		for _, line := range RegenerationLines(r.How.Regenerated) {
			fmt.Fprintf(&b, "  %s (conflict in a generated file the rig lists, settled by its generator on the merged tree, tested together)\n", line)
		}
	case r.Tested != "" && r.Findings.Text == "":
		fmt.Fprintf(&b, "  tested  %s by an earlier run of mw next: nothing was read or written again\n", r.Tested)
	case r.Tested != "":
		fmt.Fprintf(&b, "  tested  %s: %s; a Tester story, so nothing was merged, raised or pushed\n", r.Tested, r.Findings.Split())
	case r.Interrupted:
		fmt.Fprintf(&b, "  INTERRUPTED %s\n", r.Why)
		b.WriteString("          nothing was recorded as its outcome: its claim, worktree and branch are as they were\n")
	case r.MainRed != nil:
		fmt.Fprintf(&b, "  HELD (%s) %s\n", r.Reason, r.Why)
		b.WriteString("          not sent back and no attempt counted: the next dispatch tick after the bead that carries main's fix is closed runs its close-out alone again\n")
	case r.HandedBack:
		fmt.Fprintf(&b, "  HANDED BACK (%s) %s\n", r.Reason, r.Why)
		b.WriteString("          its claim, worktree and branch are kept: the next dispatch tick runs its close-out alone again, not a new session\n")
	case r.SentBack != "":
		fmt.Fprintf(&b, "  SENT BACK (%s) %s\n", r.Reason, r.Why)
		if r.Reason == ReasonMergedTestsFail {
			fmt.Fprintf(&b, "          to %s, a fresh session fixing the merged tests; sent back once, never again\n", r.SentBack)
		} else {
			fmt.Fprintf(&b, "          to %s, a fresh session rebasing onto %s; sent back only while the target keeps moving, three times at most\n", r.SentBack, r.Target)
		}
	default:
		if r.Reason == "" {
			fmt.Fprintf(&b, "  STOPPED %s\n", r.Why)
		} else {
			fmt.Fprintf(&b, "  STOPPED (%s) %s\n", r.Reason, r.Why)
		}
	}
	if r.Landed {
		for _, line := range r.Tags.Lines() {
			b.WriteString(line + "\n")
		}
	}
	switch {
	case r.Rig.Moved:
		fmt.Fprintf(&b, "  rig     %s fast-forwarded to %s on %s\n", r.RigDir, shortCommit(r.How.Commit), r.Target)
	case r.Rig.Left != "":
		fmt.Fprintf(&b, "  rig     %s left as it was, not brought up to %s: %s\n", r.RigDir, r.Target, r.Rig.Left)
	case r.Rig.Level && r.Rig.Held == "":
		fmt.Fprintf(&b, "  rig     %s was already at %s on %s\n", r.RigDir, shortCommit(r.How.Commit), r.Target)
	case r.Rig.Held != "":
		fmt.Fprintf(&b, "  rig     %s is not a place to build %s from: %s\n", r.RigDir, r.Target, r.Rig.Held)
	}
	if r.TesterFiled != "" {
		fmt.Fprintf(&b, "  tester  %s filed to drive this landing on a phone-sized screen\n", r.TesterFiled)
	}
	if len(r.Findings.Held) > 0 {
		fmt.Fprintf(&b, "  held    %s filed by the Tester for its findings: release or drop them\n", strings.Join(r.Findings.Held, ", "))
	}
	if len(r.Findings.Duplicates) > 0 {
		fmt.Fprintf(&b, "  twin    %s already open, so the Tester filed nothing for a finding the same\n", strings.Join(r.Findings.Duplicates, ", "))
	}
	if r.Swept > 0 {
		fmt.Fprintf(&b, "  stopped %d leftover processes under %s\n", r.Swept, r.SweptDir)
	}
	if len(r.Uncommitted) > 0 {
		fmt.Fprintf(&b, "  left    uncommitted work, %d path(s), in the worktree:\n", len(r.Uncommitted))
		for _, line := range strings.Split(bullets(r.Uncommitted, UncommittedListed), "\n") {
			fmt.Fprintf(&b, "            %s\n", line)
		}
	}
	for _, line := range r.Bench {
		if forMail && !strings.HasPrefix(line, "FLAG ") {
			continue
		}
		fmt.Fprintf(&b, "  bench   %s\n", line)
	}
	if r.Ledger != "" {
		if forMail {
			fmt.Fprintf(&b, "  ledger  %s\n", fuelCells(r.Ledger))
		} else {
			fmt.Fprintf(&b, "  ledger  %s\n", r.Ledger)
		}
	}
	if len(r.Committed) > 0 && !forMail {
		fmt.Fprintf(&b, "  commit  %s committed in the vault\n", strings.Join(r.Committed, ", "))
	}
	if r.NotCommitted != "" {
		fmt.Fprintf(&b, "  %s\n", r.NotCommitted)
	}
	if r.Closed && !forMail {
		b.WriteString("  closed  the story is closed\n")
	}
	if r.FormulaClosed.Any() {
		fmt.Fprintf(&b, "  formula closed %s\n", r.FormulaClosed)
	}
	if r.Queued {
		b.WriteString("  open    the story stays open and claimed, its worktree kept, until its batch lands it\n")
	}
	if r.DemoHeld {
		b.WriteString("  closed  " + DemoLeftOpen + "\n")
	}
	if r.Landed && !r.Closed && !r.DemoHeld {
		fmt.Fprintf(&b, "  OPEN    the story is landed but still open: %s\n", firstLine(r.NotClosed))
		fmt.Fprintf(&b, "          nothing above is undone; run `mw next %s` again to close it, and nothing is merged or ledgered twice\n", r.StoryID)
		if r.Assignee != "" {
			b.WriteString(r.claimHint())
		}
	}
	for _, id := range r.Abandoned {
		fmt.Fprintf(&b, "  gone    %s is claimed here with no session behind it\n", id)
	}
	for _, id := range r.GaveBack {
		fmt.Fprintf(&b, "  open    %s was claimed here but its session never started; the claim was given back\n", id)
	}
	for _, said := range r.CrossSlot {
		fmt.Fprintf(&b, "  slot    %s\n", said)
	}
	for _, note := range r.Notes {
		fmt.Fprintf(&b, "  note    %s\n", note)
	}
	if r.Synced {
		fmt.Fprintf(&b, "  synced  %s\n", r.Sync)
	}
	if r.Dispatched {
		b.WriteString(r.Dispatch.String())
	}
	return b.String()
}

// claimHint is what a person is told when a landed story would not close
// because somebody's name holds the claim: whose name it is, why mw cannot act
// under it, and the bd command that closes the story as its holder. The command
// is written for a shell to read as it stands, so a name with a space in it —
// as the claims made before mw acted under a name of its own carry — is quoted.
func (r NextReport) claimHint() string {
	mw := SeatIdentity(MwSeat, r.Host)
	closeIt := "`" + strings.Join([]string{"bd", "--actor", shellWord(r.Assignee), "close", shellWord(r.StoryID),
		"--reason", shellWord("landed")}, " ") + "`"
	if r.Assignee == mw {
		return fmt.Sprintf("          %s holds the claim, which is mw's own name: close it under that name: %s\n", r.Assignee, closeIt)
	}
	return fmt.Sprintf("          %s holds the claim, which predates mw acting as %s: a story claimed under another name is closed under it: %s\n",
		r.Assignee, mw, closeIt)
}

// stepList is the formula steps a session left open, one per line.
func stepList(steps []FormulaStep) string {
	said := make([]string, 0, len(steps))
	for _, step := range steps {
		said = append(said, "- "+step.ID+" · "+strings.TrimSpace(step.Title))
	}
	return strings.Join(said, "\n")
}
