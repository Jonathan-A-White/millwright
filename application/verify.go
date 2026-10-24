package application

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The defaults of the [verify] table, and the names of what a proposal says.
const (
	// DefaultVerifyModel and DefaultVerifyEffort are what the matcher runs on:
	// the cheap model that reads a picture (mw-4xdnis, PROVISIONAL).
	DefaultVerifyModel  = "haiku"
	DefaultVerifyEffort = "medium"
	// DefaultVerifyConfidence is the least confidence at which a match becomes
	// a proposal (PROVISIONAL, the Governor to confirm).
	DefaultVerifyConfidence = 0.8
	// DefaultVerifySince is how far back mw verify match looks for landings
	// and for his messages and pictures.
	DefaultVerifySince = 7 * 24 * time.Hour

	// VerifyProposedWord is the first word of a proposal's comment. It is never
	// VerifiedWord: that first word is the record itself (mw-tcmhmh.1).
	VerifyProposedWord = "PROPOSED"
	// VerifiedWord is the first word of the comment that records a landing as
	// verified by the Governor's use.
	VerifiedWord = "VERIFIED"

	// VerifyKindDemo and VerifyKindLanding say what a candidate is.
	VerifyKindDemo    = "demo"
	VerifyKindLanding = "landing"

	// VerifyJobName signs the mail of a run: verify@<host>.
	VerifyJobName = "verify"
)

// verifyEpicLookback is how far back a closed story of a demo's epic is read
// for its HOW TO CHECK IT, whatever --since says: a demo outlives its landings.
const verifyEpicLookback = 30 * 24 * time.Hour

// howToCheckHeading is the heading of the section of a closing comment that
// tells the Governor how to see a change.
const howToCheckHeading = "HOW TO CHECK IT"

// VerifySettings are the [verify] table's keys; a zero field reads its default.
type VerifySettings struct {
	Model      string
	Effort     string
	Confidence float64
}

func (s VerifySettings) orDefaults() VerifySettings {
	if strings.TrimSpace(s.Model) == "" {
		s.Model = DefaultVerifyModel
	}
	if strings.TrimSpace(s.Effort) == "" {
		s.Effort = DefaultVerifyEffort
	}
	if s.Confidence <= 0 || s.Confidence > 1 {
		s.Confidence = DefaultVerifyConfidence
	}
	return s
}

// ClosedStory is a bead the tracker holds closed, with what the unverified
// landings are told apart by.
type ClosedStory struct {
	ID    string
	Title string
	// Type is the tracker's word for the kind of bead: bug, task, feature.
	Type   string
	EpicID string
	// CloseReason is why the bead was closed, as the tracker has it.
	CloseReason string
	ClosedAt    time.Time
	// Superseded is true when the bead carries a supersedes link: another
	// story landed its work, so nothing landed under this one.
	Superseded bool
}

// VerifyBeads is what mw verify match reads and writes in the tracker. The real
// adapter is infrastructure/beads' Gateway, the fake apptest.FakeVerifyBeads.
type VerifyBeads interface {
	// WorkInHand lists the stories ready and running, the open demos among them.
	WorkInHand(ctx context.Context) (WorkInHand, error)
	// ClosedStories lists the beads closed at since or later, epics left out.
	ClosedStories(ctx context.Context, since time.Time) ([]ClosedStory, error)
	// StoriesComments is the comments of each of the beads, oldest first.
	StoriesComments(ctx context.Context, ids []string) (map[string][]Comment, error)
	// CommentOnStory appends one comment to a bead.
	CommentOnStory(ctx context.Context, id, text string) error
}

// VerifyCandidate is one bead a message or a picture of his may show met: an
// open demo, or a landing nobody has recorded as verified.
type VerifyCandidate struct {
	Bead  string
	Kind  string
	Title string
	// Check is the text that says how to see it working.
	Check string
	// LandedAt is when the change landed: a landing's close time, a demo's the
	// newest close among the stories of its epic. Evidence written before it
	// cannot show the change working. Zero when nothing is known to have landed.
	LandedAt time.Time
}

// firstWord is the first word of a comment's text.
func firstWord(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// verifiedIn reports whether a comment records the bead as verified: one whose
// first word is VERIFIED.
func verifiedIn(comments []Comment) bool {
	for _, c := range comments {
		if firstWord(c.Text) == VerifiedWord {
			return true
		}
	}
	return false
}

// proposedFor reports whether a comment already proposes the bead verified from
// txid: one whose first word is PROPOSED and that names the txid.
func proposedFor(comments []Comment, txid string) bool {
	for _, c := range comments {
		if firstWord(c.Text) == VerifyProposedWord && strings.Contains(c.Text, txid) {
			return true
		}
	}
	return false
}

// landed is whether a closed bead is a landing that can be verified: a bug,
// task or feature that is not a formula step, was not superseded and was not
// closed as never released.
func (c ClosedStory) landed() bool {
	switch c.Type {
	case "bug", "task", "feature":
	default:
		return false
	}
	if strings.HasPrefix(c.ID, "mw-mol") || strings.Contains(c.Title, "State change") {
		return false
	}
	reason := strings.ToLower(strings.TrimSpace(c.CloseReason))
	return !c.Superseded && !strings.Contains(reason, "never released") && !strings.HasPrefix(reason, "superseded")
}

// howToCheckIt is the HOW TO CHECK IT section of the newest comment that has
// one: from its heading to the 'What's new:' or 'For the rig memory:' line, or
// the end. "" when no comment has it.
func howToCheckIt(comments []Comment) string {
	for i := len(comments) - 1; i >= 0; i-- {
		lines := strings.Split(comments[i].Text, "\n")
		start := -1
		for n, line := range lines {
			if strings.Contains(strings.ToUpper(line), howToCheckHeading) {
				start = n
				break
			}
		}
		if start < 0 {
			continue
		}
		end := len(lines)
		for n := start + 1; n < len(lines); n++ {
			line := strings.TrimSpace(lines[n])
			if strings.HasPrefix(line, "What's new:") || strings.HasPrefix(line, "For the rig memory:") {
				end = n
				break
			}
		}
		return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	}
	return ""
}

// VerifyCandidates are the beads the Governor's use may show met: every open
// demo (QueuedDemos, the demos mw stuck --dampers counts) with its description
// and the HOW TO CHECK IT of its epic's closed stories, and every unverified
// landing closed within since with its own HOW TO CHECK IT. A bead with a
// VERIFIED-first comment is none. Demos come first, by id, then landings by
// close time.
func VerifyCandidates(ctx context.Context, beads VerifyBeads, now time.Time, since time.Duration) ([]VerifyCandidate, error) {
	work, err := beads.WorkInHand(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the open demos: %w", err)
	}
	demos := QueuedDemos(work)
	look := max(since, verifyEpicLookback)
	if len(demos) == 0 {
		look = since
	}
	closed, err := beads.ClosedStories(ctx, now.Add(-look))
	if err != nil {
		return nil, fmt.Errorf("reading the landed stories: %w", err)
	}
	var landed []ClosedStory
	var ids []string
	for _, c := range closed {
		if c.landed() {
			landed = append(landed, c)
			ids = append(ids, c.ID)
		}
	}
	for _, d := range demos {
		ids = append(ids, d.Story.ID)
	}
	comments, err := beads.StoriesComments(ctx, uniqueStrings(ids))
	if err != nil {
		return nil, fmt.Errorf("reading the comments of the candidates: %w", err)
	}

	var out []VerifyCandidate
	sort.Slice(demos, func(a, b int) bool { return demos[a].Story.ID < demos[b].Story.ID })
	for _, d := range demos {
		if verifiedIn(comments[d.Story.ID]) {
			continue
		}
		var check strings.Builder
		check.WriteString("The demo's description:\n" + strings.TrimSpace(d.Description) + "\n")
		var landedAt time.Time
		for _, c := range landed {
			if d.EpicID == "" || c.EpicID != d.EpicID || c.ID == d.Story.ID {
				continue
			}
			if c.ClosedAt.After(landedAt) {
				landedAt = c.ClosedAt
			}
			if section := howToCheckIt(comments[c.ID]); section != "" {
				check.WriteString(fmt.Sprintf("\nLanded under the same epic, %s (%s):\n%s\n", c.ID, c.Title, section))
			}
		}
		out = append(out, VerifyCandidate{Bead: d.Story.ID, Kind: VerifyKindDemo, Title: d.Story.Title, Check: check.String(), LandedAt: landedAt})
	}
	sort.SliceStable(landed, func(a, b int) bool { return landed[a].ClosedAt.Before(landed[b].ClosedAt) })
	have := map[string]bool{}
	for _, c := range out {
		have[c.Bead] = true
	}
	for _, c := range landed {
		if have[c.ID] || c.ClosedAt.Before(now.Add(-since)) || verifiedIn(comments[c.ID]) {
			continue
		}
		if section := howToCheckIt(comments[c.ID]); section != "" {
			out = append(out, VerifyCandidate{Bead: c.ID, Kind: VerifyKindLanding, Title: c.Title, Check: section + "\n", LandedAt: c.ClosedAt})
		}
	}
	return out, nil
}

// VerifyEvidence is one message of his, or one picture, or both: the files the
// inbox pass saved under one txid.
type VerifyEvidence struct {
	// Txid is the message's txid, as the record names it: direct:<hash>.
	Txid string
	// At is when the files were written, the newest of them.
	At time.Time
	// Files are the full paths: the .txt of the message and its pictures.
	Files []string
}

// KeptWords is the text of his message txid as the inbox kept it whole
// (<txid>.message.json, PosternKeptFile) in dir; false when none is kept.
func KeptWords(dir, txid string) (string, bool) {
	raw, err := os.ReadFile(PosternKeptFile(dir, txid))
	if err != nil {
		return "", false
	}
	var kept keptMessage
	if json.Unmarshal(raw, &kept) != nil {
		return "", false
	}
	return kept.Text, true
}

// evidenceWords is the words he sent with a piece of evidence that holds a
// picture and no .txt of its own: the text the inbox kept for its txid. A picture
// read without them reads as a screenshot of the thing working, though they may
// ask for it (mw-gq6.742). ok is false when the evidence holds no picture, has a
// .txt of its own, or no words are kept.
func evidenceWords(e VerifyEvidence) (text string, ok bool) {
	picture := false
	for _, path := range e.Files {
		if strings.EqualFold(filepath.Ext(path), ".txt") {
			return "", false
		}
		picture = true
	}
	if !picture {
		return "", false
	}
	text, ok = KeptWords(filepath.Dir(e.Files[0]), e.Txid)
	return strings.TrimSpace(text), ok && strings.TrimSpace(text) != ""
}

// verifyEvidenceExts are the files of the inbox that are evidence: his text and
// his pictures. A voice note, a record's JSON and a transcript are left out.
var verifyEvidenceExts = map[string]bool{".txt": true, ".jpg": true, ".jpeg": true, ".png": true, ".webp": true}

// txidOfStem is the txid a saved file's name stands for: posternFileName made
// the colon of "direct:<hash>" a dash.
func txidOfStem(stem string) string {
	if rest, ok := strings.CutPrefix(stem, "direct-"); ok {
		return "direct:" + rest
	}
	return stem
}

// ReadVerifyEvidence reads the saved messages and pictures of the inbox dir:
// those written at since or later, or, when txids are given, those and no
// others. Newest last. A txid that names no file is an error.
func ReadVerifyEvidence(dir string, since time.Time, txids []string) ([]VerifyEvidence, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			entries = nil
		} else {
			return nil, fmt.Errorf("reading the inbox %s: %w", dir, err)
		}
	}
	wanted := map[string]bool{}
	for _, txid := range txids {
		wanted[txidOfStem(posternFileName(strings.TrimSpace(txid)))] = true
	}
	found := map[string]*VerifyEvidence{}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.IsDir() || !verifyEvidenceExts[ext] {
			continue
		}
		txid := txidOfStem(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		if len(wanted) > 0 && !wanted[txid] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if len(wanted) == 0 && info.ModTime().Before(since) {
			continue
		}
		one := found[txid]
		if one == nil {
			one = &VerifyEvidence{Txid: txid}
			found[txid] = one
		}
		one.Files = append(one.Files, filepath.Join(dir, e.Name()))
		if info.ModTime().After(one.At) {
			one.At = info.ModTime().UTC()
		}
	}
	var out []VerifyEvidence
	for _, one := range found {
		sort.Strings(one.Files)
		out = append(out, *one)
	}
	sort.Slice(out, func(a, b int) bool {
		if !out[a].At.Equal(out[b].At) {
			return out[a].At.Before(out[b].At)
		}
		return out[a].Txid < out[b].Txid
	})
	for txid := range wanted {
		hit := false
		for _, one := range out {
			hit = hit || one.Txid == txid
		}
		if !hit {
			return out, fmt.Errorf("no message or picture of %s is saved in %s", txid, dir)
		}
	}
	return out, nil
}
