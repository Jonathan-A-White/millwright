package application

import (
	"fmt"
	"strings"
)

// sleepWeeklyBudgets is the most bytes of each part of the Sunday bundle, set
// beside the nightly ones (sleepBudgets), which are left as they are
// (mw-gq6.1344). The Sunday pass reads everything and is asked for the maps
// sweep, the investments ledger and the week's pictures, and the nightly cut
// dropped what they need: 292 of 811 beads with no titles, an unreadable diff,
// the report's tail, 612 of 1,046 queue lines, the newest messages and 12 of 25
// pictures. A part is larger than the Read tool gives in one turn only where the
// week needs it (the dump), and SleepWeeklyMaxTurns pays for the extra turns.
var sleepWeeklyBudgets = map[string]int{
	// 811 beads with a title each come to about 200,000 bytes.
	SleepTriageFile: 200_000,
	SleepDiffFile:   30_000,
	SleepQueueFile:  48_000,
	SleepGitFile:    32_000,
	SleepRunsFile:   4_000,
	SleepReportFile: 48_000,
	SleepFoldsFile:  24_000,
	SleepStuckFile:  12_000,
	// The review queue cuts itself to its budget by items (NeedsYouFacts.TextCut);
	// the week asks nothing more of it.
	SleepNeedsYouFile: 30_000,
	// A week of his messages, 60 at most, each cut to sleepEvidenceText, and the
	// check text of a week's demos and landings.
	SleepEvidenceFile:   64_000,
	SleepCandidatesFile: 40_000,
}

// SleepWeeklyTotalBudget is the most bytes of the whole Sunday bundle: the sum of
// the weekly budgets, about 133,000 tokens.
const SleepWeeklyTotalBudget = 532_000

// The turns the pass is given, and what a pass of each kind needs of them: a read
// of each part, a further read for each page of a part longer than the Read tool
// gives at once, a read of each picture, and the answer.
const (
	// SleepWeeklyMaxTurns is the Sunday pass's turn cap: the nightly cap and room
	// for the week's pictures and the pages of its larger parts (mw-gq6.1344).
	SleepWeeklyMaxTurns = 60
	// sleepReadBytes is about what one Read gives of a file, 20,000 tokens.
	sleepReadBytes = 80_000
	// sleepWeeklyMaxPictures is the most pictures the Sunday pass is given, the
	// newest, and sleepWeeklyMaxEvidence the most messages and pictures it reads:
	// the week of 2026-11-15 held 25 pictures and a message with each of many, past
	// the 30 a nightly pass (verifyMaxEvidence) reads.
	sleepWeeklyMaxPictures = 40
	sleepWeeklyMaxEvidence = 60
)

// SleepBudgetFor is the most bytes of the part held in file on the Sunday pass
// (deep) or a nightly one.
func SleepBudgetFor(deep bool, file string) int {
	if deep {
		return sleepWeeklyBudgets[file]
	}
	return sleepBudgets[file]
}

// SleepTurnsFor is the agent's turn cap on the Sunday pass (deep) or a nightly one.
func SleepTurnsFor(deep bool) int {
	if deep {
		return SleepWeeklyMaxTurns
	}
	return SleepMaxTurns
}

// SleepMaxPicturesFor is the most pictures the pass gives the agent.
func SleepMaxPicturesFor(deep bool) int {
	if deep {
		return sleepWeeklyMaxPictures
	}
	return sleepMaxPictures
}

// SleepMaxEvidenceFor is the most messages and pictures of his the pass reads.
func SleepMaxEvidenceFor(deep bool) int {
	if deep {
		return sleepWeeklyMaxEvidence
	}
	return verifyMaxEvidence
}

// SleepWorstTurns is the turns a pass of the kind needs when every part is at its
// budget and the most pictures are given.
func SleepWorstTurns(deep bool) int {
	turns := 1 // the answer
	for file := range sleepBudgets {
		turns += readsOf(SleepBudgetFor(deep, file))
	}
	return turns + SleepMaxPicturesFor(deep)
}

// readsOf is how many reads a text of size bytes takes, at least one.
func readsOf(size int) int {
	return max(1, (size+sleepReadBytes-1)/sleepReadBytes)
}

// Turns is the line the dry run prints under the sizes: the turns the bundle
// takes the agent to read, against the turn cap.
func (b SleepBundle) Turns(deep bool) string {
	reads, pictures := 0, 0
	for _, part := range b {
		reads += readsOf(len(part.Text))
		pictures += len(part.Attach)
	}
	need := reads + pictures + 1
	return fmt.Sprintf("  %-14s %-20s %d of %d turns (%d %s of the parts, %d %s, and the answer)\n",
		"turns", "", need, SleepTurnsFor(deep), reads, plural(reads, "read", "reads"), pictures, plural(pictures, "picture", "pictures"))
}

// summariseDiff is the diff as a person reads it when the whole list does not fit
// the budget: a line of counts, then each list's ids a line of many, the moved
// beads a line each, every list given as much of the room as it wants up to an
// equal share (waterFill), and a line that says how many ids it left out. It
// is never a cut through a line. It reports what it did in note.
func summariseDiff(d TriageDiff, budget int) (text, note string) {
	type list struct {
		heading string
		items   []string
		lines   bool
	}
	var moved []string
	for _, m := range d.Moved {
		var changes []string
		for _, c := range m.Changes {
			changes = append(changes, fmt.Sprintf("%s %s -> %s", c.Field, c.From, c.To))
		}
		moved = append(moved, fmt.Sprintf("  %s: %s\n", m.ID, strings.Join(changes, "; ")))
	}
	lists := []list{
		{"Moved", moved, true}, {"Newly ready", d.NewlyReady, false}, {"Newly stale", d.NewlyStale, false},
		{"New", d.New, false}, {"Closed", d.Closed, false},
	}
	counts := fmt.Sprintf("Counts: New %d, Closed %d, Moved %d, Newly ready %d, Newly stale %d\n",
		len(d.New), len(d.Closed), len(d.Moved), len(d.NewlyReady), len(d.NewlyStale))
	head := "(mw summarised this part: the whole list did not fit; ids are in lines, titles are in triage.jsonl)\n" + counts

	// render is a list's heading and as many of its lines as fit within room bytes.
	render := func(l list, room int) (string, int) {
		type line struct {
			text string
			ids  int
		}
		var rows []line
		if l.lines {
			for _, item := range l.items {
				rows = append(rows, line{item, 1})
			}
		} else {
			var cur []string
			size := 0
			flush := func() {
				if len(cur) > 0 {
					rows = append(rows, line{"  " + strings.Join(cur, " ") + "\n", len(cur)})
					cur, size = nil, 0
				}
			}
			for _, item := range l.items {
				if size+len(item)+1 > 100 {
					flush()
				}
				cur = append(cur, item)
				size += len(item) + 1
			}
			flush()
		}
		var out strings.Builder
		fmt.Fprintf(&out, "%s (%d)\n", l.heading, len(l.items))
		const rest = 80 // for the line that says what was left out
		left := 0
		for i, row := range rows {
			if out.Len()+len(row.text)+rest > room && i > 0 || out.Len()+len(row.text) > room {
				for _, r := range rows[i:] {
					left += r.ids
				}
				break
			}
			out.WriteString(row.text)
		}
		if left > 0 {
			fmt.Fprintf(&out, "  (%d more left out to fit)\n", left)
		}
		return out.String(), left
	}

	var wants []int
	var shown []int
	for n, l := range lists {
		if len(l.items) == 0 {
			continue
		}
		whole, _ := render(l, 1<<30)
		wants = append(wants, len(whole))
		shown = append(shown, n)
	}
	shares := waterFill(budget-len(head), wants)
	var out strings.Builder
	out.WriteString(head)
	var left []string
	for k, n := range shown {
		part, cut := render(lists[n], shares[k])
		out.WriteString(part)
		if cut > 0 {
			left = append(left, fmt.Sprintf("%d %s ids", cut, strings.ToLower(lists[n].heading)))
		}
	}
	note = "the diff was too long to list whole and is summarised: counts first, then the ids"
	if len(left) > 0 {
		note += "; left out: " + strings.Join(left, ", ")
	}
	return out.String(), note
}

// reportKept are the report's sections a pass reads whole, whatever else is cut
// to fit: fuel by rig, model and host, the seats, his time, the models, the
// investments with how they measured, and the cheap wins. The report's text puts
// the long sections (quality, speed, the factory's changes) around them.
var reportKept = []string{"FUEL", "SEATS", "HIS TIME", "MODELS", "INVESTMENTS", "CHEAP WINS"}

// reportHeadings are the headings of mw report's text form, at the start of a line.
var reportHeadings = append([]string{"QUALITY", "SPEED", "FACTORY CHANGES"}, reportKept...)

// reportHeading is the heading a line of mw report opens, or "".
func reportHeading(line string) string {
	for _, h := range reportHeadings {
		if line == h || strings.HasPrefix(line, h+" ") || strings.HasPrefix(line, h+":") {
			return h
		}
	}
	return ""
}

// cutReport is the report cut to at most budget bytes by sections: the sections a
// pass reads whole (reportKept) stay as they are, and the others are cut to the
// room that is left, each to a share of it by waterFill, a head of whole lines
// and a line that says it. A report whose kept sections do not fit the budget is
// cut as any text is (cutLines). A cut from the start only would drop the end of
// the report, where the investments and the cheap wins are (mw-gq6.1344).
func cutReport(text string, budget int) (cut, note string) {
	if len(text) <= budget {
		return text, ""
	}
	type section struct {
		heading, text string
	}
	var sections []section
	for _, line := range strings.SplitAfter(text, "\n") {
		if h := reportHeading(strings.TrimRight(line, "\n")); h != "" || len(sections) == 0 {
			sections = append(sections, section{heading: h})
		}
		sections[len(sections)-1].text += line
	}
	kept := func(s section) bool {
		for _, k := range reportKept {
			if s.heading == k {
				return true
			}
		}
		return false
	}
	fixed := 0
	var wants []int
	for _, s := range sections {
		if kept(s) {
			fixed += len(s.text)
		} else {
			wants = append(wants, len(s.text))
		}
	}
	// room for the line of each cut section's own note
	pool := budget - fixed
	if fixed == 0 || len(wants) == 0 || pool < 200*len(wants) {
		return cutLines(text, budget)
	}
	shares := waterFill(pool, wants)
	var out strings.Builder
	var notes []string
	next := 0
	for _, s := range sections {
		if kept(s) {
			out.WriteString(s.text)
			continue
		}
		part, why := cutLines(s.text, shares[next])
		next++
		out.WriteString(part)
		if why != "" {
			name := s.heading
			if name == "" {
				name = "the start"
			}
			notes = append(notes, name+" "+why)
		}
	}
	return out.String(), strings.Join(notes, "; ")
}
