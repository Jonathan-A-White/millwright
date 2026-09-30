package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

// PosternSnapshotWindow is how recently a child must have closed to appear in
// a snapshot's landed group: postern's docs/protocol.md §7, the Governor's
// decision (2) on mw-hy6f4 (2026-09-26) — the last day only, closed_count
// keeping the running total regardless.
const PosternSnapshotWindow = 24 * time.Hour

// PosternSnapshotTextLimit is the most runes of a description or a comment's
// text a needs_you, working or landed entry carries: postern's
// docs/protocol.md §7, the Governor's decision (2) on mw-hy6f4. Past it the
// text is cut, with a trailing marker, the same way status.go's clippedTo
// marks a line cut short.
const PosternSnapshotTextLimit = 4000

// PosternSnapshotCommentLimit is the most of a bead's newest comments a
// needs_you, working or landed entry carries, newest first: postern's
// docs/protocol.md §7, the Governor's decision (2) on mw-hy6f4.
const PosternSnapshotCommentLimit = 3

// PosternSnapshotVerifiedMarker is the word the Mayor's own comments open with
// once a landing has been checked out by hand (docs/codemap.md says so). A
// closed child with a comment that begins with it is not landed: somebody has
// already looked. A comment that merely contains it, a Builder's "VERIFIED by
// running make test", or "NOT VERIFIED", is no mark (mw-gq6.152).
const PosternSnapshotVerifiedMarker = "VERIFIED"

// commentMarksVerified reports whether text marks a landing checked: it
// begins, after any leading white space, with PosternSnapshotVerifiedMarker.
// The one test the view, the snapshot and the Governor's Verified tap share.
func commentMarksVerified(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), PosternSnapshotVerifiedMarker)
}

// PosternSnapshotMemoryKey is the note Build remembers, between runs, what it
// last found of every landed candidate still worth reading — its
// comment_count and whether that count's comments carried
// PosternSnapshotVerifiedMarker (mw-tfne4.27). Comments are append-only: a
// candidate whose comment_count has not moved since the memory was written
// cannot have gained the marker, and one already found to carry it keeps it,
// so only a candidate unknown to the memory or whose count has since risen
// needs its comments read again. It is one note for the whole snapshot,
// JSON of id -> posternSnapshotMemoryEntry, read once and rewritten only
// when it changed — TrackerSync's own Note/SetNote, the same pattern
// SweepNotes uses.
const PosternSnapshotMemoryKey = "postern.snapshot.landed"

// PosternLandedMemoryTextLimit is the most runes of one remembered comment's
// text: the landed card in the snapshot and the verify card in the view show
// no more of a comment than this, whether it was just read or remembered.
const PosternLandedMemoryTextLimit = 600

// PosternLandedMemoryLimit is the most bytes PosternSnapshotMemoryKey's value
// may encode to; saveLandedMemory drops the oldest-closed beads until it
// fits. bd 1.3.0 (embedded Dolt) keeps a kv value in a TEXT column, so
// `bd kv set` refuses a value of 65536 bytes or more ("Error 1105: string
// '...' is too large for column 'value'"; 65535 bytes were accepted, checked
// 2026-09-29): this is well under that, leaving room for the JSON's growth.
const PosternLandedMemoryLimit = 48 * 1024

// posternSnapshotMemoryEntry is what PosternSnapshotMemoryKey remembers of
// one landed candidate: its comment_count and verdict, and — only when it is
// not verified, so worth showing — the comments a fresh read of it built,
// so that a memory hit need not read them again to display them.
type posternSnapshotMemoryEntry struct {
	Count    int                      `json:"count"`
	Verified bool                     `json:"verified"`
	Comments []PosternSnapshotComment `json:"comments,omitempty"`
	// Check is the first sentence of the Mayor's landing-check comment, kept
	// beside Comments because that comment may be older than the newest few
	// they hold; "" when none was found.
	Check string `json:"check,omitempty"`
	// HowTo is the HOW TO CHECK IT section of the candidate's comments, kept
	// whole because it can run past the text a remembered comment keeps; ""
	// when they have none.
	HowTo string `json:"how_to,omitempty"`
	// Closed is when the candidate closed, in Unix seconds: the oldest are
	// dropped first when the memory outgrows PosternLandedMemoryLimit.
	Closed int64 `json:"closed,omitempty"`
}

// PosternSnapshotFile is where mw postern snapshot writes the encrypted
// snapshot of every live epic, postern's docs/protocol.md §7 — the file nginx
// serves at /snapshot.
type PosternSnapshotFile interface {
	// Path reports where the snapshot is written, for a message a person reads.
	Path() string
	// Write writes data to Path, atomically: a temp file in the same
	// directory, then renamed over it, so a reader never sees half of one.
	Write(ctx context.Context, data []byte) error
}

// PosternSnapshotDoc is the decrypted plaintext of a snapshot, postern's
// docs/protocol.md §7 exactly: one entry per live epic.
type PosternSnapshotDoc struct {
	WrittenAt string                `json:"written_at"`
	Epics     []PosternSnapshotEpic `json:"epics"`
}

// PosternSnapshotEpic is one live epic's brief within a snapshot.
type PosternSnapshotEpic struct {
	ID          string                    `json:"id"`
	Title       string                    `json:"title"`
	Priority    string                    `json:"priority"`
	Status      string                    `json:"status"`
	NeedsYou    []PosternSnapshotQuestion `json:"needs_you"`
	Landed      []PosternSnapshotLanded   `json:"landed"`
	Working     []PosternSnapshotWorking  `json:"working"`
	ClosedCount int                       `json:"closed_count"`
}

// PosternSnapshotComment is one comment carried in a snapshot entry, newest
// first: postern's docs/protocol.md §7, the Governor's decision (2) on
// mw-hy6f4.
type PosternSnapshotComment struct {
	At   string `json:"at"`
	Text string `json:"text"`
}

// PosternSnapshotQuestion is one child still waiting on a decision-needed
// question asked over the postern.
type PosternSnapshotQuestion struct {
	ID          string                   `json:"id"`
	Title       string                   `json:"title"`
	AskedAt     string                   `json:"asked_at"`
	Recommended string                   `json:"recommended"`
	Options     []string                 `json:"options"`
	Description string                   `json:"description"`
	Comments    []PosternSnapshotComment `json:"comments"`
}

// PosternSnapshotLanded is one child closed within PosternSnapshotWindow and
// not yet verified.
type PosternSnapshotLanded struct {
	ID          string                   `json:"id"`
	Title       string                   `json:"title"`
	LandedAt    string                   `json:"landed_at"`
	Description string                   `json:"description"`
	Comments    []PosternSnapshotComment `json:"comments"`
}

// PosternSnapshotWorking is one child in progress, or open and unblocked.
type PosternSnapshotWorking struct {
	ID          string                   `json:"id"`
	Title       string                   `json:"title"`
	Status      string                   `json:"status"`
	Priority    string                   `json:"priority"`
	UpdatedAt   string                   `json:"updated_at"`
	Waits       []string                 `json:"waits"`
	Description string                   `json:"description"`
	Comments    []PosternSnapshotComment `json:"comments"`
}

// posternQuestionComment reads back what PosternSend's recordQuestion wrote:
// "QUESTION <asked_at> asked by postern, txid <txid>: <text> (recommended
// <rec>; options <a, b, ...>)".
var posternQuestionComment = regexp.MustCompile(`^QUESTION (\S+) asked by postern, txid \S+: (.*) \(recommended ([^;]*); options (.*)\)$`)

// posternQuestionFacts is what an open question asked over the postern says:
// when it was asked, what it asked, what it recommended and every option it
// offered. PosternSnapshot's needs_you and PosternView's question needs are
// both built from it.
type posternQuestionFacts struct {
	AskedAt     time.Time
	Text        string
	Recommended string
	Options     []string
}

// questionFromComments reads the newest QUESTION comment among comments
// (oldest first, as the tracker lists them): asked_at from the comment's own
// recorded time when the tracker gives one, or else from the timestamp its
// text carries; the question, recommended and options from its text. ok is
// false when no comment is a QUESTION.
func questionFromComments(comments []Comment) (posternQuestionFacts, bool) {
	for i := len(comments) - 1; i >= 0; i-- {
		m := posternQuestionComment.FindStringSubmatch(comments[i].Text)
		if m == nil {
			continue
		}
		askedAt := comments[i].Created
		if askedAt.IsZero() {
			if parsed, err := time.Parse(time.RFC3339, m[1]); err == nil {
				askedAt = parsed
			}
		}
		return posternQuestionFacts{
			AskedAt:     askedAt,
			Text:        m[2],
			Recommended: strings.TrimSpace(m[3]),
			Options:     splitOptions(m[4]),
		}, true
	}
	return posternQuestionFacts{}, false
}

// questionFromNote reads a bead's PosternQuestionKey note, when it records
// what was asked (posternQuestionNote's Q): no comment need be read to know
// the question then. ok is false for a note that does not — a bare txid, or
// one written before those fields existed — whose question is read from its
// comment instead.
func questionFromNote(noteValue string) (posternQuestionFacts, bool) {
	var note posternQuestionNote
	if err := json.Unmarshal([]byte(noteValue), &note); err != nil || strings.TrimSpace(note.Q) == "" {
		return posternQuestionFacts{}, false
	}
	facts := posternQuestionFacts{Text: note.Q, Recommended: note.Rec, Options: append([]string{}, note.Options...)}
	if asked, err := time.Parse(time.RFC3339, note.Asked); err == nil {
		facts.AskedAt = asked
	}
	return facts, true
}

// PosternSnapshot builds the §7 snapshot of every live epic — open or in
// progress — and, once built, encrypts it to the Governor's key and writes it
// where nginx serves it. Building is read-only of the tracker: no bead is
// written, no note changed.
type PosternSnapshot struct {
	Tracker WorkTracker
	Notes   PosternNotes
	Cipher  Cipher
	File    PosternSnapshotFile

	// GovernorKey is who the snapshot is encrypted to: config
	// postern_governor_key. Required only by Run, not Build.
	GovernorKey string

	// Now is the clock written_at is stamped with, and PosternSnapshotWindow
	// is measured against. The zero value reads the real one.
	Now func() time.Time

	// Out is where Run prints what it wrote. A nil Out prints nothing.
	Out io.Writer
	// Err is where a failure to write the landed memory is said; the
	// snapshot is built regardless. A nil Err says nothing.
	Err io.Writer
}

// Run builds the snapshot, encrypts it to GovernorKey and writes it through
// File, reporting the doc it wrote (so a caller can say how much it held
// without decrypting the file back). What write needs — the cipher, the file
// and the governor key — is checked before Build makes a single call to the
// tracker, so that a config mistake fails in an instant rather than after
// however long a full read of every live epic takes (mw-tfne4.8).
func (s PosternSnapshot) Run(ctx context.Context) (PosternSnapshotDoc, error) {
	if err := s.checkWritable(); err != nil {
		return PosternSnapshotDoc{}, err
	}
	doc, err := s.Build(ctx)
	if err != nil {
		return PosternSnapshotDoc{}, err
	}
	plaintextBytes, err := s.write(ctx, doc)
	if err != nil {
		return PosternSnapshotDoc{}, err
	}
	if s.Out != nil {
		fmt.Fprintf(s.Out, "wrote the snapshot of %d live epic(s), %d plaintext byte(s), to %s\n", len(doc.Epics), plaintextBytes, s.File.Path())
	}
	return doc, nil
}

// checkWritable reports whether Run has everything write will need, without
// touching the tracker: a cipher, somewhere to write, and a governor key to
// encrypt to.
func (s PosternSnapshot) checkWritable() error {
	if s.Cipher == nil {
		return fmt.Errorf("mw postern snapshot: no cipher is configured")
	}
	if s.File == nil {
		return fmt.Errorf("mw postern snapshot: nowhere to write the snapshot")
	}
	if strings.TrimSpace(s.GovernorKey) == "" {
		return fmt.Errorf("mw postern snapshot: postern_governor_key is not set, so there is no one to encrypt it to")
	}
	return nil
}

// write encrypts doc's JSON to GovernorKey and writes it through File,
// reporting the plaintext's own byte size for Run to log — the encrypted
// snapshot rides in the httpd's response body, so its plaintext size is worth
// watching even though it is never itself written to disk.
func (s PosternSnapshot) write(ctx context.Context, doc PosternSnapshotDoc) (int, error) {
	if err := s.checkWritable(); err != nil {
		return 0, err
	}
	plaintext, err := json.Marshal(doc)
	if err != nil {
		return 0, fmt.Errorf("building the snapshot's JSON: %w", err)
	}
	ciphertext, err := s.Cipher.Encrypt(s.GovernorKey, string(plaintext))
	if err != nil {
		return 0, err
	}
	if err := s.File.Write(ctx, []byte(ciphertext)); err != nil {
		return 0, err
	}
	return len(plaintext), nil
}

// Build reads the tracker and reports the snapshot's plaintext, without
// encrypting or writing anything: what --json prints for inspection.
//
// Reading is done in as few tracker calls as the shape of the data allows,
// rather than one per epic and more per question (mw-tfne4.17): every live
// epic's own fields and every epic's children are read by startEpic, without
// touching a single story's comments; every child across every epic that
// still needs a comment read — every needs_you and working candidate, to
// build its entry, and a landed candidate only when its own comment_count
// says it carries one worth checking for VERIFIED (mw-tfne4.24) and
// PosternSnapshotMemoryKey does not already answer that at its current
// comment_count (mw-tfne4.27) — is collected first and read back in the one
// StoriesComments call finishEpic then applies to each epic in turn. A
// candidate with no comments at all needs none read to know it carries none;
// it is most of a live epic's closed-within-window tail.
func (s PosternSnapshot) Build(ctx context.Context) (PosternSnapshotDoc, error) {
	if s.Tracker == nil {
		return PosternSnapshotDoc{}, fmt.Errorf("mw postern snapshot: no work tracker is configured")
	}
	if s.Notes == nil {
		return PosternSnapshotDoc{}, fmt.Errorf("mw postern snapshot: nowhere to read a question's note from")
	}

	memory, err := s.readLandedMemory(ctx)
	if err != nil {
		return PosternSnapshotDoc{}, err
	}

	ids, err := s.Tracker.LiveEpics(ctx)
	if err != nil {
		return PosternSnapshotDoc{}, fmt.Errorf("listing the live epics: %w", err)
	}

	epics, err := s.Tracker.ShowEpics(ctx, ids)
	if err != nil {
		return PosternSnapshotDoc{}, fmt.Errorf("reading the live epics: %w", err)
	}

	builds := make([]*epicBuild, len(epics))
	var needComments []string
	needed := map[string]bool{}
	need := func(id string) {
		if !needed[id] {
			needed[id] = true
			needComments = append(needComments, id)
		}
	}
	for i, detail := range epics {
		b, err := s.startEpic(ctx, detail)
		if err != nil {
			return PosternSnapshotDoc{}, err
		}
		builds[i] = b
		for _, child := range b.needsYou {
			need(child.Story.ID)
		}
		for _, child := range b.landedCand {
			// A landed candidate can only carry PosternSnapshotVerifiedMarker
			// on a comment it actually has: one the listing already reports
			// as carrying none is landed without spending a read on it
			// (mw-tfne4.24) — most of a live epic's closed-within-window
			// tail, which is never commented on again once closed. One the
			// memory already answers at its current comment_count needs no
			// read either (mw-tfne4.27).
			if child.CommentCount == 0 {
				continue
			}
			if mem, known := memory[child.Story.ID]; known && mem.Count == child.CommentCount {
				continue
			}
			need(child.Story.ID)
		}
		// A working candidate can also be a needs_you question — an open,
		// unblocked story asked over the postern is both — so need dedupes
		// rather than asking for its comments twice.
		for _, cand := range b.inProgress {
			if cand.story.CommentCount > 0 {
				need(cand.story.Story.ID)
			}
		}
		for _, cand := range b.openFrontier {
			if cand.story.CommentCount > 0 {
				need(cand.story.Story.ID)
			}
		}
	}

	comments, err := s.Tracker.StoriesComments(ctx, needComments)
	if err != nil {
		return PosternSnapshotDoc{}, fmt.Errorf("reading the comments of the live epics' children: %w", err)
	}

	newMemory := map[string]posternSnapshotMemoryEntry{}
	doc := PosternSnapshotDoc{WrittenAt: s.now().UTC().Format(time.RFC3339)}
	for _, b := range builds {
		doc.Epics = append(doc.Epics, s.finishEpic(b, comments, memory, newMemory))
	}

	if err := s.writeLandedMemory(ctx, memory, newMemory); err != nil {
		sayLandedMemoryFailed(s.Err, err)
	}

	return doc, nil
}

// readLandedMemory reads PosternSnapshotMemoryKey, reporting what it holds as
// id -> posternSnapshotMemoryEntry. A note never set, or one whose text does
// not parse as that JSON, means remember nothing: Build proceeds exactly as
// if this were its first run (mw-tfne4.27).
func (s PosternSnapshot) readLandedMemory(ctx context.Context) (map[string]posternSnapshotMemoryEntry, error) {
	raw, err := s.Notes.Note(ctx, PosternSnapshotMemoryKey)
	if err != nil {
		return nil, fmt.Errorf("reading the landed comment memory: %w", err)
	}
	return parseLandedMemory(raw), nil
}

// parseLandedMemory reads PosternSnapshotMemoryKey's value as id ->
// posternSnapshotMemoryEntry; an empty or unreadable value remembers
// nothing. PosternView reads the same note, from its one read of every note.
func parseLandedMemory(raw string) map[string]posternSnapshotMemoryEntry {
	memory := map[string]posternSnapshotMemoryEntry{}
	if strings.TrimSpace(raw) != "" {
		var parsed map[string]posternSnapshotMemoryEntry
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil && parsed != nil {
			memory = parsed
		}
	}
	return memory
}

// writeLandedMemory rewrites PosternSnapshotMemoryKey to newMemory, unless it
// holds exactly what oldMemory already did: a Build that remembered nothing
// new spends no write on it. newMemory carries an entry only for a landed
// candidate this Build actually saw with a comment worth remembering, so one
// that has aged out of PosternSnapshotWindow since oldMemory was read is
// dropped here.
func (s PosternSnapshot) writeLandedMemory(ctx context.Context, oldMemory, newMemory map[string]posternSnapshotMemoryEntry) error {
	return saveLandedMemory(ctx, s.Notes, oldMemory, newMemory)
}

// saveLandedMemory rewrites PosternSnapshotMemoryKey to newMemory through
// notes, unless it holds exactly what oldMemory already did. PosternSnapshot
// and PosternView both keep it: their landed and verify candidates are the
// same stories — closed within PosternSnapshotWindow under a live epic — so
// either one's read spares the other one. It is only a cache: a caller says
// an error from here (sayLandedMemoryFailed) and carries on.
func saveLandedMemory(ctx context.Context, notes PosternNotes, oldMemory, newMemory map[string]posternSnapshotMemoryEntry) error {
	newMemory, encoded, err := boundLandedMemory(newMemory)
	if err != nil {
		return fmt.Errorf("building the landed comment memory: %w", err)
	}
	if reflect.DeepEqual(oldMemory, newMemory) {
		return nil
	}
	if err := notes.SetNote(ctx, PosternSnapshotMemoryKey, encoded); err != nil {
		return fmt.Errorf("writing the landed comment memory: %w", err)
	}
	return nil
}

// boundLandedMemory reports memory as encoded within PosternLandedMemoryLimit
// bytes, dropping the oldest-closed beads first (a bead dropped is only read
// again by a later run), and that encoding.
func boundLandedMemory(memory map[string]posternSnapshotMemoryEntry) (map[string]posternSnapshotMemoryEntry, string, error) {
	encoded, err := json.Marshal(memory)
	if err != nil {
		return nil, "", err
	}
	if len(encoded) <= PosternLandedMemoryLimit {
		return memory, string(encoded), nil
	}

	ids := make([]string, 0, len(memory))
	size := 2 + len(memory) - 1 // the braces and the commas between entries
	for id, entry := range memory {
		key, err := json.Marshal(id)
		if err != nil {
			return nil, "", err
		}
		value, err := json.Marshal(entry)
		if err != nil {
			return nil, "", err
		}
		size += len(key) + 1 + len(value)
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if a, b := memory[ids[i]].Closed, memory[ids[j]].Closed; a != b {
			return a < b
		}
		return ids[i] < ids[j]
	})
	kept := make(map[string]posternSnapshotMemoryEntry, len(memory))
	for id, entry := range memory {
		kept[id] = entry
	}
	for _, id := range ids {
		if size <= PosternLandedMemoryLimit || len(kept) == 0 {
			break
		}
		key, _ := json.Marshal(id)
		value, _ := json.Marshal(kept[id])
		size -= len(key) + 1 + len(value) + 1
		delete(kept, id)
	}
	encoded, err = json.Marshal(kept)
	if err != nil {
		return nil, "", err
	}
	return kept, string(encoded), nil
}

// sayLandedMemoryFailed says on w, in one line, that the landed memory could
// not be kept and the run goes on without it. The error may carry the whole
// value bd refused, so it is cut short. A nil w says nothing.
func sayLandedMemoryFailed(w io.Writer, err error) {
	if w == nil {
		return
	}
	line := strings.Join(strings.Fields(err.Error()), " ")
	fmt.Fprintf(w, "mw postern: %s (carrying on without it)\n", clippedTo(line, 240))
}

// clippedMemoryComments is comments with each text cut to
// PosternLandedMemoryTextLimit runes, in a slice of its own: the memory
// keeps no more of a comment than a card shows.
func clippedMemoryComments(comments []PosternSnapshotComment) []PosternSnapshotComment {
	if comments == nil {
		return nil
	}
	out := make([]PosternSnapshotComment, len(comments))
	for i, c := range comments {
		out[i] = PosternSnapshotComment{At: c.At, Text: clippedTo(c.Text, PosternLandedMemoryTextLimit)}
	}
	return out
}

// workingCandidate is one child working or on the open frontier, before its
// comments are read: startEpic finds it and what it waits on; finishEpic
// completes its entry with its description (already on StoryDetail, no
// further call) and its comments, once read.
type workingCandidate struct {
	story StoryDetail
	waits []string
}

// epicBuild is one live epic's brief in progress: everything Build can read
// without any story's comments, plus which children still need one read —
// needsYou, landedCand, inProgress and openFrontier — so that every comment
// they need, across every epic, is read back in the one StoriesComments call
// Build makes.
type epicBuild struct {
	out          PosternSnapshotEpic
	needsYou     []StoryDetail
	landedCand   []StoryDetail
	inProgress   []workingCandidate
	openFrontier []workingCandidate
	counted      int
	total        int
}

// startEpic builds an epic's header and sorts its children into needs_you,
// landed and working candidates for finishEpic to read the comments of —
// without reading a single comment itself.
func (s PosternSnapshot) startEpic(ctx context.Context, detail EpicDetail) (*epicBuild, error) {
	b := &epicBuild{
		out: PosternSnapshotEpic{
			ID:       detail.ID,
			Title:    detail.Title,
			Priority: priorityLabel(detail.Priority),
			Status:   detail.Status,
			NeedsYou: []PosternSnapshotQuestion{},
			Landed:   []PosternSnapshotLanded{},
		},
	}

	lookup := map[string]StoryDetail{}
	for _, child := range detail.Stories {
		lookup[child.Story.ID] = child
	}

	now := s.now()
	for _, child := range detail.Stories {
		if child.IsEpic {
			continue
		}
		b.total++

		// A story closed outside the Landed window is neither landed nor
		// needs_you nor working: it is read no further, so an epic's long tail
		// of old, finished stories costs no comment read and no note read
		// (mw-tfne4.8) — this is most of an epic's history on a live rig.
		if child.Closed() {
			if child.ClosedAt.IsZero() || now.Sub(child.ClosedAt) > PosternSnapshotWindow {
				continue
			}
			// Closed as dropped, not landed: no Landed entry (mw-tbx1n.21).
			if hasLanded(child) {
				b.landedCand = append(b.landedCand, child)
			}
			continue
		}

		asked, err := s.Notes.Note(ctx, PosternQuestionKey(child.Story.ID))
		if err != nil {
			return nil, fmt.Errorf("reading %s's question note: %w", child.Story.ID, err)
		}
		if strings.TrimSpace(asked) != "" {
			b.needsYou = append(b.needsYou, child)
		}

		switch {
		case strings.EqualFold(strings.TrimSpace(child.Status), StatusInProgress):
			waits, err := s.waits(ctx, child, lookup)
			if err != nil {
				return nil, err
			}
			b.inProgress = append(b.inProgress, workingCandidate{story: child, waits: waits})
			b.counted++
		case !child.Held():
			waits, err := s.waits(ctx, child, lookup)
			if err != nil {
				return nil, err
			}
			if len(waits) == 0 {
				b.openFrontier = append(b.openFrontier, workingCandidate{story: child, waits: waits})
				b.counted++
			}
		}
	}

	sort.SliceStable(b.openFrontier, func(i, j int) bool {
		return b.openFrontier[i].story.Priority < b.openFrontier[j].story.Priority
	})
	return b, nil
}

// finishEpic completes an epic's brief with its children's comments, already
// read by Build into comments, keyed by story id. memory is what Build
// remembered of a landed candidate's verdict at the start of this run;
// newMemory is filled in with what this run knows of one worth remembering,
// for Build to write back.
//
// closed_count keeps the running total of every closed child (mw-hy6f4's
// Governor decision (2)): it is b.total less only the children currently
// working (in progress or on the open frontier), so a child shown under
// landed is still counted here too — narrowing PosternSnapshotWindow no
// longer loses track of how much has landed in all.
func (s PosternSnapshot) finishEpic(b *epicBuild, comments map[string][]Comment, memory, newMemory map[string]posternSnapshotMemoryEntry) PosternSnapshotEpic {
	out := b.out
	out.Working = []PosternSnapshotWorking{}
	for _, cand := range b.inProgress {
		out.Working = append(out.Working, workingEntry(cand, comments[cand.story.Story.ID]))
	}
	for _, cand := range b.openFrontier {
		out.Working = append(out.Working, workingEntry(cand, comments[cand.story.Story.ID]))
	}

	for _, child := range b.landedCand {
		landed, ok := landedVerdict(child, comments, memory, newMemory)
		if ok {
			out.Landed = append(out.Landed, landed)
		}
	}
	for _, child := range b.needsYou {
		out.NeedsYou = append(out.NeedsYou, needsYouFromComments(child, comments[child.Story.ID]))
	}

	out.ClosedCount = b.total - b.counted
	return out
}

// needsYouFromComments builds one needs_you entry from the newest QUESTION
// comment among child's comments, recommended and options read from its
// text, asked_at from the comment's own recorded time when the tracker gives
// one, or else from the timestamp the comment's text itself carries; its
// description and newest comments (mw-hy6f4's Governor decision (2)) come
// along the same way a working or landed entry's do.
func needsYouFromComments(child StoryDetail, comments []Comment) PosternSnapshotQuestion {
	question := PosternSnapshotQuestion{
		ID:          child.Story.ID,
		Title:       child.Story.Title,
		Description: clippedTo(child.Description, PosternSnapshotTextLimit),
		Comments:    snapshotComments(comments),
	}
	if facts, ok := questionFromComments(comments); ok {
		question.AskedAt = formatOrEmpty(facts.AskedAt)
		question.Recommended = facts.Recommended
		question.Options = facts.Options
	}
	return question
}

// landedVerdict reports whether child belongs in its epic's landed group,
// consulting memory before spending a read on its comments (mw-tfne4.27): a
// candidate with no comments at all is landed by construction and never
// worth remembering; one memory already answers at its current comment_count
// is classified from there, its comments read back from the memory itself
// rather than read again (mw-hy6f4's Governor decision (2) needs them for
// display, not just the VERIFIED check mw-tfne4.27 remembered them for);
// anything else is classified fresh from comments, and that verdict, with
// its comments, is recorded into newMemory for Build to write back.
func landedVerdict(child StoryDetail, comments map[string][]Comment, memory, newMemory map[string]posternSnapshotMemoryEntry) (PosternSnapshotLanded, bool) {
	landed := PosternSnapshotLanded{
		ID:          child.Story.ID,
		Title:       child.Story.Title,
		LandedAt:    formatOrEmpty(child.ClosedAt),
		Description: clippedTo(child.Description, PosternSnapshotTextLimit),
		Comments:    []PosternSnapshotComment{},
	}

	if child.CommentCount == 0 {
		return landed, true
	}

	var closed int64
	if !child.ClosedAt.IsZero() {
		closed = child.ClosedAt.Unix()
	}

	if mem, known := memory[child.Story.ID]; known && mem.Count == child.CommentCount {
		// A memory written before comments were cut is cut here, and stamped
		// with its close time, on its way to being written back.
		mem.Comments = clippedMemoryComments(mem.Comments)
		mem.Closed = closed
		newMemory[child.Story.ID] = mem
		landed.Comments = mem.Comments
		return landed, !mem.Verified
	}

	result, ok := landedFromComments(child, comments[child.Story.ID])
	entry := posternSnapshotMemoryEntry{Count: child.CommentCount, Verified: !ok, Closed: closed}
	if ok {
		result.Comments = clippedMemoryComments(result.Comments)
		entry.Comments = result.Comments
		entry.Check = landingCheck(comments[child.Story.ID])
		entry.HowTo = howToCheck(comments[child.Story.ID])
	}
	newMemory[child.Story.ID] = entry
	return result, ok
}

// landedFromComments reports child as a landed entry, unless it carries a
// comment that marks a landing verified. The caller has already
// established child closed within PosternSnapshotWindow. ok is false for a
// verified child.
func landedFromComments(child StoryDetail, comments []Comment) (PosternSnapshotLanded, bool) {
	for _, c := range comments {
		if commentMarksVerified(c.Text) {
			return PosternSnapshotLanded{}, false
		}
	}
	return PosternSnapshotLanded{
		ID:          child.Story.ID,
		Title:       child.Story.Title,
		LandedAt:    formatOrEmpty(child.ClosedAt),
		Description: clippedTo(child.Description, PosternSnapshotTextLimit),
		Comments:    snapshotComments(comments),
	}, true
}

// waits is the ids of what child still waits on that is not finished, reading
// a blocker not among lookup already from the tracker. It mirrors Brief's own
// waitsOn, but reports ids rather than titles: postern's docs/protocol.md §7
// names waits by bead id.
func (s PosternSnapshot) waits(ctx context.Context, child StoryDetail, lookup map[string]StoryDetail) ([]string, error) {
	ids := []string{}
	for _, need := range child.Needs {
		blocker, known := lookup[need]
		if !known {
			read, err := s.Tracker.ShowStory(ctx, need)
			if err != nil {
				return nil, fmt.Errorf("reading %s, which %s waits on: %w", need, child.Story.ID, err)
			}
			blocker = read
			lookup[need] = blocker
		}
		if blocker.Closed() {
			continue
		}
		ids = append(ids, need)
	}
	return ids, nil
}

// now is the clock Build and Run use: s.Now when set, time.Now otherwise.
func (s PosternSnapshot) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// workingEntry is cand as a PosternSnapshotWorking line, its comments already
// read by Build when it carried any (mw-hy6f4's Governor decision (2)).
func workingEntry(cand workingCandidate, comments []Comment) PosternSnapshotWorking {
	child := cand.story
	return PosternSnapshotWorking{
		ID:          child.Story.ID,
		Title:       child.Story.Title,
		Status:      child.Status,
		Priority:    priorityLabel(child.Priority),
		UpdatedAt:   formatOrEmpty(child.Updated),
		Waits:       cand.waits,
		Description: clippedTo(child.Description, PosternSnapshotTextLimit),
		Comments:    snapshotComments(comments),
	}
}

// priorityLabel is a priority as postern's docs/protocol.md §7 writes it:
// "P0".."P4", 0 being the most urgent.
func priorityLabel(priority int) string { return fmt.Sprintf("P%d", priority) }

// snapshotComments is comments' newest PosternSnapshotCommentLimit, newest
// first, each cut to PosternSnapshotTextLimit runes: postern's
// docs/protocol.md §7, the Governor's decision (2) on mw-hy6f4. comments is
// oldest first, as the tracker returns it. Never nil, so a snapshot always
// writes "comments":[] rather than null.
func snapshotComments(comments []Comment) []PosternSnapshotComment {
	out := []PosternSnapshotComment{}
	for i := len(comments) - 1; i >= 0 && len(out) < PosternSnapshotCommentLimit; i-- {
		out = append(out, PosternSnapshotComment{
			At:   formatOrEmpty(comments[i].Created),
			Text: clippedTo(comments[i].Text, PosternSnapshotTextLimit),
		})
	}
	return out
}

// formatOrEmpty is a time as postern's docs/protocol.md §7 writes it, ISO
// 8601, or "" for the zero time.
func formatOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// splitOptions reads an "a, b, c" options list, as PosternSend's
// recordQuestion joins it, back into a slice. An empty list is an empty
// slice, not one empty option, and never nil: postern's docs/protocol.md §7
// writes options as an array on every question.
func splitOptions(csv string) []string {
	options := []string{}
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return options
	}
	for _, o := range strings.Split(csv, ",") {
		if o = strings.TrimSpace(o); o != "" {
			options = append(options, o)
		}
	}
	return options
}
