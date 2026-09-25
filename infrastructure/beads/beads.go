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
	"syscall"
	"time"

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
	actor   string
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

// WithActor names who beads records as the actor of everything this Gateway
// does. It is passed to bd on every call, rather than left to $BEADS_ACTOR,
// because mw is run from several environments that carry several names — a
// dispatcher's shell, a timer, a Builder session signed as its own seat — and
// bd lets only the actor that claimed a story close it. A Gateway with no name
// leaves bd to its own default, which is what a person running bd by hand gets.
func WithActor(actor string) Option {
	return func(g *Gateway) { g.actor = strings.TrimSpace(actor) }
}

// New returns a Gateway onto the beads database in a vault directory.
func New(vault string, opts ...Option) *Gateway {
	g := &Gateway{vault: vault, program: Program}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// FromConfig returns a Gateway onto the vault this host is configured with,
// acting under this host's name for mw — mw@<host>. A host that does not say
// which host it is is a plain refusal here, before any bd is started: mw writes
// under one name or it does not write.
func FromConfig(opts ...Option) (*Gateway, error) {
	vault, err := config.Vault()
	if err != nil {
		return nil, err
	}
	actor, err := ActorFromConfig()
	if err != nil {
		return nil, err
	}
	return New(vault, append([]Option{WithActor(actor)}, opts...)...), nil
}

// ActorFromConfig is the name mw acts under on this host: the mw seat on the
// host the config file names. The error is the one config.Host gives, which
// says how to set it.
func ActorFromConfig() (string, error) {
	host, err := config.Host()
	if err != nil {
		return "", err
	}
	return application.SeatIdentity(application.MwSeat, host), nil
}

// Vault reports the directory this Gateway runs bd in.
func (g *Gateway) Vault() string { return g.vault }

// Actor reports the name beads records for everything this Gateway does, or ""
// when it leaves that to bd.
func (g *Gateway) Actor() string { return g.actor }

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

	filed := application.EpicDetail{
		ID: epic.ID, Title: epic.Title, Status: epic.Status, Priority: epic.priority(), Defaults: defaults,
	}
	for _, story := range inFiledOrder(stories) {
		filed.Stories = append(filed.Stories, story.detail(defaults))
	}
	return filed, nil
}

// LiveEpics implements application.WorkTracker: every epic bd has open or in
// progress, oldest filed first, whatever it is nested under.
func (g *Gateway) LiveEpics(ctx context.Context) ([]string, error) {
	out, err := g.call(ctx, "list", "--type", TypeEpic, "--status", StatusOpen+","+StatusInProgress, "--limit", "0", "--json")
	if err != nil {
		return nil, err
	}
	beads, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading the live epics: %w", err)
	}
	beads = inFiledOrder(beads)
	ids := make([]string, 0, len(beads))
	for _, b := range beads {
		ids = append(ids, b.ID)
	}
	return ids, nil
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

// ClaimStory implements application.WorkTracker. `bd update --claim` is
// already the conditional claim: it takes a story nobody holds, is harmless on
// one this actor holds, and refuses one another actor holds, writing nothing —
// and it is the only update that grants a lease, which is why it is not
// `--if-assignee` (bd will not combine the two). A refusal is told apart from
// any other failure by reading the story back rather than by bd's wording: a
// story found in progress under an assignee is held by someone else, since
// bd would have let this actor's own claim stand.
func (g *Gateway) ClaimStory(ctx context.Context, id string) error {
	_, err := g.call(ctx, "update", id, "--claim")
	if err == nil {
		return nil
	}
	story, readErr := g.showOne(ctx, id)
	if readErr == nil && story.Status == StatusInProgress && story.Assignee != "" && story.Assignee != g.actor {
		return &application.ClaimHeldError{ID: id, Holder: story.Assignee}
	}
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

// StoryComments implements application.WorkTracker. bd lists them oldest first;
// they are put in that order here too, by the time each says it was left, so
// that a caller taking the newest never depends on it.
func (g *Gateway) StoryComments(ctx context.Context, id string) ([]application.Comment, error) {
	out, err := g.call(ctx, "comments", id, "--json")
	if err != nil {
		return nil, err
	}
	comments, err := decodeComments(out)
	if err != nil {
		return nil, fmt.Errorf("reading the comments of %s: %w", id, err)
	}
	return comments, nil
}

// CloseStory implements application.WorkTracker.
func (g *Gateway) CloseStory(ctx context.Context, id, reason string) error {
	_, err := g.call(ctx, "close", id, "--reason", reason)
	return err
}

// StaleClaims implements application.WorkTracker. It reads what is claimed,
// as RunningStories does, and keeps the claims whose lease bd says had run out
// by now. A claim bd shows no lease for is never listed: one made before bd
// kept leases, or — bd's heartbeat help calls leases node-local — one made on
// the other host. This host cannot say when that one was last heard from.
func (g *Gateway) StaleClaims(ctx context.Context, now time.Time) ([]application.StoryDetail, error) {
	out, err := g.call(ctx, "list", "--status", StatusInProgress, "--exclude-type", "epic", "--limit", "0", "--json")
	if err != nil {
		return nil, err
	}
	claimed, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading the stale claims: %w", err)
	}
	var lapsed []bead
	for _, story := range claimed {
		if expires := story.leaseExpires(); !expires.IsZero() && now.After(expires) {
			lapsed = append(lapsed, story)
		}
	}
	return g.overlaid(ctx, lapsed)
}

// HeartbeatClaim implements application.WorkTracker. bd refuses a heartbeat
// from anyone but the claim's holder, and on a story no longer in progress.
func (g *Gateway) HeartbeatClaim(ctx context.Context, id string) error {
	_, err := g.call(ctx, "heartbeat", id)
	return err
}

// ReclaimStory implements application.WorkTracker. `bd reclaim` itself judges
// whether the lease has run out, so it is asked for no grace beyond that
// (--older-than 0s) and for exactly this story; what it reports reclaimed says
// whether it did. It is never given --any-replica, which bd warns reverts a
// lease another replica granted however alive its holder is: who may reclaim
// the other host's claims is not settled.
func (g *Gateway) ReclaimStory(ctx context.Context, id string) (bool, error) {
	out, err := g.call(ctx, "reclaim", "--older-than", "0s", "--id", id, "--json")
	if err != nil {
		return false, err
	}
	var report struct {
		Reclaimed []struct {
			ID string `json:"id"`
		} `json:"reclaimed"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		return false, fmt.Errorf("reading what bd reclaimed of %s: %w", id, err)
	}
	for _, reclaimed := range report.Reclaimed {
		if reclaimed.ID == id {
			return true, nil
		}
	}
	return false, nil
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

// killGrace is how long a bd that has been told to stop is given to let go of
// its output before mw stops waiting for it.
const killGrace = time.Second

// run runs one bd command in the vault and hands back both streams and the
// unwrapped error, so that a caller that cares about bd's exit code can read
// it. Only one bd runs at a time: the database takes a single-writer lock.
//
// bd runs in a process group of its own, so that a context that ends — mw
// told to stop by a signal, or a dispatch tick giving up — takes with it not
// only bd but the git it may have started underneath (`bd sync` talks to the
// remote through git itself), rather than leaving them behind for the next
// tick to trip over.
func (g *Gateway) run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	full := []string{"-C", g.vault}
	if g.actor != "" {
		// --actor is a flag of every bd subcommand, and naming it on reads as
		// well as writes keeps one rule rather than a list of which calls write.
		full = append(full, "--actor", g.actor)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, g.program, full...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = killGrace
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	err := cmd.Run()
	if err != nil && ctx.Err() != nil {
		// The context ended, not bd: this is mw stopping, not bd failing, and the
		// exit code underneath (a process this host just killed) is not bd's own
		// to be faithful to.
		err = fmt.Errorf("stopped: %w", ctx.Err())
	}
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
