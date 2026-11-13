package application

// The rendering of mw status: StatusReport as the text a person reads on a
// phone, its section headings, and the line helpers (clip, wrapInto) every
// section writes through. It takes only a StatusReport and what a section
// holds, never a Status, so it reads and writes nothing (mw-6ww.175.6).

import (
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// WaitingHeading is what heads the section for stories the Governor must be
// present for. The report leaves the section out when there are none.
const WaitingHeading = "WAITING FOR THE GOVERNOR"

// HisToDoHeading heads the short section for the beads on the Governor's own
// to-do list (labelled hitl:todo), which WAITING FOR THE GOVERNOR leaves out.
const HisToDoHeading = "HIS TO DO"

// WaitingOnMayorHeading is what heads the section for needs that wait on the
// Mayor: a hands bead with no step filed yet. The report leaves the section
// out when there are none.
const WaitingOnMayorHeading = "WAITING ON THE MAYOR"

// WaitingOnOthersHeading is what heads the section for beads labelled
// waiting:others, whose Done-when the Mayor is to recheck each day (mw has no
// model to do it). The report leaves the section out when there are none.
const WaitingOnOthersHeading = "WAITING ON OTHERS, RECHECK DONE-WHEN"

// HeldHandsHeading is what heads the section for held beads that keep a hands
// step: the view offers no Run until the bead is open. The report leaves the
// section out when there are none.
const HeldHandsHeading = "HELD, WITH A HANDS STEP"

// OpenButClaimedHeading is what heads the section for open stories that still
// carry a host's claim (assignee mw@<host>) with no session behind it: `bd
// ready --unassigned` and so mw dispatch pass them over until `mw retry`
// gives the claim back (mw-gq6.417). The report leaves it out when there are
// none.
const OpenButClaimedHeading = "OPEN BUT CLAIMED"

// StuckClaimsHeading is what heads the section for in-progress stories held by a
// dispatch whose lease ran out and whose Path names no host (NoSessionClaim):
// they are on no host's list of what is running, so nothing else says them
// (mw-gq6.877). The report leaves it out when there are none.
const StuckClaimsHeading = "STUCK CLAIMS"

// HeldForHeading is what heads the section naming the beads still open behind a
// held-for-<event> label, so that the work an event held is not forgotten once
// the event ends (mw-gq6.766).
const HeldForHeading = "HELD FOR AN EVENT"

// NoFilesKeyHeading heads the section naming the open or held stories of a rig
// that expects a files key which have none.
const NoFilesKeyHeading = "NO FILES KEY"

// WrongRigHeading heads the section naming the open, held or running stories
// whose files key names paths that exist in none of their rig's checkout here.
const WrongRigHeading = "WRONG RIG?"

// NoSweepHeading heads the section naming the open bugs, filed after the sweep
// rule, whose acceptance has no usable Sweep: line (mw-gq6.419).
const NoSweepHeading = "BUGS WITHOUT A SWEEP"

// FormulaNotInstalledHeading heads the section naming the open stories whose
// formula this vault has not installed, which mw dispatch passes over for it
// (mw-gq6.497).
const FormulaNotInstalledHeading = "FORMULA NOT INSTALLED"

// UncheckedErrorsHeading heads the section naming the classes of the
// common-errors register not yet checked in every rig of their kinds: the
// register's standing sweep (mw-gq6.420).
const UncheckedErrorsHeading = "ERROR CLASSES TO CHECK"

// EpicsMissingHeading heads the section naming the open epics that do not meet
// what their rig requires of an epic, and EpicsWaivedHeading the one naming the
// epics the Governor waived it for. Each is left out when it has none.
const (
	EpicsMissingHeading = "EPICS MISSING REQUIREMENTS"
	EpicsWaivedHeading  = "EPICS WAIVED"
)

// FinishedHeading is what heads the section naming the open beads that look
// finished: an epic or map whose children are all closed, a grilling or
// research ticket whose epics are all closed. Nothing is closed by it: the
// Mayor decides.
const FinishedHeading = "DONE, STILL OPEN"

// noSessionLine is what the report says of a story in progress with no live
// session, with the one command that fixes it: a refused story is recovered by
// mw retry, any other is given back by the host's next mw dispatch tick. It is
// short so that clip leaves the command whole.
func (r RunningStory) noSessionLine() string {
	fix := "mw dispatch"
	if r.Refused() {
		fix = "mw retry " + r.Detail.Story.ID
	}
	return fmt.Sprintf("NO SESSION %s, fix: %s", gcAge(r.Now.Sub(r.NoSession)), fix)
}

// refusedLine is what the report says of a claim whose landing was refused: it
// is kept, with its worktree and branch, and no dispatch tick gives it back.
const refusedLine = "refused, waiting on the Mayor (mw retry or a hold)"

// handedBackLine is what the report says of a claim handed back at the merge
// slot's overall most.
const handedBackLine = "handed back at the merge slot's overall most: the next dispatch tick runs its close-out alone again"

// strandedLine is what the report says of a claim whose lease has lapsed, and
// what the host's next dispatch tick does with it for the run state it is in:
// a landed one is closed, never given back (mw-gq6.950); any other is given
// back (a refused, queued or handed-back claim has a line of its own).
func strandedLine(since time.Time, run string) string {
	if run == RunLanded {
		return fmt.Sprintf("STRANDED: lease ran out %s with no heartbeat since, so no session is working it, and its last run landed; the host that holds it closes the story on its next dispatch tick", since.UTC().Format(LastSyncFormat))
	}
	return fmt.Sprintf("STRANDED: lease ran out %s with no heartbeat since, so no session is working it; the host that holds it gives the claim back on its next dispatch tick", since.UTC().Format(LastSyncFormat))
}

// mainShort is main's commit as the report names it: its first seven characters.
func mainShort(main string) string {
	if len(main) > 7 {
		return main[:7]
	}
	return main
}

// behindMain says a host whose mw was built from commit is not level with main:
// main is known and is not that commit, the shorter of the two being a prefix of
// the longer. A host that noted no commit runs an mw older than the note.
func behindMain(commit, main string) bool {
	if main == "" {
		return false
	}
	return commit == "" || !(strings.HasPrefix(main, commit) || strings.HasPrefix(commit, main))
}

// String is the status report as a person reads it on a phone: no line wider
// than Width, however long a title runs.
func (r StatusReport) String() string {
	var b strings.Builder
	clip(&b, fmt.Sprintf("mw status · %s", r.Host))
	if r.IdeasOnly != "" {
		// The line is too wide for the phone with its note that to-dos still
		// work, which goes on a line of its own.
		head, note, hasNote := strings.Cut(r.IdeasOnly, "; ")
		clip(&b, head)
		if hasNote {
			clip(&b, "  "+note)
		}
	}
	b.WriteString("\n")

	if r.Halt != nil {
		if r.SyncMode == BeadsSyncBackup {
			clip(&b, backupHaltLine(r.Host, *r.Halt))
		} else {
			clip(&b, haltLine(r.Host, *r.Halt))
		}
		b.WriteString("\n")
	}

	if r.BeadsKnown {
		clip(&b, beadsLine(r.BeadsBytes, r.BeadsBudgetBytes)+r.gcSaid())
		clip(&b, r.syncModeLine())
		b.WriteString("\n")
	}

	r.Dampers.Write(&b)

	if line := r.HereGivesWay.line(r.Host); line != "" {
		wrapInto(&b, "", line)
	}
	if r.Drained {
		clip(&b, fmt.Sprintf("host: drained (cap 0), %d running: it starts nothing, and still syncs and closes out", r.InUse))
		if r.CapWhy != "" {
			clip(&b, "  cap 0: "+r.CapWhy)
		}
	}
	if r.Cap > 0 {
		clip(&b, fmt.Sprintf("host: %d of cap %d", r.InUse, r.Cap))
		if r.CapWhy != "" {
			clip(&b, fmt.Sprintf("  cap %d: %s", r.Cap, r.CapWhy))
		}
		// One line a reason, so that none runs past a phone's width.
		for _, why := range strings.Split(r.NoRoom, "; ") {
			if why != "" {
				clip(&b, "  held back, no room: "+why)
			}
		}
		if line := r.Grist.Line(); line != "" {
			clip(&b, line)
			if capLine := r.Grist.CapLine(); capLine != "" {
				clip(&b, "  "+capLine)
			}
		}
		if line := r.Grist.PoolLine(); line != "" {
			clip(&b, "  "+line)
		}
		b.WriteString("\n")
	}

	clip(&b, fmt.Sprintf("RUNNING (%d)", len(r.Running)))
	if len(r.Running) == 0 {
		clip(&b, "  nothing running")
	}
	for _, rs := range r.Running {
		rs.write(&b)
	}
	b.WriteString("\n")

	if r.Paused != nil {
		clip(&b, fmt.Sprintf("PAUSED by %s %s: no story is started", r.Paused.Actor, r.Paused.At.UTC().Format("01-02 15:04Z")))
		b.WriteString("\n")
	}

	if hold := r.HarnessHold; hold != nil {
		if hold.Reason == ReasonLoginExpired {
			clip(&b, fmt.Sprintf("HELD · Claude login expired on %s", r.Host))
			clip(&b, fmt.Sprintf("  claims nothing new until %s; once the", hold.Until.UTC().Format("01-02 15:04Z")))
			clip(&b, "  Governor has logged in again, clear it with:")
			clip(&b, "    "+LoginHoldClearCommand(r.Host))
		} else {
			clip(&b, "HELD: "+hold.harnessHeld(r.Host))
		}
		b.WriteString("\n")
	}

	if len(r.Cancelled) > 0 {
		clip(&b, fmt.Sprintf("CANCELLED (%d)", len(r.Cancelled)))
		for _, cancel := range r.Cancelled {
			clip(&b, fmt.Sprintf("  %s · cancelled by %s %s", cancel.Bead, cancel.Actor, cancel.At.UTC().Format("01-02 15:04Z")))
		}
		b.WriteString("\n")
	}

	clip(&b, fmt.Sprintf("READY (%d)", len(r.Ready)))
	if len(r.Ready) == 0 {
		clip(&b, "  nothing ready")
	}
	for _, d := range r.Ready {
		note := ""
		if d.Merged().Host == domain.HostAuto {
			note = "host " + domain.HostAuto
		}
		writeStory(&b, d, note)
		// Wrapped, not clipped: a long path must not cut off whom it waits for.
		if line := r.NoPath[d.Story.ID]; line != "" {
			wrapInto(&b, "    ", line)
		}
		if share, sharing := r.Sharing[d.Story.ID]; sharing {
			wrapInto(&b, "    ", share.String())
		}
		if hold, holding := r.TesterHolds[d.Story.ID]; holding {
			wrapInto(&b, "    ", hold.String())
		}
		if held := r.HeldByFiles[d.Story.ID]; held != "" {
			wrapInto(&b, "    ", held)
		}
		if line := r.UnderHandsStep[d.Story.ID]; line != "" {
			wrapInto(&b, "    ", line)
		}
		if line := r.NoGristKey[d.Story.ID]; line != "" {
			wrapInto(&b, "    ", line)
		}
		if takes, passed := r.NoHostTakes[d.Story.ID]; passed {
			wrapInto(&b, "    ", takes.String())
		}
		if line := r.HeldByPin[d.Story.ID]; line != "" {
			wrapInto(&b, "    ", line)
		}
	}
	b.WriteString("\n")

	if len(r.Waiting) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", WaitingHeading, len(r.Waiting)))
		for _, d := range r.Waiting {
			note := readyOrClaimed(d)
			if kinds := d.HitlKinds(); len(kinds) > 0 {
				note += ", " + strings.Join(kinds, " + ")
			}
			writeStory(&b, d, note)
		}
		for _, warning := range r.Warnings {
			wrapInto(&b, "  ", "warning: "+warning)
		}
		b.WriteString("\n")
	}

	if len(r.HisToDo) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", HisToDoHeading, len(r.HisToDo)))
		for _, d := range r.HisToDo {
			clip(&b, "  "+d.Story.ID+" "+d.Story.Title)
		}
		b.WriteString("\n")
	}

	if len(r.WaitingOnOthers) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", WaitingOnOthersHeading, len(r.WaitingOnOthers)))
		for _, w := range r.WaitingOnOthers {
			note := "waiting on " + w.Of
			if w.Of == "" {
				note = "waiting on others"
			}
			if !w.Since.IsZero() {
				note += " since " + viewClock(w.Since)
			}
			writeStory(&b, w.Story, note)
		}
		b.WriteString("\n")
	}

	if len(r.WaitingOnMayor) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", WaitingOnMayorHeading, len(r.WaitingOnMayor)))
		for _, need := range r.WaitingOnMayor {
			clip(&b, "  "+need.Bead+" · "+needAge(need, r.NeedsAt))
			clip(&b, "    "+needWhy(need))
		}
		b.WriteString("\n")
	}

	if len(r.HeldHands) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", HeldHandsHeading, len(r.HeldHands)))
		for _, d := range r.HeldHands {
			writeStory(&b, d, "no Run until it is open")
		}
		b.WriteString("\n")
	}

	if len(r.HeldFor) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", HeldForHeading, len(r.HeldFor)))
		for _, held := range r.HeldFor {
			clip(&b, fmt.Sprintf("  %s · %d open: %s", held.Label, len(held.Beads), strings.Join(held.Beads, " ")))
		}
		b.WriteString("\n")
	}

	if r.Icebox.Known {
		clip(&b, r.Icebox.Line())
		b.WriteString("\n")
	}

	if len(r.OpenButClaimed) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", OpenButClaimedHeading, len(r.OpenButClaimed)))
		for _, d := range r.OpenButClaimed {
			writeStory(&b, d, "claimed by "+d.Assignee+", no session")
			if hasLabel(d.Labels, RunState+":"+RunLanded) {
				// Its last run landed: a retry is refused, it wants closing (mw-gq6.954).
				clip(&b, "    its last run landed; closes it: mw next "+d.Story.ID)
			} else {
				clip(&b, "    frees it: mw retry "+d.Story.ID)
			}
		}
		b.WriteString("\n")
	}

	if len(r.NoSessionClaims) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", StuckClaimsHeading, len(r.NoSessionClaims)))
		for _, d := range r.NoSessionClaims {
			since := d.LeaseExpires.UTC().Format(LastSyncFormat)
			switch run := r.NoSessionRuns[d.Story.ID]; run {
			case "", RunRunning:
				writeStory(&b, d, "STUCK: claimed by "+d.Assignee+", lease ran out "+since+", no host or session names it; any host's next mw dispatch tick gives it back")
				clip(&b, "    or now: mw retry "+d.Story.ID)
			case RunBlocked:
				// Dispatch keeps a refused claim: no tick gives it back (mw-gq6.941).
				writeStory(&b, d, "STUCK: claimed by "+d.Assignee+", lease ran out "+since+", no host or session names it")
				wrapInto(&b, "    ", refusedLine)
				clip(&b, "    mw retry "+d.Story.ID+" --hold")
			case RunLanded:
				// Landed already: mw retry would work it a second time. It wants
				// closing, which mw next does without landing anything again
				// (mw-gq6.954).
				writeStory(&b, d, "STUCK: claimed by "+d.Assignee+", lease ran out "+since+", no host or session names it; its last run landed, so it only wants closing")
				clip(&b, "    mw next "+d.Story.ID)
			default:
				writeStory(&b, d, "STUCK: claimed by "+d.Assignee+", lease ran out "+since+", no host or session names it; its run state is "+run+", which no dispatch tick gives back")
				clip(&b, "    mw retry "+d.Story.ID)
			}
		}
		b.WriteString("\n")
	}

	clip(&b, fmt.Sprintf("BLOCKED (%d)", len(r.Blocked)))
	if len(r.Blocked) == 0 {
		clip(&b, "  nothing blocked")
	}
	for _, d := range r.Blocked {
		note := ""
		if len(d.Needs) > 0 {
			note = "needs " + strings.Join(d.Needs, ", ")
		}
		writeStory(&b, d, note)
	}
	b.WriteString("\n")

	clip(&b, fmt.Sprintf("OTHER HOSTS (%d)", len(r.Others)))
	if len(r.Others) == 0 {
		clip(&b, "  nothing pathed to another host")
	}
	for _, w := range r.Others {
		w.write(&b, r.Host, r.MainCommit, r.SyncMode.OneDatabase())
	}
	b.WriteString("\n")

	if r.Cloud != nil {
		r.Cloud.write(&b)
		b.WriteString("\n")
	}

	if len(r.BoxHarness) > 0 {
		writeBoxHarness(&b, r.BoxHarness)
		b.WriteString("\n")
	}

	if r.Benchmarks != nil {
		r.Benchmarks.write(&b)
		b.WriteString("\n")
	}

	r.writeCausesMissing(&b)

	if len(r.GristSmoke) > 0 {
		writeGristSmoke(&b, r.GristSmoke)
		b.WriteString("\n")
	}

	if len(r.NoVision) > 0 {
		for _, app := range r.NoVision {
			clip(&b, "VISION: "+app+" has no "+VisionFile)
		}
		b.WriteString("\n")
	}

	if len(r.AtLimit) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", AtTheUsageLimitHeading, len(r.AtLimit)))
		for _, w := range r.AtLimit {
			w.write(&b)
		}
		b.WriteString("\n")
	}

	if r.Ticks.Known() {
		clip(&b, TicksHeading)
		r.Ticks.write(&b, "  ")
		if r.MillhandResume != "" {
			clip(&b, "    "+r.MillhandResume)
		}
		b.WriteString("\n")
	}

	if len(r.RecheckSkips) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", RecheckSkipsHeading, len(r.RecheckSkips)))
		for _, skip := range r.RecheckSkips {
			wrapInto(&b, "  ", skip.Line())
		}
		b.WriteString("\n")
	}

	r.writeEpicShortfalls(&b)

	if len(r.NoFilesKey) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", NoFilesKeyHeading, len(r.NoFilesKey)))
		for _, n := range r.NoFilesKey {
			// Not clipped: the line is the command that mends it.
			b.WriteString("  " + n.Line() + "\n")
		}
		b.WriteString("\n")
	}

	if len(r.WrongRig) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", WrongRigHeading, len(r.WrongRig)))
		for _, w := range r.WrongRig {
			// Not clipped: the line says which story and which rig.
			b.WriteString("  " + w.Line() + "\n")
		}
		b.WriteString("\n")
	}

	if len(r.HandedBack) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", HandedBackHeading, len(r.HandedBack)))
		for _, h := range r.HandedBack {
			writeStory(&b, h.Detail, "handed back on "+handedBackDay(h.At)+", unchanged since")
			clip(&b, "    to run it: "+repathedWith(h.Detail.Story.ID))
		}
		b.WriteString("\n")
	}

	if len(r.NoSweep) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", NoSweepHeading, len(r.NoSweep)))
		for _, d := range r.NoSweep {
			verdict := domain.SweepOfStory(d.Acceptance, d.Description)
			if verdict == domain.SweepInDescription {
				writeStory(&b, d, "")
				// Not clipped: the line is the command that mends it.
				b.WriteString("    " + verdict.String() + ": " + verdict.MoveWords(d.Story.ID) + "\n")
				continue
			}
			writeStory(&b, d, verdict.String())
		}
		b.WriteString("\n")
	}

	if len(r.NoFormula) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", FormulaNotInstalledHeading, len(r.NoFormula)))
		for _, d := range r.NoFormula {
			clip(&b, fmt.Sprintf("  %s: formula %s", d.Story.ID, d.Merged().Formula))
		}
		b.WriteString("\n")
	}

	if len(r.UncheckedErrors) > 0 || r.UncheckedErrorsUnread != "" {
		clip(&b, fmt.Sprintf("%s (%d)", UncheckedErrorsHeading, len(r.UncheckedErrors)))
		if r.UncheckedErrorsUnread != "" {
			clip(&b, "  cannot read the register: "+r.UncheckedErrorsUnread)
		}
		for _, unchecked := range r.UncheckedErrors {
			for _, line := range unchecked.Lines() {
				clip(&b, "  "+line)
			}
		}
		b.WriteString("\n")
	}

	r.writeSuite(&b)
	r.writeLandRush(&b)

	if r.FinishedKnown {
		clip(&b, fmt.Sprintf("%s (%d)", FinishedHeading, len(r.Finished)))
		if len(r.Finished) == 0 {
			clip(&b, "  nothing")
		}
		for _, f := range r.Finished {
			clip(&b, "  "+f.ID+" · "+f.Title)
			clip(&b, "    "+f.Why)
		}
		b.WriteString("\n")
	}

	if len(r.RigMemory) > 0 || len(r.RigMemoryUnseen) > 0 || len(r.RigEvals) > 0 {
		clip(&b, RigMemoryHeading)
		for _, size := range r.RigMemory {
			fix := "prune"
			if size.Facts {
				fix = "retire or supersede"
			}
			clip(&b, fmt.Sprintf("  %s %d/%d bytes: %s (Mayor)", size.Rig, size.Bytes, r.RigMemoryBudget, fix))
		}
		for _, size := range r.RigMemoryUnseen {
			facts := "facts are"
			if size.SearchOnlyNewest == 1 {
				facts = "fact is"
			}
			wrapInto(&b, "  ", fmt.Sprintf("%s memory: boot render %d/%d; the last %d new %s search-only: run `mw memory make-room %s --bytes N` (seats/mayor/pruning.md)",
				size.Rig, size.Bytes, r.RigMemoryBudget, size.SearchOnlyNewest, facts, size.Rig))
		}
		for _, failure := range r.RigEvals {
			clip(&b, "  "+failure.Line())
		}
		b.WriteString("\n")
	}

	if r.Events != nil {
		clip(&b, r.Events.Line())
		b.WriteString("\n")
	}

	if !r.Idle.IsZero() {
		line := "IDLE since " + r.Idle.Format("15:04")
		if r.HarnessKnown {
			line += fmt.Sprintf(" · %d harness processes", r.Harness)
		}
		clip(&b, line)
		b.WriteString("\n")
	} else if r.HarnessKnown {
		clip(&b, fmt.Sprintf("HARNESS %d processes", r.Harness))
		b.WriteString("\n")
	}

	if r.Network != nil {
		clip(&b, r.Network.Line())
		b.WriteString("\n")
	}

	if len(r.UsageLines) > 0 {
		for _, line := range r.UsageLines {
			clip(&b, line)
		}
		b.WriteString("\n")
	}

	if r.VPSNginx != nil {
		clip(&b, r.VPSNginx.Line())
		b.WriteString("\n")
	}
	if r.Standby != nil {
		clip(&b, r.Standby.Line())
		b.WriteString("\n")
	}

	clip(&b, fmt.Sprintf("FUEL today (Builder ledger): %s tokens", Thousands(r.FuelToday)))
	b.WriteString("\n")
	return b.String()
}

// writeEpicShortfalls is the two sections about epics and their rig's
// requirements: the epics still missing something, then the ones the Governor
// waived. Each is left out when there is nothing to put in it.
func (r StatusReport) writeEpicShortfalls(b *strings.Builder) {
	for _, section := range []struct {
		heading string
		waived  bool
	}{{EpicsMissingHeading, false}, {EpicsWaivedHeading, true}} {
		var lines []string
		for _, epic := range r.EpicShortfalls {
			lacks := epic.Missing
			if section.waived {
				lacks = epic.Waived
			}
			if len(lacks) == 0 {
				continue
			}
			var names []string
			for _, lack := range lacks {
				if lack.Section {
					names = append(names, lack.Name+" section")
				} else {
					names = append(names, lack.Name+" story")
				}
			}
			lines = append(lines, "  "+epic.ID+" · "+strings.Join(names, ", "))
		}
		if len(lines) == 0 {
			continue
		}
		clip(b, fmt.Sprintf("%s (%d)", section.heading, len(lines)))
		for _, line := range lines {
			clip(b, line)
		}
		b.WriteString("\n")
	}
}

// needAge is how long a need has waited at now, or that it is not known.
func needAge(need PosternViewNeed, now time.Time) string {
	since, err := time.Parse(time.RFC3339, need.Since)
	if err != nil || since.IsZero() {
		return "age unknown"
	}
	return Clock(now.Sub(since))
}

// needWhy is the one line a need is worth: its kind, and what it waits on or,
// where it names nothing, what it says.
func needWhy(need PosternViewNeed) string {
	if len(need.WaitingOn) > 0 {
		return need.Kind + ": " + strings.Join(need.WaitingOn, ", ")
	}
	return need.Kind + ": " + need.Text
}

// write is one other host's block: the host and how long it has been quiet,
// the sync it last recorded, and the stories pathed to it — marked stranded,
// with the one line that re-paths them, when the host is asleep. here is the
// host the report is for, and so the host a stranded story is re-pathed to.
// live says the note was read straight out of the one database every host
// shares, rather than out of this host's own copy, which is a cycle behind.
func (w HostWork) write(b *strings.Builder, here, main string, live bool) {
	if w.Retired {
		w.writeRetired(b, here)
		return
	}
	clip(b, fmt.Sprintf("  %s · %s", w.Host, w.state()))
	if line := w.GivesWay.line(w.Host); line != "" {
		wrapInto(b, "    ", line)
	}
	if w.Halt != nil {
		clip(b, "    "+haltLine(w.Host, *w.Halt))
	}
	switch {
	case w.Unreadable():
		clip(b, fmt.Sprintf("    its note says %q, which is not a time", w.Said))
	case !w.LastSync.IsZero() && live:
		clip(b, fmt.Sprintf("    last sync %s", w.LastSync.UTC().Format(LastSyncFormat)))
	case !w.LastSync.IsZero():
		clip(b, fmt.Sprintf("    last sync %s (a cycle behind)", w.LastSync.UTC().Format(LastSyncFormat)))
	}
	if !w.Sign.At.IsZero() {
		clip(b, fmt.Sprintf("    newest sign of life %s %s ago", withArticle(w.Sign.What), Clock(w.SignAge)))
	}
	if w.Asleep {
		clip(b, "    re-path: "+RepathHint+here)
	}
	w.writeMw(b, main)
	w.writeDoctor(b)
	w.Ticks.write(b, "    ")

	note := ""
	if w.Asleep {
		note = "stranded"
	}
	for _, d := range w.Stories {
		writeStoryIn(b, "    ", d, strings.TrimPrefix(note+" · "+w.readyOrClaimed(d), " · "))
		if line := w.Pins[d.Story.ID]; line != "" {
			wrapInto(b, "      ", line)
		}
		if QueuedForBatch(d) {
			wait := BatchWait{Since: d.QueuedAt}
			if had, ok := w.Waits[d.Story.ID]; ok {
				wait = had
			}
			wait.write(b, "      ")
		} else if w.Runs[d.Story.ID] == RunBlocked {
			wrapInto(b, "      ", refusedLine)
		} else if w.Runs[d.Story.ID] == RunCloseOutAgain {
			wrapInto(b, "      ", handedBackLine)
		} else if since := lapsedLease(d, w.Now); !d.Hitl() && !since.IsZero() && !w.GivesWay.Stands {
			wrapInto(b, "      ", strandedLine(since, w.Runs[d.Story.ID]))
		}
	}
}

// writeRetired is a retired host's block: one line saying so, and, when work is
// still pathed to or claimed by it, the line that re-paths it and the stories,
// each once. Nothing of its sleep, doctor, sync or timers is said.
func (w HostWork) writeRetired(b *strings.Builder, here string) {
	clip(b, fmt.Sprintf("  %s · %s", w.Host, w.state()))
	if len(w.Stories) == 0 {
		return
	}
	clip(b, "    re-path: "+RepathHint+here)
	for _, d := range w.Stories {
		writeStoryIn(b, "    ", d, w.readyOrClaimed(d))
		if line := w.Pins[d.Story.ID]; line != "" {
			wrapInto(b, "      ", line)
		}
	}
}

// DoctorStale says the host's doctor report is older than a day, or the note
// is not a verdict at all: neither is to be believed as ok.
func (w HostWork) DoctorStale() bool {
	return w.DoctorSaid != "" && !w.DoctorUnreadable && w.Now.Sub(w.Doctor.At) > DoctorVerdictStale
}

// writeDoctor says what the host's doctor last found, one line a faulty check,
// naming the check and why. An ok report says nothing; one older than a day
// says it is stale, and what it last said was faulty, and one that cannot be
// read says so. A host that has left none says nothing.
func (w HostWork) writeDoctor(b *strings.Builder) {
	switch {
	case w.DoctorSaid == "":
		return
	case w.DoctorUnreadable:
		wrapInto(b, "    ", fmt.Sprintf("doctor note is not a verdict: %q", w.DoctorSaid))
	case w.DoctorStale():
		line := fmt.Sprintf("doctor report is %s old: stale", gcAge(w.Now.Sub(w.Doctor.At)))
		if len(w.Doctor.Faulty) > 0 {
			names := make([]string, len(w.Doctor.Faulty))
			for i, f := range w.Doctor.Faulty {
				names[i] = f.Check + " faulty"
			}
			line += " (last said: " + strings.Join(names, ", ") + ")"
		}
		wrapInto(b, "    ", line)
	default:
		for _, f := range w.Doctor.Faulty {
			wrapInto(b, "    ", "doctor "+f.Check+" faulty: "+f.Reason)
		}
	}
}

// DoctorAlarms are the DAMPERS alarms of a host's doctor, one for each check
// its fresh report left faulty: a stale report is said under the host, but is
// not an alarm about a fault it may no longer have.
func (w HostWork) DoctorAlarms() []DamperAlarm {
	if w.DoctorSaid == "" || w.DoctorUnreadable || w.DoctorStale() {
		return nil
	}
	var alarms []DamperAlarm
	for _, f := range w.Doctor.Faulty {
		alarms = append(alarms, DamperAlarm{Principle: DamperDoctor, Text: fmt.Sprintf("%s: %s faulty: %s", w.Host, f.Check, f.Reason)})
	}
	return alarms
}

// writeMw is the line beside the sync line that says which mw the host runs:
// its commit, and that it is behind main when it is not main's.
func (w HostWork) writeMw(b *strings.Builder, main string) {
	switch {
	case w.MwCommit != "" && w.BehindMain:
		clip(b, fmt.Sprintf("    mw %s · behind main (%s)", w.MwCommit, mainShort(main)))
	case w.MwCommit != "":
		clip(b, "    mw "+w.MwCommit)
	case w.BehindMain:
		clip(b, fmt.Sprintf("    mw commit not noted · behind main (%s)", mainShort(main)))
	}
}

// state is how a host's silence reads at the head of its block.
func (w HostWork) state() string {
	switch {
	case w.Retired:
		return "retired"
	case w.NeverSynced():
		return "ASLEEP, never synced"
	case w.Unreadable():
		return "ASLEEP, last sync unreadable"
	case w.Asleep:
		return "ASLEEP, silent " + Clock(w.Silent)
	default:
		return "synced " + Clock(w.Silent) + " ago"
	}
}

// haltLine is the one line a halted sync is worth, wherever it is shown: the
// host, since when, and what bd said.
func haltLine(host string, halt SyncHaltInfo) string {
	return fmt.Sprintf("host %s: sync halted since %s: %s", host, halt.At.UTC().Format(LastSyncFormat), halt.Said)
}

// backupHaltLine is haltLine on the host that keeps the one database, where
// the only cycle that can halt is its backup.
func backupHaltLine(host string, halt SyncHaltInfo) string {
	return fmt.Sprintf("host %s: beads backup halted since %s: %s", host, halt.At.UTC().Format(LastSyncFormat), halt.Said)
}

// syncModeLine is the one line how this host's beads are synced is worth,
// and on the host that keeps the one database, when it last backed it up.
func (r StatusReport) syncModeLine() string {
	line := r.syncModeSaid()
	if r.SyncMode == BeadsSyncShared && r.SyncWhy != "" {
		// "boost of desktop" already says the database is kept elsewhere, and
		// both together would not fit a phone.
		return "BEADS SYNC shared · " + r.SyncWhy
	}
	if r.SyncWhy != "" {
		line += " · " + r.SyncWhy
	}
	return line
}

func (r StatusReport) syncModeSaid() string {
	switch r.SyncMode {
	case BeadsSyncBackup:
		switch {
		case r.LastBackupSaid == "":
			return "BEADS SYNC backup · never backed up"
		case r.LastBackup.IsZero():
			return "BEADS SYNC backup · last backup unreadable"
		default:
			return "BEADS SYNC backup · backed up " + Clock(r.BackupAge) + " ago"
		}
	case BeadsSyncShared:
		return "BEADS SYNC shared · kept on another host"
	case BeadsSyncAuto:
		return "BEADS SYNC auto"
	default:
		return "BEADS SYNC remote"
	}
}

// beadsLine is the one line this host's own beads database is worth: its
// size, and a warning once it is past budget, so the Mayor sees it without
// asking a Clerk to run du.
func beadsLine(bytes, budget int64) string {
	if bytes > budget {
		return fmt.Sprintf("BEADS %s: past the %s budget", formatBytes(bytes), formatBytes(budget))
	}
	return fmt.Sprintf("BEADS %s", formatBytes(bytes))
}

// gcSaid is what the BEADS line adds about the host's last collection, so
// that one starved of its cadence shows without a Clerk reading the note.
// Nothing for a host that does not collect.
func (r StatusReport) gcSaid() string {
	switch {
	case r.SyncMode == BeadsSyncShared:
		return ""
	case r.LastGCSaid == "":
		return " · never collected"
	case r.LastGC.IsZero():
		return " · last GC unreadable"
	default:
		return " · last GC " + gcAge(r.GCAge) + " ago"
	}
}

// gcAge writes how long ago a collection ran: whole days once it is a day or
// more, otherwise as Clock says it.
func gcAge(d time.Duration) string {
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
	return Clock(d)
}

// formatBytes writes a byte count the way a person reads one: whole
// gigabytes once there are any, otherwise whole megabytes, otherwise the
// bytes themselves.
func formatBytes(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fGB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%dMB", n/1_000_000)
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// readyOrClaimed says, in one word, whether a story elsewhere is waiting to be
// taken or already in somebody's hands.
func readyOrClaimed(d StoryDetail) string {
	if strings.EqualFold(strings.TrimSpace(d.Status), StatusInProgress) {
		return "claimed"
	}
	return "ready"
}

// readyOrClaimed is readyOrClaimed for a story pathed to this host: one the
// host's dispatch passed for a reason of its own says that reason, from the
// host's note of its last tick, in place of "ready" (mw-gq6.822). A story
// claimed, or since re-pathed to another host, is not under this note.
func (w HostWork) readyOrClaimed(d StoryDetail) string {
	note := readyOrClaimed(d)
	if p, passed := w.Passed[d.Story.ID]; passed && note == "ready" && d.Merged().Host == w.Host {
		return "passed: " + p.Why
	}
	return note
}

// write is one running story as the report shows it: the story, its session,
// and whatever a person must not mistake it for — a session recorded stopped
// or stuck rather than running, one stopped at the usage limit, a landing
// refused and waiting on the Mayor, or
// a formula step still open that will refuse to let it close out.
func (r RunningStory) write(b *strings.Builder) {
	writeStory(b, r.Detail, "session "+r.Session)
	if r.CloseOut != nil {
		clip(b, "    "+r.CloseOut.since(r.Now))
	}
	if r.Stopped() {
		clip(b, fmt.Sprintf("    NOT RUNNING: run=%s", r.Run))
	}
	if !r.Stranded.IsZero() {
		wrapInto(b, "    ", strandedLine(r.Stranded, r.Run))
	}
	if !r.NoSession.IsZero() {
		clip(b, "    "+r.noSessionLine())
	}
	if r.UsageLimit != "" {
		clip(b, "    stopped at the usage limit: "+r.UsageLimit)
	}
	if past := r.turnsNamedPast(); r.Turns > past {
		line := fmt.Sprintf("    past %d turns: %d so far", past, r.Turns)
		if r.TurnCap > 0 {
			line += fmt.Sprintf(" (capped at %d)", r.TurnCap)
		}
		clip(b, line)
	}
	if r.Refused() {
		clip(b, "    "+refusedLine)
	}
	if r.Queued() {
		wait := BatchWait{Since: r.Detail.QueuedAt}
		if r.Wait != nil {
			wait = *r.Wait
		}
		wait.write(b, "    ")
	}
	if r.HandedBack() {
		clip(b, "    "+handedBackLine)
	}
	if r.CloseBlocked() {
		clip(b, fmt.Sprintf("    close blocked: %d formula step(s) open", r.FormulaSteps))
	}
}

// writeStory is one story's block: its id and rig, its title, and one more
// line of note when there is something to say. A bead with no rig — a ticket
// for the Governor — is its id alone. Every line is clipped to Width on its
// own, so a title of any length never widens the report.
func writeStory(b *strings.Builder, d StoryDetail, note string) {
	writeStoryIn(b, "  ", d, note)
}

// writeStoryIn is writeStory indented under something else — a story listed
// under the host it is pathed to, rather than under a heading.
func writeStoryIn(b *strings.Builder, pad string, d StoryDetail, note string) {
	head := pad + d.Story.ID
	if rig := d.Merged().Rig; rig != "" && d.Guest != "" {
		head += fmt.Sprintf(" · %s · guest: %s", rig, d.Guest)
	} else if rig != "" {
		head += " · " + rig
	}
	if size := d.Merged().Size; size != "" {
		head += " · size " + string(size)
	}
	clip(b, head)
	clip(b, pad+"  "+d.Story.Title)
	if note != "" {
		wrapInto(b, pad+"  ", note)
	}
	// A story started once is the usual story; one started more than once is one
	// that was refused or given back, and is running out of tries: only its own
	// attempts count toward max_attempts, so it says how many those were when
	// mw cause named some test, tooling or process (mw-gq6.450).
	if d.Attempts > 1 {
		line := fmt.Sprintf("%s  attempts %d", pad, d.Attempts)
		if own, _ := OwnAttempts(d); own != d.Attempts {
			line += fmt.Sprintf(" (%d own)", own)
		}
		clip(b, line)
	}
}

// clip writes one line, cut to Width runes so a phone-width terminal never
// wraps it, with the newline it ends in.
func clip(b *strings.Builder, text string) {
	b.WriteString(clipped(text))
	b.WriteString("\n")
}

// clipped is text cut to at most Width runes, marked with an ellipsis when
// something was cut off so a person knows the line does not say everything.
func clipped(text string) string { return clippedTo(text, Width) }

// clippedTo is text cut to at most n runes, marked the same way.
func clippedTo(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// GristSmokeHeading is the heading of the section of mw status that says how
// each app's last grist smoke went.
const GristSmokeHeading = "GRIST SMOKE"

// writeGristSmoke writes the section: one line for each app, under a failed
// one what it holds and each example that failed, in full over as many lines as
// it needs (the line of a phone is Width wide), and a line for each warning.
func writeGristSmoke(b *strings.Builder, records []GristSmokeRecord) {
	clip(b, GristSmokeHeading)
	for _, r := range records {
		if r.At.IsZero() {
			writeGristBehind(b, r)
			continue
		}
		when := r.At.UTC().Format("2006-01-02 15:04Z")
		if !r.Failed && len(r.NotRun) > 0 {
			clip(b, fmt.Sprintf("  %s not run %s", r.App, when))
			for _, why := range r.NotRun {
				wrapInto(b, "    ", "not run: "+why)
			}
			continue
		}
		if !r.Failed {
			clip(b, fmt.Sprintf("  %s ok %s, %d examples", r.App, when, r.Examples))
		} else {
			clip(b, fmt.Sprintf("  %s FAILED %s", r.App, when))
			if len(r.FiledOn) > 0 {
				clip(b, fmt.Sprintf("    filed on %s, not held", strings.Join(r.FiledOn, ", ")))
			} else if r.Rig != "" {
				clip(b, fmt.Sprintf("    the open stories of %s are held", r.Rig))
			} else {
				clip(b, "    no rig's stories are held (run by hand)")
			}
		}
		for _, failure := range r.Failures {
			wrapInto(b, "    ", failure)
		}
		if r.Failed {
			// Not run beside the failures: no failure, and held for nothing.
			for _, why := range r.NotRun {
				wrapInto(b, "    ", "not run: "+why)
			}
		}
		for _, warning := range r.Warnings {
			wrapInto(b, "    ", "warning: "+warning)
		}
		if r.Held() {
			clip(b, "    lifted by a smoke that passes:")
			clip(b, "      mw grist smoke "+r.App)
			clip(b, "    or by hand: mw grist smoke "+r.App+" --lift")
		}
		writeGristBehind(b, r)
	}
}

// writeGristBehind says, when the home's checkout of the app's rig was left
// behind the rig's main, that the mill grinds with older grinds than the app's.
func writeGristBehind(b *strings.Builder, r GristSmokeRecord) {
	if r.Behind == "" {
		return
	}
	clip(b, fmt.Sprintf("  %s checkout left alone, not level with main: the mill grinds with its old grinds", r.App))
	wrapInto(b, "    ", r.Behind)
}

// wrapInto writes text over lines of at most Width runes, each led by indent,
// broken between words (a word longer than a line is cut).
func wrapInto(b *strings.Builder, indent, text string) {
	line := indent
	for _, word := range strings.Fields(text) {
		if len([]rune(line))+len([]rune(word))+1 > Width && strings.TrimSpace(line) != "" {
			clip(b, line)
			line = indent + "  "
		}
		if strings.TrimSpace(line) != "" {
			line += " "
		}
		line += word
	}
	if strings.TrimSpace(line) != "" {
		clip(b, line)
	}
}

// writeBoxHarness is the HARNESS section: each box's harness version, and a
// box with BoxHarnessCrashesNamed or more crashes in the last day named, with
// the sessions that crashed.
func writeBoxHarness(b *strings.Builder, boxes []BoxHarness) {
	clip(b, "HARNESS: Claude Code on each cloud box")
	for _, box := range boxes {
		if box.Version == "" {
			clip(b, fmt.Sprintf("  %s: claude version not known", box.Box))
		} else {
			clip(b, fmt.Sprintf("  %s: claude %s", box.Box, box.Version))
		}
		if box.ReadFail != "" {
			wrapInto(b, "    ", "its panes could not be read for crashes: "+box.ReadFail)
		}
		if box.IntegrityFail != "" {
			wrapInto(b, "    ", "its integrity mark could not be read: "+box.IntegrityFail)
		}
		if !box.Integrity.IsZero() {
			wrapInto(b, "    ", fmt.Sprintf("%s met a corrupt git object or a segmentation fault at %s, under an hour ago; a second within the hour says the box is bad: mw cloud down %s",
				box.Box, box.Integrity.UTC().Format("01-02 15:04Z"), box.Box))
		}
		if len(box.Crashes) < BoxHarnessCrashesNamed {
			continue
		}
		var last time.Time
		var windows []string
		for _, pane := range box.Crashes {
			windows = append(windows, pane.Window)
			if pane.At.After(last) {
				last = pane.At
			}
		}
		clip(b, fmt.Sprintf("  %s: the harness crashed %d times in the last day", box.Box, len(box.Crashes)))
		wrapInto(b, "    ", fmt.Sprintf("last %s: %s", last.UTC().Format("01-02 15:04Z"), strings.Join(windows, ", ")))
	}
}
