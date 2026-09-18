// Package beads reads and writes the factory's stories by shelling out to the
// `bd` command in the vault. It is the adapter behind application.WorkTracker.
//
// The beads database takes a single-writer lock, so a Gateway holds a mutex
// across every `bd` it starts: no two run at once through one Gateway.
package beads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// Program is the beads command this package shells out to.
const Program = "bd"

// StatusInProgress is beads' name for the status a claimed story is in.
const StatusInProgress = "in_progress"

// Gateway reads and writes stories in one vault's beads database.
type Gateway struct {
	vault string
	mu    sync.Mutex
}

// New returns a Gateway onto the beads database in a vault directory.
func New(vault string) *Gateway {
	return &Gateway{vault: vault}
}

// Vault reports the directory this Gateway runs bd in.
func (g *Gateway) Vault() string { return g.vault }

// Available reports whether the beads command is on PATH. Tests that need a
// real database skip themselves when it is not.
func Available() bool {
	_, err := exec.LookPath(Program)
	return err == nil
}

// ShowStory implements application.WorkTracker. It reads the story, and the
// defaults it inherits from its parent bead — from the copy of the parent that
// bd embeds in the story, or from a second look if that copy is not there.
func (g *Gateway) ShowStory(ctx context.Context, id string) (application.StoryDetail, error) {
	story, err := g.showOne(ctx, id)
	if err != nil {
		return application.StoryDetail{}, err
	}

	var defaults domain.Path
	if story.Parent != "" {
		var found bool
		if defaults, found = story.parentPath(); !found {
			epic, err := g.showOne(ctx, story.Parent)
			if err != nil {
				return application.StoryDetail{}, fmt.Errorf("reading the epic %s of story %s: %w", story.Parent, id, err)
			}
			defaults = domain.PathFromMetadata(epic.pathMetadata())
		}
	}
	return story.detail(defaults), nil
}

// ReadyStories implements application.WorkTracker. The epic's defaults are read
// once and overlaid on every story under it, including stories nested deeper
// than one level: the epic asked for is what sets the defaults.
func (g *Gateway) ReadyStories(ctx context.Context, epicID, host string) ([]application.StoryDetail, error) {
	epic, err := g.showOne(ctx, epicID)
	if err != nil {
		return nil, fmt.Errorf("reading the epic %s: %w", epicID, err)
	}
	defaults := domain.PathFromMetadata(epic.pathMetadata())

	out, err := g.call(ctx, "ready", "--parent", epicID, "--unassigned", "--exclude-type", "epic", "--json")
	if err != nil {
		return nil, err
	}
	stories, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading the ready stories of %s: %w", epicID, err)
	}

	// The host a story is worked on may be its own or the epic's, so the
	// filtering is on the overlaid Path rather than on the story's metadata.
	var ready []application.StoryDetail
	for _, story := range stories {
		detail := story.detail(defaults)
		if detail.Merged().Host != host {
			continue
		}
		ready = append(ready, detail)
	}
	return ready, nil
}

// ClaimStory implements application.WorkTracker.
func (g *Gateway) ClaimStory(ctx context.Context, id string) error {
	_, err := g.call(ctx, "update", id, "--claim")
	return err
}

// SetStoryMetadata implements application.WorkTracker. The fields are written
// in a stable order, so that what ran can be read off a failure.
func (g *Gateway) SetStoryMetadata(ctx context.Context, id string, fields map[string]string) error {
	if len(fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	args := []string{"update", id}
	for _, key := range keys {
		args = append(args, "--set-metadata", key+"="+fields[key])
	}
	_, err := g.call(ctx, args...)
	return err
}

// CommentOnStory implements application.WorkTracker.
func (g *Gateway) CommentOnStory(ctx context.Context, id, text string) error {
	_, err := g.call(ctx, "comment", id, text)
	return err
}

// CloseStory implements application.WorkTracker.
func (g *Gateway) CloseStory(ctx context.Context, id, reason string) error {
	_, err := g.call(ctx, "close", id, "--reason", reason)
	return err
}

// StaleClaims implements application.WorkTracker. bd counts staleness in whole
// days and will not accept fewer than one.
func (g *Gateway) StaleClaims(ctx context.Context, days int) ([]application.StoryDetail, error) {
	if days < 1 {
		return nil, fmt.Errorf("stale claims need at least 1 day, got %d", days)
	}
	out, err := g.call(ctx, "stale", "--days", strconv.Itoa(days), "--status", StatusInProgress, "--json")
	if err != nil {
		return nil, err
	}
	stories, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading the stale claims: %w", err)
	}

	claims := make([]application.StoryDetail, 0, len(stories))
	for _, story := range stories {
		claims = append(claims, story.detail(domain.Path{}))
	}
	return claims, nil
}

// showOne reads exactly one bead.
func (g *Gateway) showOne(ctx context.Context, id string) (bead, error) {
	out, err := g.call(ctx, "show", id, "--json")
	if err != nil {
		return bead{}, err
	}
	found, err := decodeBeads(out)
	if err != nil {
		return bead{}, fmt.Errorf("reading %s: %w", id, err)
	}
	if len(found) == 0 {
		return bead{}, fmt.Errorf("no bead %s in %s", id, g.vault)
	}
	return found[0], nil
}

// call runs one bd command in the vault and returns its standard output. Only
// one bd runs at a time: the database takes a single-writer lock.
func (g *Gateway) call(ctx context.Context, args ...string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	full := append([]string{"-C", g.vault}, args...)
	cmd := exec.CommandContext(ctx, Program, full...)
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	if err := cmd.Run(); err != nil {
		if said := said(out.Bytes(), errs.Bytes()); said != "" {
			return nil, fmt.Errorf("%s %s: %w: %s", Program, strings.Join(args, " "), err, said)
		}
		return nil, fmt.Errorf("%s %s: %w", Program, strings.Join(args, " "), err)
	}
	return out.Bytes(), nil
}

// said is what bd told us about a failure: its error object if it printed one,
// otherwise whatever it wrote, standard error first.
func said(out, errs []byte) string {
	for _, printed := range [][]byte{errs, out} {
		var reported struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(printed, &reported) == nil && reported.Error != "" {
			return reported.Error
		}
	}
	return strings.TrimSpace(string(errs) + "\n" + string(out))
}
