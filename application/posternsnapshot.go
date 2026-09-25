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
// without decrypting the file back).
func (s PosternSnapshot) Run(ctx context.Context) (PosternSnapshotDoc, error) {
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

// write encrypts doc's JSON to GovernorKey and writes it through File.
func (s PosternSnapshot) write(ctx context.Context, doc PosternSnapshotDoc) error {
	if s.Cipher == nil {
		return fmt.Errorf("mw postern snapshot: no cipher is configured")
	}
	if s.File == nil {
		return fmt.Errorf("mw postern snapshot: nowhere to write the snapshot")
	}
	if strings.TrimSpace(s.GovernorKey) == "" {
		return fmt.Errorf("mw postern snapshot: postern_governor_key is not set, so there is no one to encrypt it to")
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

	doc := PosternSnapshotDoc{WrittenAt: s.now().UTC().Format(time.RFC3339)}
	for _, id := range ids {
		epic, err := s.epic(ctx, id)
		if err != nil {
			return PosternSnapshotDoc{}, err
		}
		doc.Epics = append(doc.Epics, epic)
	}
	return doc, nil
}

// epic builds one live epic's brief: needs_you, landed and working, in that
// order, and everything else collapsed to closed_count.
func (s PosternSnapshot) epic(ctx context.Context, id string) (PosternSnapshotEpic, error) {
	detail, err := s.Tracker.ShowEpic(ctx, id)
	if err != nil {
		return PosternSnapshotEpic{}, fmt.Errorf("reading the epic %s: %w", id, err)
	}

	out := PosternSnapshotEpic{
		ID:       detail.ID,
		Title:    detail.Title,
		Priority: priorityLabel(detail.Priority),
		Status:   detail.Status,
	}

	lookup := map[string]StoryDetail{}
	for _, child := range detail.Stories {
		lookup[child.Story.ID] = child
	}

	now := s.now()
	var inProgress, openFrontier []PosternSnapshotWorking
	counted := 0
	total := 0

	for _, child := range detail.Stories {
		if child.IsEpic {
			continue
		}
		total++

		asked, err := s.Notes.Note(ctx, PosternQuestionKey(child.Story.ID))
		if err != nil {
			return PosternSnapshotEpic{}, fmt.Errorf("reading %s's question note: %w", child.Story.ID, err)
		}
		if strings.TrimSpace(asked) != "" {
			question, err := s.needsYou(ctx, child)
			if err != nil {
				return PosternSnapshotEpic{}, err
			}
			out.NeedsYou = append(out.NeedsYou, question)
		}

		switch {
		case child.Closed():
			landed, ok, err := s.landed(ctx, child, now)
			if err != nil {
				return PosternSnapshotEpic{}, err
			}
			if ok {
				out.Landed = append(out.Landed, landed)
				counted++
			}
		case strings.EqualFold(strings.TrimSpace(child.Status), StatusInProgress):
			waits, err := s.waits(ctx, child, lookup)
			if err != nil {
				return PosternSnapshotEpic{}, err
			}
			inProgress = append(inProgress, workingEntry(child, waits))
			counted++
		case !child.Held():
			waits, err := s.waits(ctx, child, lookup)
			if err != nil {
				return PosternSnapshotEpic{}, err
			}
			if len(waits) == 0 {
				openFrontier = append(openFrontier, workingEntry(child, waits))
				counted++
			}
		}
	}

	sort.SliceStable(openFrontier, func(i, j int) bool {
		return priorityOf(openFrontier[i].Priority) < priorityOf(openFrontier[j].Priority)
	})
	out.Working = append(inProgress, openFrontier...)
	out.ClosedCount = total - counted
	return out, nil
}

// needsYou builds one needs_you entry from the newest QUESTION comment on
// child, recommended and options read from its text, asked_at from the
// comment's own recorded time when the tracker gives one, or else from the
// timestamp the comment's text itself carries.
func (s PosternSnapshot) needsYou(ctx context.Context, child StoryDetail) (PosternSnapshotQuestion, error) {
	comments, err := s.Tracker.StoryComments(ctx, child.Story.ID)
	if err != nil {
		return PosternSnapshotQuestion{}, fmt.Errorf("reading %s's comments: %w", child.Story.ID, err)
	}
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
	return question, nil
}

// landed reports child as a landed entry when it closed within
// PosternSnapshotWindow of now and carries no comment naming
// PosternSnapshotVerifiedMarker. ok is false, with no error, for a child that
// does not belong in landed at all.
func (s PosternSnapshot) landed(ctx context.Context, child StoryDetail, now time.Time) (PosternSnapshotLanded, bool, error) {
	if child.ClosedAt.IsZero() || now.Sub(child.ClosedAt) > PosternSnapshotWindow {
		return PosternSnapshotLanded{}, false, nil
	}
	comments, err := s.Tracker.StoryComments(ctx, child.Story.ID)
	if err != nil {
		return PosternSnapshotLanded{}, false, fmt.Errorf("reading %s's comments: %w", child.Story.ID, err)
	}
	for _, c := range comments {
		if strings.Contains(c.Text, PosternSnapshotVerifiedMarker) {
			return PosternSnapshotLanded{}, false, nil
		}
	}
	return PosternSnapshotLanded{ID: child.Story.ID, Title: child.Story.Title, LandedAt: formatOrEmpty(child.ClosedAt)}, true, nil
}

// waits is the ids of what child still waits on that is not finished, reading
// a blocker not among lookup already from the tracker. It mirrors Brief's own
// waitsOn, but reports ids rather than titles: postern's docs/protocol.md §7
// names waits by bead id.
func (s PosternSnapshot) waits(ctx context.Context, child StoryDetail, lookup map[string]StoryDetail) ([]string, error) {
	var ids []string
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
// recordQuestion joins it, back into a slice. An empty list is nil, not one
// empty option.
func splitOptions(csv string) []string {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	var options []string
	for _, o := range strings.Split(csv, ",") {
		if o = strings.TrimSpace(o); o != "" {
			options = append(options, o)
		}
	}
	return options
}
