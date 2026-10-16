package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
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
	if len(report.Listed) == 0 {
		d.say("nothing to digest\n")
		return report, nil
	}

	card := d.card(report.Listed)
	if d.DryRun {
		d.say("dry run: would file %q under %s and send:\n%s\noptions: %s (recommended), %s\n",
			d.title(len(report.Listed)), d.parent(), card.Text, card.Options[0], card.Options[1])
		return report, nil
	}
	id, err := d.Tracker.CreateStory(ctx, NewStory{
		EpicID: d.parent(), Title: d.title(len(report.Listed)), Description: digestDescription(report.Listed),
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
// shows as a chip; its Bead is set once the digest is filed.
func (d Digest) card(listed []DigestEntry) PosternSendRequest {
	lines := make([]string, 0, len(listed))
	for _, e := range listed {
		lines = append(lines, fmt.Sprintf("- %s: %s", e.ID, e.Title))
	}
	text := fmt.Sprintf("Weekly digest: %d factory demos I verified myself.\n%s\nA closes them all; B holds them and I ask which.",
		len(listed), strings.Join(lines, "\n"))
	looksGood := DigestLooksGoodOption(len(listed))
	return PosternSendRequest{
		Class: "decision-needed", Text: text, Recommend: looksGood, Options: []string{looksGood, DigestHoldOption},
	}
}

func (d Digest) title(n int) string {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	return fmt.Sprintf("Weekly digest %s: %d factory demos", now().UTC().Format(time.DateOnly), n)
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
// title and VERIFIED line.
func digestDescription(listed []DigestEntry) string {
	var b strings.Builder
	for _, e := range listed {
		fmt.Fprintf(&b, "- %s: %s\n  %s\n", e.ID, e.Title, e.Verified)
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
