package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

// PosternSnapshotWindow is how recently a child must have closed to appear in
// a snapshot's landed group: postern's docs/protocol.md §7.
const PosternSnapshotWindow = 7 * 24 * time.Hour

// PosternSnapshotVerifiedMarker is the word the Mayor's own comments carry
// once a landing has been checked out by hand (docs/codemap.md says so). A
// closed child carrying it in any comment is not landed: somebody has already
// looked.
const PosternSnapshotVerifiedMarker = "VERIFIED"

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

// PosternSnapshotQuestion is one child still waiting on a decision-needed
// question asked over the postern.
type PosternSnapshotQuestion struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	AskedAt     string   `json:"asked_at"`
	Recommended string   `json:"recommended"`
	Options     []string `json:"options"`
}

// PosternSnapshotLanded is one child closed recently and not yet verified.
type PosternSnapshotLanded struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	LandedAt string `json:"landed_at"`
}

// PosternSnapshotWorking is one child in progress, or open and unblocked.
type PosternSnapshotWorking struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	Priority  string   `json:"priority"`
	UpdatedAt string   `json:"updated_at"`
	Waits     []string `json:"waits"`
}

// posternQuestionComment reads back what PosternSend's recordQuestion wrote:
// "QUESTION <asked_at> asked by postern, txid <txid>: <text> (recommended
// <rec>; options <a, b, ...>)".
var posternQuestionComment = regexp.MustCompile(`^QUESTION (\S+) asked by postern, txid \S+: .*\(recommended ([^;]*); options (.*)\)$`)

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

	// Now is the clock written_at is stamped with, and "closed in the last 7
	// days" is measured against. The zero value reads the real one.
	Now func() time.Time

	// Out is where Run prints what it wrote. A nil Out prints nothing.
	Out io.Writer
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
	if err := s.write(ctx, doc); err != nil {
		return PosternSnapshotDoc{}, err
	}
	if s.Out != nil {
		fmt.Fprintf(s.Out, "wrote the snapshot of %d live epic(s) to %s\n", len(doc.Epics), s.File.Path())
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

// write encrypts doc's JSON to GovernorKey and writes it through File.
func (s PosternSnapshot) write(ctx context.Context, doc PosternSnapshotDoc) error {
	if err := s.checkWritable(); err != nil {
		return err
	}
	plaintext, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("building the snapshot's JSON: %w", err)
	}
	ciphertext, err := s.Cipher.Encrypt(s.GovernorKey, string(plaintext))
	if err != nil {
		return err
	}
	return s.File.Write(ctx, []byte(ciphertext))
}

// Build reads the tracker and reports the snapshot's plaintext, without
// encrypting or writing anything: what --json prints for inspection.
//
// Reading is done in as few tracker calls as the shape of the data allows,
// rather than one per epic and more per question (mw-tfne4.17): every live
// epic's own fields and every epic's children are read by startEpic, without
// touching a single story's comments; every child across every epic that
// still needs a comment read — to build its needs_you entry or to check a
// landed one for VERIFIED — is collected first and read back in the one
// StoriesComments call finishEpic then applies to each epic in turn.
func (s PosternSnapshot) Build(ctx context.Context) (PosternSnapshotDoc, error) {
	if s.Tracker == nil {
		return PosternSnapshotDoc{}, fmt.Errorf("mw postern snapshot: no work tracker is configured")
	}
	if s.Notes == nil {
		return PosternSnapshotDoc{}, fmt.Errorf("mw postern snapshot: nowhere to read a question's note from")
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
	for i, detail := range epics {
		b, err := s.startEpic(ctx, detail)
		if err != nil {
			return PosternSnapshotDoc{}, err
		}
		builds[i] = b
		for _, child := range b.needsYou {
			needComments = append(needComments, child.Story.ID)
		}
		for _, child := range b.landedCand {
			needComments = append(needComments, child.Story.ID)
		}
	}

	comments, err := s.Tracker.StoriesComments(ctx, needComments)
	if err != nil {
		return PosternSnapshotDoc{}, fmt.Errorf("reading the comments of the live epics' children: %w", err)
	}

	doc := PosternSnapshotDoc{WrittenAt: s.now().UTC().Format(time.RFC3339)}
	for _, b := range builds {
		doc.Epics = append(doc.Epics, s.finishEpic(b, comments))
	}
	return doc, nil
}

// epicBuild is one live epic's brief in progress: everything Build can read
// without any story's comments, plus which children still need one read —
// needsYou and landedCand — so that every comment they need, across every
// epic, is read back in the one StoriesComments call Build makes.
type epicBuild struct {
	out          PosternSnapshotEpic
	needsYou     []StoryDetail
	landedCand   []StoryDetail
	inProgress   []PosternSnapshotWorking
	openFrontier []PosternSnapshotWorking
	counted      int
	total        int
}

// startEpic builds an epic's header and its working frontier, and sorts its
// children into needs_you and landed candidates for finishEpic to read the
// comments of — without reading a single comment itself.
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
		inProgress:   []PosternSnapshotWorking{},
		openFrontier: []PosternSnapshotWorking{},
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
			b.landedCand = append(b.landedCand, child)
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
			b.inProgress = append(b.inProgress, workingEntry(child, waits))
			b.counted++
		case !child.Held():
			waits, err := s.waits(ctx, child, lookup)
			if err != nil {
				return nil, err
			}
			if len(waits) == 0 {
				b.openFrontier = append(b.openFrontier, workingEntry(child, waits))
				b.counted++
			}
		}
	}

	sort.SliceStable(b.openFrontier, func(i, j int) bool {
		return priorityOf(b.openFrontier[i].Priority) < priorityOf(b.openFrontier[j].Priority)
	})
	return b, nil
}

// finishEpic completes an epic's brief with its children's comments, already
// read by Build into comments, keyed by story id.
func (s PosternSnapshot) finishEpic(b *epicBuild, comments map[string][]Comment) PosternSnapshotEpic {
	out := b.out
	out.Working = append(b.inProgress, b.openFrontier...)

	counted := b.counted
	for _, child := range b.landedCand {
		landed, ok := landedFromComments(child, comments[child.Story.ID])
		if ok {
			out.Landed = append(out.Landed, landed)
			counted++
		}
	}
	for _, child := range b.needsYou {
		out.NeedsYou = append(out.NeedsYou, needsYouFromComments(child, comments[child.Story.ID]))
	}

	out.ClosedCount = b.total - counted
	return out
}

// needsYouFromComments builds one needs_you entry from the newest QUESTION
// comment among child's comments, recommended and options read from its
// text, asked_at from the comment's own recorded time when the tracker gives
// one, or else from the timestamp the comment's text itself carries.
func needsYouFromComments(child StoryDetail, comments []Comment) PosternSnapshotQuestion {
	question := PosternSnapshotQuestion{ID: child.Story.ID, Title: child.Story.Title}
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
		question.AskedAt = formatOrEmpty(askedAt)
		question.Recommended = strings.TrimSpace(m[2])
		question.Options = splitOptions(m[3])
		break
	}
	return question
}

// landedFromComments reports child as a landed entry, unless it carries a
// comment naming PosternSnapshotVerifiedMarker. The caller has already
// established child closed within PosternSnapshotWindow. ok is false for a
// verified child.
func landedFromComments(child StoryDetail, comments []Comment) (PosternSnapshotLanded, bool) {
	for _, c := range comments {
		if strings.Contains(c.Text, PosternSnapshotVerifiedMarker) {
			return PosternSnapshotLanded{}, false
		}
	}
	return PosternSnapshotLanded{ID: child.Story.ID, Title: child.Story.Title, LandedAt: formatOrEmpty(child.ClosedAt)}, true
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

// workingEntry is child as a PosternSnapshotWorking line.
func workingEntry(child StoryDetail, waits []string) PosternSnapshotWorking {
	return PosternSnapshotWorking{
		ID:        child.Story.ID,
		Title:     child.Story.Title,
		Status:    child.Status,
		Priority:  priorityLabel(child.Priority),
		UpdatedAt: formatOrEmpty(child.Updated),
		Waits:     waits,
	}
}

// priorityLabel is a priority as postern's docs/protocol.md §7 writes it:
// "P0".."P4", 0 being the most urgent.
func priorityLabel(priority int) string { return fmt.Sprintf("P%d", priority) }

// priorityOf reads a label priorityLabel wrote back into a number, for
// sorting the working frontier. A label it cannot read sorts last.
func priorityOf(label string) int {
	var n int
	if _, err := fmt.Sscanf(label, "P%d", &n); err != nil {
		return int(^uint(0) >> 1)
	}
	return n
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
