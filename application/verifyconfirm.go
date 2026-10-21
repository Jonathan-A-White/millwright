package application

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// verifyListLookback is how far back mw verify list looks for landings with a
// proposal waiting; an open demo is listed whatever its age.
const verifyListLookback = 14 * 24 * time.Hour

// VerifyConfirmBeads is what mw verify confirm and mw verify list read and write
// in the tracker: what mw verify match uses, and a bead's own record and its
// close. The real adapter is infrastructure/beads' Gateway, the fake
// apptest.FakeVerifyBeads. It is a TidyBeads too, so that a card is cleared
// through PosternClear.
type VerifyConfirmBeads interface {
	VerifyBeads
	// ShowBeads reads beads whole; an id the tracker has no record of is left out.
	ShowBeads(ctx context.Context, ids []string) ([]StoryDetail, error)
	// StoryComments lists the comments of one bead, oldest first.
	StoryComments(ctx context.Context, id string) ([]Comment, error)
	// CloseStory closes a bead with the reason it was closed for.
	CloseStory(ctx context.Context, id, reason string) error
}

// VerifiedComment is the standing form of the record that a landing was verified
// by the Governor's use (mw-tcmhmh.1): its first word is VERIFIED, which is what
// every reader of the tracker looks for. by is Mayor or Deputy; how is what the
// inference came from, "proposed by mw verify match, run <id>" or "by hand".
// It is the one place the form is written.
func VerifiedComment(by, txid, shows, how string) string {
	return fmt.Sprintf("%s by the Governor's use, inferred by the %s from %s: %s (%s)", VerifiedWord, by, txid, shows, how)
}

// verifyProposalLine reads a comment mw verify match wrote:
// PROPOSED VERIFIED by the Governor's use, from <txid> (<time>): <shows> (confidence <c>, <model>, run <id>)
var verifyProposalLine = regexp.MustCompile(`(?s)^` + VerifyProposedWord + ` ` + VerifiedWord +
	` by the Governor's use, from (\S+) \(([^)]*)\): (.*) \(confidence ([0-9.]+), ([^,]*), run ([^)]*)\)\s*$`)

// verifyProposed is one proposal as its comment holds it.
type verifyProposed struct {
	Txid, At, Shows, Run string
	Confidence           float64
}

// proposalsIn is the proposals the comments hold, oldest first. A comment that
// opens PROPOSED but is not in the form mw verify match writes is left out.
func proposalsIn(comments []Comment) []verifyProposed {
	var out []verifyProposed
	for _, c := range comments {
		if firstWord(c.Text) != VerifyProposedWord {
			continue
		}
		if m := verifyProposalLine.FindStringSubmatch(c.Text); m != nil {
			conf, _ := strconv.ParseFloat(m[4], 64)
			out = append(out, verifyProposed{Txid: m[1], At: m[2], Shows: m[3], Confidence: conf, Run: strings.TrimSpace(m[6])})
		}
	}
	return out
}

// proposalFor is the newest proposal in the comments that names txid.
func proposalFor(comments []Comment, txid string) (verifyProposed, bool) {
	found := proposalsIn(comments)
	for i := len(found) - 1; i >= 0; i-- {
		if found[i].Txid == txid {
			return found[i], true
		}
	}
	return verifyProposed{}, false
}

// ownWordPattern finds what only the Governor says yes to: money, keys, secrets
// and a charter. It reads a demo's title and description and its epic's.
var ownWordPattern = regexp.MustCompile(`(?i)\b(money|charters?|secrets?|credentials?|(?:api|ssh|private|signing|wallet) keys?|keys)\b`)

// asksHisOwnWord is what in the texts makes a demo his to confirm, or "".
func asksHisOwnWord(texts ...string) string {
	for _, text := range texts {
		if m := ownWordPattern.FindString(text); m != "" {
			return strings.ToLower(m)
		}
	}
	return ""
}

// ownWordOf is what makes a demo his to confirm: found in its own title and
// description or in its epic's, or "".
func ownWordOf(ctx context.Context, beads VerifyConfirmBeads, demo StoryDetail, epics map[string]string) (string, error) {
	texts := []string{demo.Story.Title, demo.Description}
	if demo.EpicID != "" {
		epic, seen := epics[demo.EpicID]
		if !seen {
			found, err := beads.ShowBeads(ctx, []string{demo.EpicID})
			if err != nil {
				return "", fmt.Errorf("reading %s's epic %s: %w", demo.Story.ID, demo.EpicID, err)
			}
			for _, e := range found {
				epic += e.Story.Title + "\n" + e.Description
			}
			epics[demo.EpicID] = epic
		}
		texts = append(texts, epic)
	}
	return asksHisOwnWord(texts...), nil
}

func ownWordRefusal(bead, word string) string {
	return fmt.Sprintf("%s asks his own word (it touches %q: money, keys or a charter), so it is listed and never confirmed by a seat; put it on a card to him", bead, word)
}

// VerifyConfirmRequest is what mw verify confirm is asked.
type VerifyConfirmRequest struct {
	Bead string
	Txid string
	// As is who infers it: mayor or deputy; empty is mayor.
	As string
	// Shows, when given, is what the evidence shows; without a PROPOSED comment
	// for the txid it is required, and the record says it was by hand.
	Shows string
}

// VerifyConfirm is mw verify confirm: it writes the VERIFIED comment for a
// bead from a PROPOSED comment for the txid, closes a demo on it and clears its
// card. No agent is called.
type VerifyConfirm struct {
	Beads  VerifyConfirmBeads
	Notes  PosternNotes
	Sender PosternSender
	// Out is where each act is said. A nil Out says nothing.
	Out io.Writer
}

func (c VerifyConfirm) printf(format string, args ...any) {
	if c.Out != nil {
		fmt.Fprintf(c.Out, format, args...)
	}
}

// verifyAs reads --as.
func verifyAs(as string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(as)) {
	case "", "mayor":
		return "Mayor", nil
	case "deputy":
		return "Deputy", nil
	}
	return "", fmt.Errorf("--as %q: say mayor or deputy", as)
}

// Run confirms the bead. It is refused, writing nothing, when the bead is
// unknown or already carries a VERIFIED comment, when no PROPOSED comment names
// the txid and no --shows is given, and when the bead is a demo whose epic asks
// his own word. A demo that is open or deferred is closed on the comment and
// its card cleared; any other bead gets the comment only.
func (c VerifyConfirm) Run(ctx context.Context, req VerifyConfirmRequest) error {
	by, err := verifyAs(req.As)
	if err != nil {
		return fmt.Errorf("mw verify confirm: %w", err)
	}
	req.Bead, req.Txid = strings.TrimSpace(req.Bead), strings.TrimSpace(req.Txid)
	if req.Bead == "" || req.Txid == "" {
		return fmt.Errorf("mw verify confirm: say the bead and the evidence: mw verify confirm <bead> --txid <txid>")
	}
	found, err := c.Beads.ShowBeads(ctx, []string{req.Bead})
	if err != nil {
		return fmt.Errorf("mw verify confirm: reading %s: %w", req.Bead, err)
	}
	if len(found) != 1 {
		return fmt.Errorf("mw verify confirm: the tracker has no bead %s", req.Bead)
	}
	bead := found[0]
	all, err := c.Beads.StoriesComments(ctx, []string{req.Bead})
	if err != nil {
		return fmt.Errorf("mw verify confirm: reading %s's comments: %w", req.Bead, err)
	}
	comments := all[req.Bead]
	if verifiedIn(comments) {
		return fmt.Errorf("mw verify confirm: %s already has a comment opening %s, so there is nothing to confirm", req.Bead, VerifiedWord)
	}
	shows, how := strings.Join(strings.Fields(req.Shows), " "), "by hand"
	proposal, proposed := proposalFor(comments, req.Txid)
	switch {
	case proposed:
		how = "proposed by mw verify match, run " + proposal.Run
		if shows == "" {
			shows = proposal.Shows
		}
	case shows == "" && proposedFor(comments, req.Txid):
		return fmt.Errorf("mw verify confirm: the %s comment on %s for %s is not in the form mw verify match writes, so say what it shows with --shows \"<text>\"", VerifyProposedWord, req.Bead, req.Txid)
	case shows == "":
		return fmt.Errorf("mw verify confirm: %s has no %s comment for %s; if you have seen it shown, say what with --shows \"<text>\" and it is recorded by hand", req.Bead, VerifyProposedWord, req.Txid)
	}

	demo := hasLabel(bead.Labels, LabelDemo) && (strings.EqualFold(bead.Status, StatusOpen) || strings.EqualFold(bead.Status, StatusHeld))
	if demo {
		word, err := ownWordOf(ctx, c.Beads, bead, map[string]string{})
		if err != nil {
			return fmt.Errorf("mw verify confirm: %w", err)
		}
		if word != "" {
			return fmt.Errorf("mw verify confirm: %s", ownWordRefusal(req.Bead, word))
		}
	}

	text := VerifiedComment(by, req.Txid, shows, how)
	if err := c.Beads.CommentOnStory(ctx, req.Bead, text); err != nil {
		return fmt.Errorf("mw verify confirm: commenting on %s: %w", req.Bead, err)
	}
	c.printf("commented on %s: %s\n", req.Bead, text)
	if !demo {
		c.printf("left %s as it is (%s, not an open or deferred demo)\n", req.Bead, bead.Status)
		return nil
	}
	if err := c.Beads.CloseStory(ctx, req.Bead, text); err != nil {
		return fmt.Errorf("mw verify confirm: %s is commented VERIFIED, but closing it failed: %w", req.Bead, err)
	}
	c.printf("closed %s\n", req.Bead)
	return c.clearCard(ctx, req)
}

// clearCard clears the demo's card the way mw postern clear does, with one reply
// under it; a demo with no card has nothing to clear.
func (c VerifyConfirm) clearCard(ctx context.Context, req VerifyConfirmRequest) error {
	if c.Notes == nil {
		return nil
	}
	note, err := c.Notes.Note(ctx, PosternQuestionKey(req.Bead))
	if err != nil {
		return fmt.Errorf("mw verify confirm: %s is closed, but reading its card failed: %w", req.Bead, err)
	}
	if strings.TrimSpace(note) == "" {
		c.printf("%s has no card to clear\n", req.Bead)
		return nil
	}
	return PosternClear{Beads: c.Beads, Notes: c.Notes, Sender: c.Sender, Out: c.Out}.Run(ctx, req.Bead, "his use of it shows it works ("+req.Txid+")")
}

// VerifyWaiting is one proposal waiting to be confirmed.
type VerifyWaiting struct {
	Bead  string
	Kind  string
	Title string
	Txid  string
	At    string
	Shows string
	// Confidence is how sure the matcher was.
	Confidence float64
	// NeedsHisWord is why a seat may not confirm it, or "".
	NeedsHisWord string
}

// VerifyList is mw verify list: the proposals mw verify match left that nobody
// has confirmed. No agent is called.
type VerifyList struct {
	Beads VerifyConfirmBeads
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Out is where the list is printed. A nil Out prints nothing.
	Out io.Writer
}

// Run lists, and prints, every open demo and every landing of the last 14 days
// that has a PROPOSED comment and no VERIFIED one: one entry for each proposal,
// demos first, by id, then landings by close time.
func (l VerifyList) Run(ctx context.Context) ([]VerifyWaiting, error) {
	now := time.Now()
	if l.Now != nil {
		now = l.Now()
	}
	work, err := l.Beads.WorkInHand(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the open demos: %w", err)
	}
	demos := QueuedDemos(work)
	sort.Slice(demos, func(a, b int) bool { return demos[a].Story.ID < demos[b].Story.ID })
	closed, err := l.Beads.ClosedStories(ctx, now.Add(-verifyListLookback))
	if err != nil {
		return nil, fmt.Errorf("reading the landed stories: %w", err)
	}
	var landed []ClosedStory
	for _, c := range closed {
		if c.landed() {
			landed = append(landed, c)
		}
	}
	sort.SliceStable(landed, func(a, b int) bool { return landed[a].ClosedAt.Before(landed[b].ClosedAt) })
	var ids []string
	for _, d := range demos {
		ids = append(ids, d.Story.ID)
	}
	for _, c := range landed {
		ids = append(ids, c.ID)
	}
	comments, err := l.Beads.StoriesComments(ctx, uniqueStrings(ids))
	if err != nil {
		return nil, fmt.Errorf("reading the comments of the beads: %w", err)
	}

	var waiting []VerifyWaiting
	add := func(bead, kind, title, refusal string) {
		if verifiedIn(comments[bead]) {
			return
		}
		for _, p := range proposalsIn(comments[bead]) {
			waiting = append(waiting, VerifyWaiting{Bead: bead, Kind: kind, Title: title, Txid: p.Txid, At: p.At, Shows: p.Shows, Confidence: p.Confidence, NeedsHisWord: refusal})
		}
	}
	epics := map[string]string{}
	for _, d := range demos {
		refusal := ""
		if len(proposalsIn(comments[d.Story.ID])) > 0 && !verifiedIn(comments[d.Story.ID]) {
			word, err := ownWordOf(ctx, l.Beads, d, epics)
			if err != nil {
				return nil, err
			}
			if word != "" {
				refusal = ownWordRefusal(d.Story.ID, word)
			}
		}
		add(d.Story.ID, VerifyKindDemo, d.Story.Title, refusal)
	}
	have := map[string]bool{}
	for _, d := range demos {
		have[d.Story.ID] = true
	}
	for _, c := range landed {
		if !have[c.ID] {
			add(c.ID, VerifyKindLanding, c.Title, "")
		}
	}
	l.print(waiting)
	return waiting, nil
}

func (l VerifyList) print(waiting []VerifyWaiting) {
	if l.Out == nil {
		return
	}
	if len(waiting) == 0 {
		fmt.Fprintln(l.Out, "No proposals waiting.")
		return
	}
	for _, w := range waiting {
		fmt.Fprintf(l.Out, "%s  %s  %s\n  %s (%s): %s (confidence %.2f)\n", w.Bead, w.Kind, w.Title, w.Txid, w.At, w.Shows, w.Confidence)
		if w.NeedsHisWord != "" {
			fmt.Fprintf(l.Out, "  needs his word: %s\n", w.NeedsHisWord)
			continue
		}
		fmt.Fprintf(l.Out, "  confirm: mw verify confirm %s --txid %s\n", w.Bead, w.Txid)
	}
	fmt.Fprintf(l.Out, "\n%d proposals waiting.\n", len(waiting))
}
