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
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// Program is the beads command this package shells out to.
const Program = "bd"

// Beads' own names for the statuses the factory puts a story in: claimed, held
// back from every dispatcher, released to them again, and finished.
const (
	StatusInProgress = application.StatusInProgress
	StatusDeferred   = application.StatusHeld
	StatusOpen       = application.StatusOpen
	StatusClosed     = application.StatusClosed
)

// TypeEpic is beads' name for the type of bead an epic is filed as.
const TypeEpic = "epic"

// Gateway reads and writes stories in one vault's beads database.
type Gateway struct {
	vault   string
	program string
	mu      sync.Mutex
}

// Gateway satisfies the port.
var _ application.WorkTracker = (*Gateway)(nil)

// Option is a setting of a Gateway, given to New.
type Option func(*Gateway)

// WithProgram names the beads command to run, for a host that keeps it
// somewhere unusual — and for a test that needs a stand-in for bd rather than
// the real thing.
func WithProgram(program string) Option {
	return func(g *Gateway) { g.program = program }
}

// New returns a Gateway onto the beads database in a vault directory.
func New(vault string, opts ...Option) *Gateway {
	g := &Gateway{vault: vault, program: Program}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// FromConfig returns a Gateway onto the vault this host is configured with.
func FromConfig(opts ...Option) (*Gateway, error) {
	vault, err := config.Vault()
	if err != nil {
		return nil, err
	}
	return New(vault, opts...), nil
}

// Vault reports the directory this Gateway runs bd in.
func (g *Gateway) Vault() string { return g.vault }

// Available reports whether the beads command is on PATH. Tests that need a
// real database skip themselves when it is not.
func Available() bool {
	_, err := exec.LookPath(Program)
	return err == nil
}

// CreateEpic implements application.WorkTracker. An epic's success criteria go
// in a section of its description rather than in the acceptance field: that is
// where `bd lint` looks for them, and where the epics already in the factory's
// tracker carry them.
func (g *Gateway) CreateEpic(ctx context.Context, epic application.NewEpic) (string, error) {
	if strings.TrimSpace(epic.Title) == "" {
		return "", fmt.Errorf("an epic needs a title")
	}
	args := []string{"create", epic.Title, "--type", "epic"}

	description := strings.TrimSpace(epic.Description)
	if criteria := strings.TrimSpace(epic.SuccessCriteria); criteria != "" {
		description = strings.TrimSpace(description + "\n\n## Success Criteria\n\n" + criteria)
	}
	if description != "" {
		args = append(args, "--description", description)
	}
	if epic.Priority > 0 {
		args = append(args, "--priority", strconv.Itoa(epic.Priority))
	}
	metadata, err := metadataJSON(epic.Defaults.Metadata())
	if err != nil {
		return "", fmt.Errorf("writing the default path of the epic %q: %w", epic.Title, err)
	}
	if metadata != "" {
		args = append(args, "--metadata", metadata)
	}
	return g.created(ctx, "the epic "+epic.Title, args)
}

// CreateStory implements application.WorkTracker. The story is created
// deferred, which is beads' own way of holding work back: a deferred story is
// in the database, with its path, its acceptance criteria and everything it
// waits on, and `bd ready` will not offer it to anybody until it is released.
func (g *Gateway) CreateStory(ctx context.Context, story application.NewStory) (string, error) {
	switch {
	case strings.TrimSpace(story.Title) == "":
		return "", fmt.Errorf("a story needs a title")
	case strings.TrimSpace(story.EpicID) == "":
		return "", fmt.Errorf("the story %q needs an epic to be filed under", story.Title)
	}
	args := []string{"create", story.Title, "--parent", story.EpicID, "--status", StatusDeferred}

	if description := strings.TrimSpace(story.Description); description != "" {
		args = append(args, "--description", description)
	}
	if acceptance := strings.TrimSpace(story.Acceptance); acceptance != "" {
		args = append(args, "--acceptance", acceptance)
	}
	if story.Priority > 0 {
		args = append(args, "--priority", strconv.Itoa(story.Priority))
	}
	if story.EstimateMinutes > 0 {
		args = append(args, "--estimate", strconv.Itoa(story.EstimateMinutes))
	}
	metadata, err := metadataJSON(story.Overrides.Metadata())
	if err != nil {
		return "", fmt.Errorf("writing the path of the story %q: %w", story.Title, err)
	}
	if metadata != "" {
		args = append(args, "--metadata", metadata)
	}
	for _, need := range story.Needs {
		args = append(args, "--deps", "blocked-by:"+need)
	}
	return g.created(ctx, "the story "+story.Title, args)
}

// ReleaseStory implements application.WorkTracker: a held story becomes an open
// one, and beads offers it as soon as nothing blocks it.
func (g *Gateway) ReleaseStory(ctx context.Context, id string) error {
	_, err := g.call(ctx, "update", id, "--status", StatusOpen)
	return err
}

// created runs one `bd create` and reads back the id it gave what it created.
func (g *Gateway) created(ctx context.Context, what string, args []string) (string, error) {
	out, err := g.call(ctx, append(args, "--silent")...)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("%s created %s without saying what id it gave it", g.program, what)
	}
	return id, nil
}

// metadataJSON is the metadata as bd's --metadata flag takes it, or "" when
// there is none to write.
func metadataJSON(fields map[string]string) (string, error) {
	if len(fields) == 0 {
		return "", nil
	}
	written, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	return string(written), nil
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

// ShowEpic implements application.WorkTracker: an epic as it stands, with the
// default Path its stories inherit and every story filed under it — closed and
// claimed ones included, which is why the listing asks bd for everything rather
// than take its default of the unfinished. It only reads; releasing is a
// separate call per story, so that a failure half way through has released
// exactly the stories it says it did.
func (g *Gateway) ShowEpic(ctx context.Context, id string) (application.EpicDetail, error) {
	if strings.TrimSpace(id) == "" {
		return application.EpicDetail{}, fmt.Errorf("reading an epic: which epic?")
	}
	epic, err := g.showOne(ctx, id)
	if err != nil {
		return application.EpicDetail{}, err
	}
	if epic.Type != "" && epic.Type != TypeEpic {
		return application.EpicDetail{}, fmt.Errorf("%s is a %s, not an epic", id, epic.Type)
	}
	defaults := domain.PathFromMetadata(epic.pathMetadata())

	out, err := g.call(ctx, "list", "--parent", id, "--limit", "0", "--all", "--json")
	if err != nil {
		return application.EpicDetail{}, err
	}
	stories, err := decodeBeads(out)
	if err != nil {
		return application.EpicDetail{}, fmt.Errorf("reading the stories of %s: %w", id, err)
	}

	filed := application.EpicDetail{ID: epic.ID, Title: epic.Title, Defaults: defaults}
	for _, story := range inFiledOrder(stories) {
		filed.Stories = append(filed.Stories, story.detail(defaults))
	}
	return filed, nil
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

// call runs one bd command in the vault and returns its standard output, or
// what bd said about why it would not.
func (g *Gateway) call(ctx context.Context, args ...string) ([]byte, error) {
	out, errs, err := g.run(ctx, args...)
	if err != nil {
		if said := said(out, errs); said != "" {
			return nil, fmt.Errorf("%s %s: %w: %s", g.program, strings.Join(args, " "), err, said)
		}
		return nil, fmt.Errorf("%s %s: %w", g.program, strings.Join(args, " "), err)
	}
	return out, nil
}

// run runs one bd command in the vault and hands back both streams and the
// unwrapped error, so that a caller that cares about bd's exit code can read
// it. Only one bd runs at a time: the database takes a single-writer lock.
func (g *Gateway) run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	full := append([]string{"-C", g.vault}, args...)
	cmd := exec.CommandContext(ctx, g.program, full...)
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	err := cmd.Run()
	return out.Bytes(), errs.Bytes(), err
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
