package application

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// LabelDigest marks a demo the Mayor has verified himself, after writing
// VERIFIED on it, to go in the Governor's weekly digest (his Q4 A on
// mw-6ww.119).
const LabelDigest = "digest"

// DigestDefaultParent is the bead a weekly digest is filed under when none is
// named.
const DigestDefaultParent = "mw-6ww.119"

// DigestHoldOption is the digest card's second option: it changes nothing,
// and the Mayor asks which demos to hold.
const DigestHoldOption = "B: Hold, I'll name which"

// DigestVerifiedClip is how many characters of a demo's VERIFIED line the
// digest bead's description quotes.
const DigestVerifiedClip = 120

// DigestLooksGoodOption is the digest card's first, recommended option for n
// demos.
func DigestLooksGoodOption(n int) string {
	return fmt.Sprintf("A: Looks good on all %d", n)
}

// Digest is mw digest send: every open, unclaimed demo labelled digest whose
// comments include one beginning VERIFIED goes on a fresh open task bead
// under Parent, and one decision card on that bead asks the Governor to take
// them all with one tap. A labelled demo with no VERIFIED comment is said in
// Out as left out and is in neither the bead nor the card.
type Digest struct {
	Tracker WorkTracker
	// Sender posts the card, as mw postern send --class decision-needed does.
	Sender PosternSender
	// Parent is the bead the digest is filed under; empty is
	// DigestDefaultParent.
	Parent string
	// Loop, when set, is where the card's Factory loop section is read from
	// (mw loop brief's inputs); nil leaves the section out.
	Loop LoopSource
	// DryRun prints the card and files and sends nothing.
	DryRun bool
	// Now dates the digest's title; nil reads the real clock.
	Now func() time.Time
	// Out is where what was done is said; nil says nothing.
	Out io.Writer
}

// DigestEntry is one demo in a digest: its full id, its title and the first
// DigestVerifiedClip characters of its VERIFIED line.
type DigestEntry struct {
	ID       string
	Title    string
	Verified string
}

// DigestReport is what one run of Digest found and did.
type DigestReport struct {
	// Listed are the verified demos the digest names, in the tracker's order.
	Listed []DigestEntry
	// LeftOut are the ids of labelled demos with no VERIFIED comment.
	LeftOut []string
	// Bead is the digest bead filed, and Txid the card sent on it; both empty
	// on a dry run or when there was nothing to digest.
	Bead string
	Txid string
}

// Run lists the labelled demos, files the digest bead and sends its card.
func (d Digest) Run(ctx context.Context) (DigestReport, error) {
	var report DigestReport
	labelled, err := d.Tracker.ReadyWithLabel(ctx, LabelDigest)
	if err != nil {
		return report, fmt.Errorf("listing the demos labelled %s: %w", LabelDigest, err)
	}
	var demos []StoryDetail
	var ids []string
	for _, bead := range labelled {
		if bead.IsEpic || bead.Closed() || isClaimed(bead) || !isDemo(bead) {
			continue
		}
		demos = append(demos, bead)
		ids = append(ids, bead.Story.ID)
	}
	comments := map[string][]Comment{}
	if len(ids) > 0 {
		if comments, err = d.Tracker.StoriesComments(ctx, ids); err != nil {
			return report, fmt.Errorf("reading the comments of the demos labelled %s: %w", LabelDigest, err)
		}
	}
	for _, demo := range demos {
		line, ok := verifiedLine(comments[demo.Story.ID])
		if !ok {
			report.LeftOut = append(report.LeftOut, demo.Story.ID)
			continue
		}
		report.Listed = append(report.Listed, DigestEntry{
			ID: demo.Story.ID, Title: strings.TrimSpace(demo.Story.Title), Verified: clipRunes(line, DigestVerifiedClip),
		})
	}
	for _, id := range report.LeftOut {
		d.say("%s: not verified, left out\n", id)
	}
	section := d.loopSection(ctx)
	if len(report.Listed) == 0 && !section.hasContent() {
		d.say("nothing to digest\n")
		return report, nil
	}

	card := d.card(report.Listed, section)
	if d.DryRun {
		d.say("dry run: would file %q under %s and send:\n%s\noptions: %s (recommended), %s\n",
			d.title(len(report.Listed)), d.parent(), card.Text, card.Options[0], card.Options[1])
		return report, nil
	}
	id, err := d.Tracker.CreateStory(ctx, NewStory{
		EpicID: d.parent(), Title: d.title(len(report.Listed)), Description: digestDescription(report.Listed, section.seen),
	})
	if err != nil {
		return report, fmt.Errorf("filing the weekly digest: %w", err)
	}
	report.Bead = id
	// CreateStory files held; the digest is open the moment it is filed.
	if err := d.Tracker.ReleaseStory(ctx, id); err != nil {
		return report, fmt.Errorf("filed %s but opening it failed: %w", id, err)
	}
	d.say("filed %s: %s\n", id, d.title(len(report.Listed)))
	card.Bead = id
	txid, err := d.Sender.Run(ctx, card)
	if err != nil {
		return report, fmt.Errorf("filed %s but sending its card failed: %w", id, err)
	}
	report.Txid = txid
	d.say("sent the digest card on %s: %s\n", id, txid)
	return report, nil
}

// card is the decision card naming each listed demo by its full id, so each
// shows as a chip, then the Factory loop section; its Bead is set once the
// digest is filed.
func (d Digest) card(listed []DigestEntry, section digestLoop) PosternSendRequest {
	var text string
	if len(listed) == 0 {
		text = "Weekly digest: no factory demos this week.\nA closes this digest; B holds it and I ask."
	} else {
		lines := make([]string, 0, len(listed))
		for _, e := range listed {
			lines = append(lines, fmt.Sprintf("- %s: %s", e.ID, e.Title))
		}
		text = fmt.Sprintf("Weekly digest: %d factory demos I verified myself.\n%s\nA closes them all; B holds them and I ask which.",
			len(listed), strings.Join(lines, "\n"))
	}
	if loop := section.text(); loop != "" {
		text += "\n\n" + loop
	}
	looksGood := DigestLooksGoodOption(len(listed))
	return PosternSendRequest{
		Class: "decision-needed", Text: text, Recommend: looksGood, Options: []string{looksGood, DigestHoldOption},
	}
}

// DigestLoopClip is how many characters of a pick's title and of an outcome's
// lesson the Factory loop section quotes.
const DigestLoopClip = 80

// DigestShareKey begins the line of a digest bead's description that records
// the investment target the digest saw, in percent: "invest_share: 33%".
const DigestShareKey = "invest_share: "

// digestLoop is the card's Factory loop section, laid out from the loop's
// inputs. The zero value is a section not read.
type digestLoop struct {
	read                   bool
	picks, verdicts, share []string
	// seen is the target in percent as the digest bead's description records
	// it; empty when the report carried no share.
	seen string
}

// hasContent says the section has a pick or a verdict: the share alone does not
// make a week worth a card.
func (s digestLoop) hasContent() bool { return len(s.picks) > 0 || len(s.verdicts) > 0 }

// text is the section under the heading "Factory loop"; empty when not read.
func (s digestLoop) text() string {
	if !s.read {
		return ""
	}
	var b strings.Builder
	b.WriteString("Factory loop\n")
	part := func(heading string, lines []string, none string) {
		b.WriteString(heading + "\n")
		if len(lines) == 0 {
			lines = []string{"  " + none}
		}
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
	part("PICKS", s.picks, "none filed this week")
	part("VERDICTS", s.verdicts, "none reached 14 days or recorded this week")
	part("SHARE", s.share, "no share read")
	return strings.TrimRight(b.String(), "\n")
}

// loopSection reads the Factory loop section from d.Loop. A loop that cannot be
// read is said in Out and leaves the section out, so that the demos' card still
// goes out.
func (d Digest) loopSection(ctx context.Context) digestLoop {
	if d.Loop == nil {
		return digestLoop{}
	}
	in, err := d.Loop.Gather(ctx)
	if err != nil {
		d.say("the factory loop section is left out: %v\n", err)
		return digestLoop{}
	}
	brief := ReadLoopBrief(in)
	r := in.Result
	s := digestLoop{read: true}

	picks := append([]StoryDetail(nil), in.Picks...)
	sort.SliceStable(picks, func(i, j int) bool { return picks[i].Created.Before(picks[j].Created) })
	for _, p := range picks {
		if !r.inWindowDay(p.Created.UTC().Format(LedgerDate)) {
			continue
		}
		expect := "no Expect line"
		if lines := domain.LinesBeginning(p.Acceptance, "Expect"); len(lines) > 0 {
			expect = "Expect: " + strings.TrimSpace(lines[0])
		}
		s.picks = append(s.picks, fmt.Sprintf("- %s: %s — %s", p.Story.ID, clipRunes(strings.TrimSpace(p.Story.Title), DigestLoopClip), expect))
	}

	for _, c := range brief.Verdicts.Changes {
		if c.Verdict == ChangeKept || c.Verdict == ChangeWorse {
			s.verdicts = append(s.verdicts, fmt.Sprintf("- %s %s (%s)", c.Verdict, clipRunes(strings.TrimSpace(c.Title), DigestLoopClip), c.Story))
		}
	}
	for _, inv := range in.Investments {
		for _, o := range inv.Outcomes {
			if r.inWindowDay(o.At.UTC().Format(LedgerDate)) {
				s.verdicts = append(s.verdicts, fmt.Sprintf("- %s %s: %s", o.Bead, o.Result,
					clipRunes(strings.Join(strings.Fields(o.Lesson), " "), DigestLoopClip)))
			}
		}
	}

	for _, line := range brief.Share.Lines {
		s.share = append(s.share, "  "+strings.TrimSpace(line))
	}
	if brief.Share.Bound != "" {
		s.share = append(s.share, "  "+brief.Share.Bound)
	}
	if r.InvestShare != nil {
		s.seen = percentText(r.InvestShare.Target)
		if was, recorded := d.previousShare(ctx); recorded && math.Abs(was-r.InvestShare.Target) > 0.05 {
			s.share = append(s.share, fmt.Sprintf("  moved to %s on %s", s.seen, d.now().UTC().Format(time.DateOnly)))
		}
	}
	return s
}

// previousShare is the investment target the newest weekly digest bead under
// the parent recorded in its description, and whether it recorded one.
func (d Digest) previousShare(ctx context.Context) (float64, bool) {
	epic, err := d.Tracker.ShowEpic(ctx, d.parent())
	if err != nil {
		d.say("the share the previous digest saw is not known: %v\n", err)
		return 0, false
	}
	var newest *StoryDetail
	for i, bead := range epic.Stories {
		if bead.IsEpic || !isDigest(bead) {
			continue
		}
		if newest == nil || !bead.Created.Before(newest.Created) {
			newest = &epic.Stories[i]
		}
	}
	if newest == nil {
		return 0, false
	}
	for _, line := range strings.Split(newest.Description, "\n") {
		if rest, ok := strings.CutPrefix(line, DigestShareKey); ok {
			v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(rest), "%"), 64)
			return v, err == nil
		}
	}
	return 0, false
}

func (d Digest) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Digest) title(n int) string {
	return fmt.Sprintf("Weekly digest %s: %d factory demos", d.now().UTC().Format(time.DateOnly), n)
}

func (d Digest) parent() string {
	if p := strings.TrimSpace(d.Parent); p != "" {
		return p
	}
	return DigestDefaultParent
}

func (d Digest) say(format string, args ...any) {
	if d.Out != nil {
		fmt.Fprintf(d.Out, format, args...)
	}
}

// digestDescription is the digest bead's description: each demo's full id,
// title and VERIFIED line, then the investment target the digest saw (seen, in
// percent) when there is one, for the next digest to compare. The line is not
// a listed demo (digestListed).
func digestDescription(listed []DigestEntry, seen string) string {
	var b strings.Builder
	for _, e := range listed {
		fmt.Fprintf(&b, "- %s: %s\n  %s\n", e.ID, e.Title, e.Verified)
	}
	if seen != "" {
		b.WriteString(DigestShareKey + seen + "\n")
	}
	return b.String()
}

// verifiedLine is the first line of the first comment beginning VERIFIED.
func verifiedLine(comments []Comment) (string, bool) {
	for _, c := range comments {
		if commentMarksVerified(c.Text) {
			line, _, _ := strings.Cut(strings.TrimSpace(c.Text), "\n")
			return strings.TrimSpace(line), true
		}
	}
	return "", false
}

// DigestTitlePrefix begins the title of every weekly digest bead.
const DigestTitlePrefix = "Weekly digest"

// isDigest reports whether bead is a weekly digest bead, as Digest files it.
func isDigest(bead StoryDetail) bool {
	return strings.HasPrefix(strings.TrimSpace(bead.Story.Title), DigestTitlePrefix)
}

// digestListed is the ids of the demos a digest bead's description lists, in
// order: the first word after the "- " that begins a line, as digestDescription
// writes it.
func digestListed(description string) []string {
	var ids []string
	for _, line := range strings.Split(description, "\n") {
		rest, ok := strings.CutPrefix(line, "- ")
		if !ok {
			continue
		}
		if id, _, found := strings.Cut(rest, ":"); found && strings.TrimSpace(id) != "" && !strings.ContainsAny(id, " \t") {
			ids = append(ids, id)
		}
	}
	return ids
}

// DigestJobName is the follower job that sends the weekly digest: its events'
// actor is digest@<host>, and only the home sends.
const DigestJobName = "digest"

// DigestWeekday and DigestHour are when, in UTC, the weekly digest is due:
// Monday from 11:00. PROVISIONAL, until the Governor names the hour.
const (
	DigestWeekday = time.Monday
	DigestHour    = 11
)

// DigestLastKey is the note the follower keeps the UTC day it last sent the
// digest on, so a restart on the same Monday does not send it twice.
const DigestLastKey = "digest.weekly.last"

// DigestWeekly is the follower's digest job: on DigestWeekday, from
// DigestHour UTC, once for each day, on the home only, it runs Send (mw digest
// send). The day is marked before Send runs, so a pass that dies half-way
// never files two digests.
type DigestWeekly struct {
	// Notes keeps the day of the last send.
	Notes TodoNightlyNotes
	// Home says whether this host is the home: only the home sends.
	Home func(ctx context.Context) (bool, error)
	// Send is mw digest send.
	Send func(ctx context.Context) error
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Err is where a pass passed over is said. A nil Err says nothing.
	Err io.Writer

	mu    sync.Mutex
	tried string // the UTC day the digest was last begun
}

func (w *DigestWeekly) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

// Job is the follower's job: no event springs it; every pass asks whether the
// digest is due.
func (w *DigestWeekly) Job() SpringJob {
	return SpringJob{
		Name: DigestJobName,
		// A failed week's digest is told to the Mayor (mw-gq6.531).
		TellFailure: true,
		Probe: func(ctx context.Context) (string, bool) {
			if w.due(ctx) {
				return ClockReason, true
			}
			return "", false
		},
		Run: w.run,
	}
}

// due reports whether the digest is to be sent now: it is DigestWeekday from
// DigestHour, none has been sent today, and this is the home. The home is
// asked last, for it reads a file in the vault.
func (w *DigestWeekly) due(ctx context.Context) bool {
	now := w.now()
	if now.Weekday() != DigestWeekday || now.Hour() < DigestHour {
		return false
	}
	day := now.Format(time.DateOnly)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.tried == day {
		return false
	}
	last, err := w.Notes.Note(ctx, DigestLastKey)
	if err != nil {
		w.say("mw events follow: reading the day of the last weekly digest: %v\n", err)
		return false
	}
	if strings.TrimSpace(last) == day {
		w.tried = day
		return false
	}
	at, err := w.Home(ctx)
	return err == nil && at
}

// run sends the digest if it is due, marking the day first.
func (w *DigestWeekly) run(ctx context.Context) error {
	if !w.due(ctx) {
		return nil
	}
	day := w.now().Format(time.DateOnly)
	w.mu.Lock()
	w.tried = day
	w.mu.Unlock()
	if err := w.Notes.SetNote(ctx, DigestLastKey, day); err != nil {
		return fmt.Errorf("marking the day of the weekly digest: %w", err)
	}
	return w.Send(ctx)
}

func (w *DigestWeekly) say(format string, args ...any) {
	if w.Err != nil {
		fmt.Fprintf(w.Err, format, args...)
	}
}
