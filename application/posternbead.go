package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// PosternBeadCommentLimit is the most runes of one comment a bead's detail
// carries, postern's docs/protocol.md §12; past it the comment is cut, with a
// trailing marker.
const PosternBeadCommentLimit = 16000

// PosternBeadMissingExit is the status mw postern bead leaves with for a bead
// the tracker does not know: the postern backend answers it 404 (§12).
const PosternBeadMissingExit = 3

// posternBeadID is what §12 accepts as a bead id; anything else could never
// name one — and one starting with a dash would read to bd as a flag.
var posternBeadID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// PosternBeadMissing is a bead mw postern bead was asked for that the tracker
// does not hold.
type PosternBeadMissing struct {
	ID string
}

func (e *PosternBeadMissing) Error() string {
	return fmt.Sprintf("mw postern bead: there is no bead %q", e.ID)
}

// PosternBeadIsMissing reports whether err says a bead is not there.
func PosternBeadIsMissing(err error) bool {
	var missing *PosternBeadMissing
	return errors.As(err, &missing)
}

// PosternBeadDetail is one bead in full, postern's docs/protocol.md §12.
type PosternBeadDetail struct {
	V           int                  `json:"v"`
	ID          string               `json:"id"`
	Title       string               `json:"title"`
	Type        string               `json:"type"`
	Status      string               `json:"status"`
	Priority    int                  `json:"priority"`
	Parent      string               `json:"parent"`
	Labels      []string             `json:"labels"`
	Assignee    string               `json:"assignee"`
	Waits       []string             `json:"waits"`
	Blocks      []string             `json:"blocks"`
	Children    []string             `json:"children"`
	Created     string               `json:"created"`
	Updated     string               `json:"updated"`
	Started     string               `json:"started"`
	Closed      string               `json:"closed"`
	Path        *PosternViewPath     `json:"path,omitempty"`
	Attempts    int                  `json:"attempts"`
	Description string               `json:"description"`
	Acceptance  string               `json:"acceptance"`
	Comments    []PosternBeadComment `json:"comments"`
}

// PosternBeadComment is one comment of a bead's detail.
type PosternBeadComment struct {
	At     string `json:"at"`
	Author string `json:"author"`
	Text   string `json:"text"`
}

// PosternBead reads one bead in full for the Governor's app, postern's
// docs/protocol.md §12: the postern backend runs mw postern bead per request
// (POSTERN_BEAD_CMD), so it only reads, and reads little — the bead itself,
// its epic's children (to say what waits on it) or its own (for an epic),
// and its comments: four bd calls for a story.
type PosternBead struct {
	Tracker WorkTracker
	// Cipher and GovernorKey seal the detail, as the view is sealed. Required
	// only by Run.
	Cipher      Cipher
	GovernorKey string
	// Out is where Run prints the sealed detail. A nil Out prints nothing.
	Out io.Writer
}

// Run builds the detail of id, seals it to GovernorKey and prints it, one
// line: base64(BRC-78(gzip(JSON))), the same encoding as the view.
func (p PosternBead) Run(ctx context.Context, id string) (PosternBeadDetail, error) {
	if p.Cipher == nil {
		return PosternBeadDetail{}, fmt.Errorf("mw postern bead: no cipher is configured")
	}
	if strings.TrimSpace(p.GovernorKey) == "" {
		return PosternBeadDetail{}, fmt.Errorf("mw postern bead: postern_governor_key is not set, so there is no one to seal it to")
	}
	detail, err := p.Build(ctx, id)
	if err != nil {
		return PosternBeadDetail{}, err
	}
	sealed, _, _, err := SealPosternDoc(p.Cipher, p.GovernorKey, detail)
	if err != nil {
		return PosternBeadDetail{}, err
	}
	if p.Out != nil {
		fmt.Fprintln(p.Out, sealed)
	}
	return detail, nil
}

// Build reads the detail of id without sealing it: what --json prints. An id
// the tracker does not know, or one that could never name a bead, is a
// *PosternBeadMissing.
func (p PosternBead) Build(ctx context.Context, id string) (PosternBeadDetail, error) {
	if p.Tracker == nil {
		return PosternBeadDetail{}, fmt.Errorf("mw postern bead: no work tracker is configured")
	}
	if !posternBeadID.MatchString(id) {
		return PosternBeadDetail{}, &PosternBeadMissing{ID: id}
	}
	found, err := p.Tracker.ShowBeads(ctx, []string{id})
	if err != nil {
		return PosternBeadDetail{}, fmt.Errorf("reading %s: %w", id, err)
	}
	if len(found) == 0 {
		return PosternBeadDetail{}, &PosternBeadMissing{ID: id}
	}
	d := found[0]

	kind := strings.TrimSpace(d.Type)
	if kind == "" {
		kind = "task"
		if d.IsEpic {
			kind = "epic"
		}
	}
	detail := PosternBeadDetail{
		V: PosternViewVersion, ID: d.Story.ID, Title: d.Story.Title, Type: kind, Status: d.Status,
		Priority: d.Priority, Parent: d.EpicID, Labels: append([]string{}, d.Labels...), Assignee: d.Assignee,
		Waits: append([]string{}, d.Needs...), Blocks: []string{}, Children: []string{},
		Created: formatOrEmpty(d.Created), Updated: formatOrEmpty(d.Updated),
		Started: formatOrEmpty(d.Started), Closed: formatOrEmpty(d.ClosedAt),
		Attempts: d.Attempts, Description: d.Description, Acceptance: d.Acceptance,
		Comments: []PosternBeadComment{},
	}

	// What waits on it, as far as its own tree knows: its epic's children,
	// and — for an epic — its own.
	var tree []StoryDetail
	if d.IsEpic {
		epic, err := p.Tracker.ShowEpic(ctx, id)
		if err != nil {
			return PosternBeadDetail{}, fmt.Errorf("reading the children of %s: %w", id, err)
		}
		for _, child := range epic.Stories {
			detail.Children = append(detail.Children, child.Story.ID)
		}
		detail.Path = viewPathOrNil(epic.Defaults)
		tree = append(tree, epic.Stories...)
	} else {
		detail.Path = viewPathOf(d.Merged())
	}
	if d.EpicID != "" {
		// A parent that cannot be read as an epic leaves blocks as far as
		// the bead's own children know, and the path as the bead said it.
		if parent, err := p.Tracker.ShowEpic(ctx, d.EpicID); err == nil {
			tree = append(tree, parent.Stories...)
			if !d.IsEpic {
				detail.Path = viewPathOf(parent.Defaults.Overlay(d.Story.Overrides))
			}
		}
	}
	seen := map[string]bool{}
	for _, other := range tree {
		if other.Closed() || seen[other.Story.ID] || other.Story.ID == id {
			continue
		}
		for _, need := range other.Needs {
			if need == id {
				seen[other.Story.ID] = true
				detail.Blocks = append(detail.Blocks, other.Story.ID)
				break
			}
		}
	}

	comments, err := p.Tracker.StoryComments(ctx, id)
	if err != nil {
		return PosternBeadDetail{}, fmt.Errorf("reading the comments of %s: %w", id, err)
	}
	for _, c := range comments {
		detail.Comments = append(detail.Comments, PosternBeadComment{
			At: formatOrEmpty(c.Created), Author: c.Author, Text: clippedTo(c.Text, PosternBeadCommentLimit),
		})
	}
	return detail, nil
}
