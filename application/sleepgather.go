package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The bundle's files, by name.
const (
	SleepTriageFile = "triage.jsonl"
	SleepDiffFile   = "triage-diff.txt"
	SleepQueueFile  = "queue.json"
	SleepGitFile    = "git-log.txt"
	SleepRunsFile   = "runs.txt"
	SleepReportFile = "report.txt"
	SleepFoldsFile  = "folds.txt"
	SleepStuckFile  = "stuck.txt"
	// SleepEvidenceFile holds the Governor's messages of the window, and names
	// his pictures, which sit in the agent's directory beside it; SleepCandidatesFile
	// holds the check text of the open demos and unverified landings they may
	// show met (mw-4xdnis.5).
	SleepEvidenceFile   = "evidence.txt"
	SleepCandidatesFile = "candidates.txt"
)

// sleepBudgets is the most bytes of each part of the bundle the agent is given:
// the one place they are named (mw-gq6.522). A part over its budget is cut by
// mw, and the cut is said. Together they come to SleepTotalBudget, about 35,000
// tokens, which the agent reads in a few turns.
var sleepBudgets = map[string]int{
	SleepTriageFile: 50_000,
	SleepDiffFile:   10_000,
	SleepQueueFile:  24_000,
	SleepGitFile:    20_000,
	SleepRunsFile:   4_000,
	SleepReportFile: 24_000,
	SleepFoldsFile:  12_000,
	SleepStuckFile:  6_000,
	// His review queue, an item to a block: 57 items came to about 30,000 bytes
	// on 2026-10-21 (mw-gq6.674).
	SleepNeedsYouFile: 30_000,
	// A day's messages of his, and the check text of a week's demos and
	// landings, cut to fit a turn like the other parts (mw-4xdnis.5).
	SleepEvidenceFile:   16_000,
	SleepCandidatesFile: 40_000,
}

// SleepTotalBudget is the most bytes of the whole bundle: the sum of the parts'
// budgets.
const SleepTotalBudget = 236_000

// SleepBudgetOf is the most bytes of the part held in file.
func SleepBudgetOf(file string) int { return sleepBudgets[file] }

// SleepPartCount is how many parts the bundle holds.
func SleepPartCount() int { return len(sleepBudgets) }

// sleepDescriptionRunes are the lengths of a bead's description the dump is
// compacted to, one after another, until it fits its budget.
var sleepDescriptionRunes = []int{160, 80, 0}

// SleepSession is one session's result in the vault's runs, and when it was
// written.
type SleepSession struct {
	Story  string
	At     time.Time
	Result SessionResult
}

// SleepRuns reads the sessions' results: the vault's runs/<story>/result.json.
type SleepRuns interface {
	// Sessions are the results written at or after since, oldest first.
	Sessions(ctx context.Context, since time.Time) ([]SleepSession, error)
}

// SleepEarlier finds the dump the last pass kept, for the diff to start from.
type SleepEarlier interface {
	// EarlierDump is the newest dump kept for a date before date (the passes'
	// plans/sleep-<date>/triage.jsonl, else the Mayor's own triage dump), and
	// its name; ok is false when there is none.
	EarlierDump(ctx context.Context, date string) (name, text string, ok bool, err error)
}

// SleepGather gathers the night's bundle (SleepGatherer): the triage dump and
// what moved since the last one, the ready-story queue, each rig's git log, the
// sessions' aggregates and mw report. A part that cannot be read, other than
// the dump, is said so in the bundle and the pass goes on: the agent can be
// told what it is missing, and a bundle short of one part is still worth the
// night.
type SleepGather struct {
	// Dump writes the dump of the live work beads (mw triage dump).
	Dump func(ctx context.Context, w io.Writer) error
	// Mw runs mw itself with these arguments and reports what it printed.
	Mw func(ctx context.Context, args ...string) (string, error)
	// Git reports the git log of the rig checked out at dir since days days
	// ago.
	Git func(ctx context.Context, dir string, days int) (string, error)
	// Rigs are the rigs checked out here, by name.
	Rigs    map[string]string
	Runs    SleepRuns
	Earlier SleepEarlier
	// NeedsYou reads his review queue for needs-you.txt; nil leaves the part
	// saying there is nothing to read it from.
	NeedsYou func(ctx context.Context) (NeedsYouFacts, error)
	// Beads and InboxDir are where evidence.txt and candidates.txt are read
	// from: the tracker's open demos and landings, and the dir the inbox pass
	// saves his messages and pictures in. Without Beads or InboxDir the two
	// parts say they could not be read.
	Beads    VerifyBeads
	InboxDir string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Gather implements SleepGatherer.
func (g SleepGather) Gather(ctx context.Context, window SleepWindow) (SleepBundle, error) {
	now := time.Now()
	if g.Now != nil {
		now = g.Now()
	}
	var dump bytes.Buffer
	if err := g.Dump(ctx, &dump); err != nil {
		return nil, fmt.Errorf("the triage dump: %w", err)
	}
	triage := cutTriage(dump.String(), sleepBudgets[SleepTriageFile])
	triage.Name, triage.File, triage.Keep = "triage dump", SleepTriageFile, true
	bundle := SleepBundle{triage}
	bundle = append(bundle, cutPart("triage diff", SleepDiffFile, g.diff(ctx, window, dump.String())))
	bundle = append(bundle, cutPart("queue", SleepQueueFile, g.said(g.Mw(ctx, "queue", "--json"))))
	bundle = append(bundle, g.gitPart(ctx, window))
	bundle = append(bundle, cutPart("runs", SleepRunsFile, g.runs(ctx, window, now)))
	// The report's text form, which carries what the rule reads; its JSON is
	// ten times the size (413 KB against 17 KB on 2026-10-16).
	bundle = append(bundle, cutPart("report", SleepReportFile, g.said(g.Mw(ctx, "report", "--since", window.ReportSince()))))
	bundle = append(bundle, cutPart("folds", SleepFoldsFile, g.folds(ctx, dump.String())))
	// The held roots that live beads wait behind (mw stuck): the pass names
	// them in its summary (mw-gq6.621).
	// With them, the beads below the priority of what waits on them
	// (mw-gq6.623), read from the same dump.
	bundle = append(bundle, cutPart("stuck", SleepStuckFile, withNewline(g.said(g.Mw(ctx, "stuck")))+inversionsOfDump(dump.String())))
	bundle = append(bundle, g.needsYou(ctx))
	evidence, candidates := g.verifyParts(ctx, window, now)
	bundle = append(bundle, evidence, candidates)
	return bundle, nil
}

// verifyParts are evidence.txt and candidates.txt: his messages and pictures
// written within the window, and the check text of the open demos and the
// unverified landings of the last week. The evidence part carries both lists,
// for the pass to check the agent's proposals against. The sets are
// mw verify match's (ReadVerifyEvidence, VerifyCandidates).
func (g SleepGather) verifyParts(ctx context.Context, window SleepWindow, now time.Time) (SleepPart, SleepPart) {
	evidence := SleepPart{Name: "evidence", File: SleepEvidenceFile, Budget: sleepBudgets[SleepEvidenceFile]}
	candidates := SleepPart{Name: "candidates", File: SleepCandidatesFile, Budget: sleepBudgets[SleepCandidatesFile]}
	if g.Beads == nil || g.InboxDir == "" {
		unread := "(this part could not be read: nothing was given to read his messages and the open demos from)\n"
		evidence.Text, candidates.Text = unread, unread
		return evidence, candidates
	}
	days := window.RunsDays()
	found, err := ReadVerifyEvidence(g.InboxDir, now.Add(-time.Duration(days)*24*time.Hour), nil)
	if err != nil {
		evidence.Text = fmt.Sprintf("(this part could not be read: %v)\n", err)
	}
	wanted, err := VerifyCandidates(ctx, g.Beads, now, DefaultVerifySince)
	if err != nil {
		candidates.Text = fmt.Sprintf("(this part could not be read: %v)\n", err)
	}
	if evidence.Text == "" {
		evidence.Text, evidence.Cut, evidence.Attach, found = evidenceText(found, days, evidence.Budget)
	}
	if candidates.Text == "" {
		candidates.Text, candidates.Cut = candidatesText(wanted, candidates.Budget)
	}
	if evidence.Attach != nil || len(found) > 0 || len(wanted) > 0 {
		evidence.Verify = &SleepVerify{Candidates: wanted, Evidence: found}
	}
	return evidence, candidates
}

const (
	// sleepEvidenceText is the most bytes of one message of his the part holds.
	sleepEvidenceText = 1500
	// sleepMaxPictures is the most pictures given to the agent, the newest: it
	// reads each with a tool call, and a Sunday's week can hold a hundred.
	sleepMaxPictures = 12
)

// evidenceText is evidence.txt: for each message or picture, newest last, its
// txid, its time and his words; a picture is named by the file in the agent's
// directory. The newest verifyMaxEvidence are read. It reports the pictures to
// put in the directory and the evidence it holds.
func evidenceText(found []VerifyEvidence, days, budget int) (text, cut string, attach []SleepAttachment, held []VerifyEvidence) {
	if len(found) == 0 {
		return fmt.Sprintf("(he sent no message or picture in the last %d %s)\n", days, plural(days, "day", "days")), "", nil, nil
	}
	if len(found) > verifyMaxEvidence {
		found = found[len(found)-verifyMaxEvidence:]
	}
	given := map[string]bool{}
	for i := len(found) - 1; i >= 0 && len(given) < sleepMaxPictures; i-- {
		for _, path := range found[i].Files {
			if !strings.EqualFold(filepath.Ext(path), ".txt") && len(given) < sleepMaxPictures {
				given[path] = true
			}
		}
	}
	var out strings.Builder
	for _, e := range found {
		fmt.Fprintf(&out, "## %s, written %s\n", e.Txid, e.At.Format(verifyTimeFormat))
		for _, path := range e.Files {
			name := filepath.Base(path)
			if strings.EqualFold(filepath.Ext(name), ".txt") {
				body, err := os.ReadFile(path)
				if err != nil {
					fmt.Fprintf(&out, "(his message could not be read: %v)\n", err)
					continue
				}
				out.WriteString(strings.TrimSpace(cutBytes(string(body), sleepEvidenceText)) + "\n")
				continue
			}
			if !given[path] {
				fmt.Fprintf(&out, "picture: %s (not given: only the newest %d pictures are)\n", name, sleepMaxPictures)
				continue
			}
			size := 0
			if info, err := os.Stat(path); err == nil {
				size = int(info.Size())
			}
			attach = append(attach, SleepAttachment{Name: name, Path: path, Size: size})
			fmt.Fprintf(&out, "picture: %s (a file in your working directory; read it)\n", name)
		}
		out.WriteString("\n")
	}
	text, cut = cutLines(out.String(), budget)
	return text, cut, attach, found
}

// The check text kept of a candidate, in bytes: a demo's at most sleepDemoCheck,
// a landing's sleepLandingCheck, and the room a candidate's heading takes.
const (
	sleepDemoCheck    = 1200
	sleepLandingCheck = 400
	sleepCandHeading  = 160
)

// candidatesText is candidates.txt: the open demos first, each with its check
// text cut to fit, then as many of the newest landings as the rest of the budget
// holds, with a shorter check each; it says how many landings were left out.
func candidatesText(wanted []VerifyCandidate, budget int) (text, cut string) {
	if len(wanted) == 0 {
		return "(no demo is open and no landing of the last week is unverified)\n", ""
	}
	demos, landings := candidatesOf(wanted, VerifyKindDemo), candidatesOf(wanted, VerifyKindLanding)
	var out strings.Builder
	block := func(c VerifyCandidate, check int) {
		fmt.Fprintf(&out, "## %s (%s): %s\n%s\n\n", c.Bead, c.Kind, c.Title, strings.TrimSpace(cutBytes(c.Check, check)))
	}
	demoCheck := sleepDemoCheck
	if len(demos) > 0 {
		demoCheck = min(sleepDemoCheck, max(sleepLandingCheck, budget*3/4/len(demos)-sleepCandHeading))
	}
	for _, c := range demos {
		block(c, demoCheck)
	}
	// room is left for the line that says what was left out.
	const room = 200
	fit := max(0, (budget-out.Len()-room)/(sleepLandingCheck+sleepCandHeading))
	left := 0
	if len(landings) > fit {
		left = len(landings) - fit
		landings = landings[left:]
	}
	for _, c := range landings {
		block(c, sleepLandingCheck)
	}
	if left > 0 {
		cut = fmt.Sprintf("%d older unverified %s left out to fit; mw verify match reads them", left, plural(left, "landing", "landings"))
		fmt.Fprintf(&out, "(mw cut this part: %s)\n", cut)
	}
	text, note := cutLines(out.String(), budget)
	if note != "" {
		cut = strings.TrimPrefix(cut+"; "+note, "; ")
	}
	return text, cut
}

// needsYou is the part that holds his review queue, with the evidence of the
// stale items; a queue that cannot be read is said so.
func (g SleepGather) needsYou(ctx context.Context) SleepPart {
	part := SleepPart{Name: "needs you", File: SleepNeedsYouFile, Budget: sleepBudgets[SleepNeedsYouFile]}
	if g.NeedsYou == nil {
		part.Text = "(this part could not be read: nothing was given to read his review queue from)\n"
		return part
	}
	facts, err := g.NeedsYou(ctx)
	if err != nil {
		part.Text = fmt.Sprintf("(this part could not be read: %v)\n", err)
		return part
	}
	part.Text, part.Cut = facts.TextCut(part.Budget)
	return part
}

// inversionsOfDump is the priority inversions of the dump's live beads, one a
// line, under a heading; a dump that cannot be read says so.
func inversionsOfDump(dump string) string {
	lines, err := ReadTriageLines(strings.NewReader(dump))
	if err != nil {
		return fmt.Sprintf("\npriority inversions: (the dump could not be read: %v)\n", err)
	}
	beads := make([]domain.GraphBead, len(lines))
	for n, l := range lines {
		beads[n] = domain.GraphBead{ID: l.ID, Status: l.Status, Type: l.Type, Priority: l.Priority, Labels: l.Labels}
		if l.HisPriority {
			beads[n].Labels = append(slices.Clone(l.Labels), HisPriorityLabel)
		}
		for _, dep := range l.Deps {
			beads[n].Edges = append(beads[n].Edges, domain.BeadEdge{To: dep, Kind: "blocks"})
		}
	}
	found := InversionsOf(beads)
	if len(found) == 0 {
		return "\nno priority inversion: no bead sits below what waits on it.\n"
	}
	out := "\npriority inversions: " + fmt.Sprintf("%d %s below what waits on %s (the raise is a proposal; (his) is his word, a card only)\n", len(found), plural(len(found), "bead", "beads"), plural(len(found), "it", "them"))
	for _, one := range found {
		out += one.Text() + "\n"
	}
	return out
}

// folds is mw fold's list of each rig that has an open or held bug, in the
// rigs' name order: the groups of bugs the Mayor folds in the morning
// (mw-gq6.607). A rig with no such bug adds nothing; a list that cannot be read
// is said so under the rig's name.
func (g SleepGather) folds(ctx context.Context, dump string) string {
	lines, err := ReadTriageLines(strings.NewReader(dump))
	if err != nil {
		return fmt.Sprintf("(the dump could not be read for the bugs: %v)\n", err)
	}
	withBugs := map[string]bool{}
	for _, l := range lines {
		status := strings.ToLower(strings.TrimSpace(l.Status))
		isBug := strings.EqualFold(l.Type, LabelBug) || hasLabel(l.Labels, LabelBug)
		if isBug && l.Rig != "" && (status == StatusOpen || status == StatusHeld) {
			withBugs[l.Rig] = true
		}
	}
	if len(withBugs) == 0 {
		return "(no rig has an open or held bug)\n"
	}
	rigs := make([]string, 0, len(withBugs))
	for rig := range withBugs {
		rigs = append(rigs, rig)
	}
	sort.Strings(rigs)
	var out strings.Builder
	for _, rig := range rigs {
		out.WriteString("## " + rig + "\n" + withNewline(g.said(g.Mw(ctx, "fold", rig))) + "\n")
	}
	return out.String()
}

// cutPart is the part held in file, cut to its budget by lines.
func cutPart(name, file, text string) SleepPart {
	budget := sleepBudgets[file]
	cut, note := cutLines(text, budget)
	return SleepPart{Name: name, File: file, Text: cut, Budget: budget, Cut: note}
}

// cutLines is text cut to at most budget bytes, whole lines from its start,
// ending in a line that says what was cut; note says it too, and is "" when
// nothing was cut.
func cutLines(text string, budget int) (cut, note string) {
	if len(text) <= budget {
		return text, ""
	}
	// room is left for the line that says it.
	const room = 160
	keep := budget - room
	if keep < 0 {
		keep = 0
	}
	head := text[:keep]
	if i := strings.LastIndex(head, "\n"); i >= 0 {
		head = head[:i+1]
	} else {
		for !utf8.ValidString(head) {
			head = head[:len(head)-1]
		}
	}
	note = fmt.Sprintf("was %s bytes, cut to its first %s bytes (%d of %d lines)",
		Thousands(len(text)), Thousands(len(head)), strings.Count(head, "\n"), strings.Count(text, "\n"))
	return head + "(mw cut this part: it " + note + ")\n", note
}

// sleepLine is one bead of the dump as the agent reads it: the fields the rule
// weighs, with what is empty left out.
type sleepLine struct {
	ID              string   `json:"id"`
	Type            string   `json:"type"`
	Status          string   `json:"status"`
	Priority        int      `json:"priority"`
	Labels          []string `json:"labels,omitempty"`
	Rig             string   `json:"rig,omitempty"`
	Size            string   `json:"size,omitempty"`
	Parent          string   `json:"parent,omitempty"`
	DaysSinceUpdate int      `json:"days_since_update"`
	Ready           bool     `json:"ready,omitempty"`
	DaysReady       int      `json:"days_ready,omitempty"`
	Deps            []string `json:"deps,omitempty"`
	Dependents      []string `json:"dependents,omitempty"`
	Description     string   `json:"description,omitempty"`
	HisPriority     bool     `json:"his_priority,omitempty"`
}

// cutTriage is the dump compacted for the agent: the dates are left out (the
// days since are in), then the descriptions shortened until the dump fits its
// budget, and, if it still does not, the beads that matter least left out: his
// own priorities first, then the ready ones, the most urgent first. The part
// keeps the whole dump (Whole), which tomorrow's diff starts from.
func cutTriage(dump string, budget int) SleepPart {
	part := SleepPart{Text: dump, Budget: budget}
	lines, err := ReadTriageLines(strings.NewReader(dump))
	if err != nil || len(dump) <= budget {
		cut, note := cutLines(dump, budget)
		part.Text, part.Cut = cut, note
		if note != "" {
			part.Whole = dump
		}
		return part
	}
	part.Whole = dump
	for _, runes := range sleepDescriptionRunes {
		text := encodeSleepLines(lines, runes)
		if len(text) <= budget {
			part.Text = text
			part.Cut = fmt.Sprintf("was %s bytes, compacted to %s: descriptions cut to %d characters, dates left out",
				Thousands(len(dump)), Thousands(len(text)), runes)
			return part
		}
	}
	// Whole beads, in rank order, until the budget is spent; shown in id order.
	ranked := append([]TriageLine(nil), lines...)
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if his(a) != his(b) {
			return his(a)
		}
		if a.Ready != b.Ready {
			return a.Ready
		}
		return a.Priority < b.Priority
	})
	const room = 200
	used, kept := 0, 0
	for _, line := range ranked {
		n := len(encodeSleepLines([]TriageLine{line}, 0))
		if used+n > budget-room {
			break
		}
		used += n
		kept++
	}
	chosen := map[string]bool{}
	for _, line := range ranked[:kept] {
		chosen[line.ID] = true
	}
	var shown []TriageLine
	for _, line := range lines {
		if chosen[line.ID] {
			shown = append(shown, line)
		}
	}
	text := encodeSleepLines(shown, 0)
	part.Cut = fmt.Sprintf("was %s bytes (%d beads), compacted to the %d most urgent beads, without descriptions or dates; the rest are in the diff when they moved",
		Thousands(len(dump)), len(lines), kept)
	part.Text = text + "(mw cut this part: it " + part.Cut + ")\n"
	return part
}

// his is whether the bead's priority is the Governor's: the dump's mark, or the
// label on the bead (a dump made before the label counted still reads right).
func his(l TriageLine) bool { return l.HisPriority || HasHisPriorityLabel(l.Labels) }

// encodeSleepLines is the dump lines as the agent reads them, descriptions cut
// to runes characters.
func encodeSleepLines(lines []TriageLine, runes int) string {
	var out strings.Builder
	enc := json.NewEncoder(&out)
	for _, l := range lines {
		description := []rune(strings.Join(strings.Fields(l.Description), " "))
		if len(description) > runes {
			description = description[:runes]
		}
		_ = enc.Encode(sleepLine{
			ID: l.ID, Type: l.Type, Status: l.Status, Priority: l.Priority, Labels: l.Labels, Rig: l.Rig,
			Size: l.Size, Parent: l.Parent, DaysSinceUpdate: l.DaysSinceUpdate, Ready: l.Ready, DaysReady: l.DaysReady,
			Deps: l.Deps, Dependents: l.Dependents, Description: string(description), HisPriority: his(l),
		})
	}
	return out.String()
}

// said is a part's text, or the plain sentence that it could not be read.
func (g SleepGather) said(text string, err error) string {
	if err != nil {
		return fmt.Sprintf("(this part could not be read: %v)\n", err)
	}
	return text
}

// diff is what moved between the last kept dump and this one.
func (g SleepGather) diff(ctx context.Context, window SleepWindow, dump string) string {
	name, earlier, ok, err := g.Earlier.EarlierDump(ctx, window.Date)
	if err != nil {
		return fmt.Sprintf("(the earlier dump could not be read: %v)\n", err)
	}
	if !ok {
		return "(there is no earlier dump to diff against: this is the first pass)\n"
	}
	old, err := ReadTriageLines(strings.NewReader(earlier))
	if err != nil {
		return fmt.Sprintf("(the earlier dump %s could not be read: %v)\n", name, err)
	}
	fresh, err := ReadTriageLines(strings.NewReader(dump))
	if err != nil {
		return fmt.Sprintf("(today's dump could not be read: %v)\n", err)
	}
	return "Since " + name + ":\n" + DiffTriage(old, fresh).String()
}

// gitPart is the git log part: the room goes to the rigs by water-filling, so
// one busy rig cannot push the others out and a quiet rig's unused share is
// passed on to the rigs that have more to say (mw-gq6.526). Each cut is said
// per rig.
func (g SleepGather) gitPart(ctx context.Context, window SleepWindow) SleepPart {
	budget := sleepBudgets[SleepGitFile]
	logs := g.gitLogs(ctx, window)
	// The headings and the "no commit" lines are not cut: they come out of the
	// budget first, and the rest is shared out among the logs.
	pool := budget
	var sizes []int
	for _, l := range logs {
		pool -= len(l.fixed)
		if l.log != "" {
			sizes = append(sizes, len(l.log))
		}
	}
	shares := waterFill(pool, sizes)
	var text strings.Builder
	var notes []string
	next := 0
	for _, l := range logs {
		text.WriteString(l.fixed)
		if l.log == "" {
			continue
		}
		cut, note := cutLines(l.log, shares[next])
		next++
		if note != "" {
			notes = append(notes, l.rig+" "+note)
		}
		text.WriteString(cut)
	}
	if len(logs) == 0 {
		text.WriteString("(no rig is checked out on this host)\n")
	}
	part := SleepPart{Name: "git log", File: SleepGitFile, Text: text.String(), Budget: budget}
	if len(notes) > 0 {
		part.Cut = strings.Join(notes, "; ")
	}
	return part
}

// waterFill is the share of pool each of the wants may use, in their order:
// one that wants less than an equal share of what is left gets what it wants,
// and what it leaves unused is passed on to the others, so the ones that want
// more are all cut to the same share, never below an equal share of pool.
func waterFill(pool int, wants []int) []int {
	order := make([]int, len(wants))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return wants[order[a]] < wants[order[b]] })
	if pool < 0 {
		pool = 0
	}
	shares := make([]int, len(wants))
	for k, i := range order {
		equal := pool / (len(order) - k)
		if wants[i] < equal {
			equal = wants[i]
		}
		shares[i] = equal
		pool -= equal
	}
	return shares
}

// gitLog is one rig's part of the git log: fixed is what mw writes (its
// heading, or its heading and the line that says there is no log), log the
// commits, which are "" when there are none to show.
type gitLog struct {
	rig, fixed, log string
}

// gitLogs is each rig's log of the last GitDays days, in the rigs' name order.
func (g SleepGather) gitLogs(ctx context.Context, window SleepWindow) []gitLog {
	names := make([]string, 0, len(g.Rigs))
	for name := range g.Rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	logs := make([]gitLog, 0, len(names))
	for _, name := range names {
		entry := gitLog{rig: name, fixed: fmt.Sprintf("## %s\n", name)}
		log, err := g.Git(ctx, g.Rigs[name], window.GitDays())
		switch {
		case err != nil:
			entry.fixed += fmt.Sprintf("(the log could not be read: %v)\n", err)
		case strings.TrimSpace(log) == "":
			entry.fixed += fmt.Sprintf("(no commit in %d days)\n", window.GitDays())
		default:
			entry.log = withNewline(log)
		}
		logs = append(logs, entry)
	}
	return logs
}

// runs is the aggregate of the sessions' results of the window, for each model
// that ran: how many, how many failed, the turns they took, the cache read for
// each turn, the time spent on the model and the tokens.
func (g SleepGather) runs(ctx context.Context, window SleepWindow, now time.Time) string {
	days := window.RunsDays()
	sessions, err := g.Runs.Sessions(ctx, now.AddDate(0, 0, -days))
	if err != nil {
		return fmt.Sprintf("(the sessions could not be read: %v)\n", err)
	}
	return AggregateSleepSessions(sessions, days)
}

// AggregateSleepSessions is the sessions summed up by the model that ran them.
func AggregateSleepSessions(sessions []SleepSession, days int) string {
	if len(sessions) == 0 {
		return fmt.Sprintf("No session result was written in the last %d days.\n", days)
	}
	type sum struct {
		sessions, failed, turns, tokens, cacheRead int
		api                                        time.Duration
		subtypes                                   map[string]int
	}
	byModel := map[string]*sum{}
	for _, s := range sessions {
		model := firstOf(s.Result.ModelID, s.Result.Model, "unknown")
		total := byModel[model]
		if total == nil {
			total = &sum{subtypes: map[string]int{}}
			byModel[model] = total
		}
		total.sessions++
		if s.Result.IsError {
			total.failed++
		}
		total.turns += s.Result.Turns
		total.tokens += s.Result.Fuel.Total()
		total.cacheRead += s.Result.Fuel.CacheRead
		total.api += s.Result.APIDuration
		total.subtypes[firstOf(s.Result.Subtype, "unknown")]++
	}
	models := make([]string, 0, len(byModel))
	for model := range byModel {
		models = append(models, model)
	}
	sort.Strings(models)
	var out strings.Builder
	fmt.Fprintf(&out, "Sessions with a result written in the last %d days, by the model that ran them:\n", days)
	for _, model := range models {
		total := byModel[model]
		perTurn := 0
		if total.turns > 0 {
			perTurn = total.cacheRead / total.turns
		}
		var ended []string
		for subtype, n := range total.subtypes {
			ended = append(ended, fmt.Sprintf("%s %d", subtype, n))
		}
		sort.Strings(ended)
		fmt.Fprintf(&out, "- %s: %d sessions (%d failed), %d turns, cache read %s a turn, %s on the model, %s tokens; ended: %s\n",
			model, total.sessions, total.failed, total.turns, Thousands(perTurn), Clock(total.api), Thousands(total.tokens), strings.Join(ended, ", "))
	}
	out.WriteString("The gate's seconds, what landed and the refusal causes are in the report.\n")
	return out.String()
}
