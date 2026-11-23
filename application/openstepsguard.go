package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The close-out guard (mw-gq6.1676). A Builder's session is launched with a Stop
// hook that runs `mw step guard <story>`: Claude Code runs it each time the
// session is about to end, and when it exits 2 with a message on its standard
// error, the session goes on and reads the message. The guard lists the story's
// open formula steps; the Builder closes each one it has done, or marks it the
// Governor's, with the reason only he can do it (`mw step governor`). Without
// the guard, a Builder that ended with a step open cost a whole second attempt:
// the close-out refuses ReasonOpenSteps. The guard blocks at most
// OpenStepsGuardMaxBlocks times a session, and then lets it end, and the
// close-out refuses as it always did.
//
// Claude only: Codex's `codex exec` launch has no Stop hook.

// OpenStepsGuardMaxBlocks is the most times the guard holds one session back.
const OpenStepsGuardMaxBlocks = 3

// GovernorsStepPrefix begins the note a step marked the Governor's is kept in:
// governors-step.<story>.<step>, holding a GovernorsStep's JSON. It is not under
// "hands.", whose notes every reader of the hands steps parses as steps to run:
// a marked step has no command, nothing to approve and nothing to run.
const GovernorsStepPrefix = "governors-step."

// GovernorsStepKey is the note a marked step is kept in.
func GovernorsStepKey(story, step string) string { return GovernorsStepPrefix + story + "." + step }

// GovernorsStep is an open formula step its Builder marked as the Governor's:
// the story, the step's id and why only he can do it.
type GovernorsStep struct {
	Story  string `json:"story"`
	Step   string `json:"step"`
	Reason string `json:"reason"`
	At     string `json:"at"`
}

// GuardNotes is the tracker's notes, as the guard and the mark read them.
type GuardNotes interface {
	NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error)
}

// GovernorsStepNotes is GuardNotes that can also keep a note.
type GovernorsStepNotes interface {
	GuardNotes
	SetNote(ctx context.Context, key, value string) error
}

// GuardTracker is what the guard and the mark read of a story: its formula's
// root, and the steps of it that are open.
type GuardTracker interface {
	ShowStory(ctx context.Context, id string) (StoryDetail, error)
	OpenSteps(ctx context.Context, moleculeID string) ([]FormulaStep, error)
}

// GuardRecords is where the guard counts its blocks: a file under the story's
// run directory.
type GuardRecords interface {
	ReadRunFile(ctx context.Context, storyID, name string) (string, error)
	PutRunFile(ctx context.Context, storyID, name, contents string) (string, error)
}

// parseGovernorsStep reads a marked step's note.
func parseGovernorsStep(raw string) (GovernorsStep, bool) {
	var step GovernorsStep
	if err := json.Unmarshal([]byte(raw), &step); err != nil || step.Story == "" || step.Step == "" {
		return GovernorsStep{}, false
	}
	return step, true
}

// GovernorsSteps are the steps of a story marked the Governor's, by step id.
func GovernorsSteps(ctx context.Context, notes GuardNotes, story string) (map[string]GovernorsStep, error) {
	read, err := notes.NotesWithPrefix(ctx, GovernorsStepPrefix)
	if err != nil {
		return nil, fmt.Errorf("reading the steps marked the Governor's: %w", err)
	}
	marked := map[string]GovernorsStep{}
	for _, raw := range read {
		if step, ok := parseGovernorsStep(raw); ok && step.Story == story {
			marked[step.Step] = step
		}
	}
	return marked, nil
}

// openStepsOf is the open steps of a story's formula; none when it has none
// poured.
func openStepsOf(ctx context.Context, tracker GuardTracker, story string) (root string, open []FormulaStep, err error) {
	detail, err := tracker.ShowStory(ctx, story)
	if err != nil {
		return "", nil, fmt.Errorf("reading %s: %w", story, err)
	}
	if root = detail.Molecule.RootID; root == "" {
		return "", nil, nil
	}
	open, err = tracker.OpenSteps(ctx, root)
	if err != nil {
		return "", nil, fmt.Errorf("reading the steps of %s: %w", root, err)
	}
	return root, open, nil
}

// OpenStepsGuard is what `mw step guard <story>` runs when a Builder's session
// is about to end.
type OpenStepsGuard struct {
	Tracker GuardTracker
	Notes   GuardNotes
	Records GuardRecords
	// Out is where the message to the session is written: the standard error of
	// the hook, which Claude Code gives the session when the hook exits 2.
	Out io.Writer
	// Vault is the directory bd is pointed at in the message's close command.
	Vault string
}

// guardInput is what the hook is given on its standard input (the Stop event's
// JSON): the guard reads the session only. A document that is not that is a
// session of its own, since the input is not what decides a step is open.
type guardInput struct {
	SessionID string `json:"session_id"`
}

var notFileSafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// guardCountFile is the file the session's blocks are counted in.
func guardCountFile(session string) string {
	session = notFileSafe.ReplaceAllString(session, "")
	if len(session) > 64 {
		session = session[:64]
	}
	if session == "" {
		session = "session"
	}
	return "open-steps-guard." + session
}

// Run says whether the session is held back: true when the story has a formula
// step open that is neither closed nor marked the Governor's, the session has
// been held fewer than OpenStepsGuardMaxBlocks times, and the message has been
// written. A step list that cannot be read is an error and no block: a session
// is not kept from ending for what mw could not see.
func (g OpenStepsGuard) Run(ctx context.Context, story string, stdin io.Reader) (blocked bool, err error) {
	var input guardInput
	if raw, readErr := io.ReadAll(io.LimitReader(stdin, 1<<20)); readErr == nil {
		_ = json.Unmarshal(raw, &input)
	}
	root, open, err := openStepsOf(ctx, g.Tracker, story)
	if err != nil {
		return false, err
	}
	marked, err := GovernorsSteps(ctx, g.Notes, story)
	if err != nil {
		return false, err
	}
	var left []FormulaStep
	for _, step := range open {
		if _, governors := marked[step.ID]; !governors {
			left = append(left, step)
		}
	}
	if len(left) == 0 {
		return false, nil
	}

	name := guardCountFile(input.SessionID)
	blocks := 0
	if text, readErr := g.Records.ReadRunFile(ctx, story, name); readErr == nil {
		blocks, _ = strconv.Atoi(strings.TrimSpace(text))
	}
	if blocks >= OpenStepsGuardMaxBlocks {
		return false, nil
	}
	if _, err := g.Records.PutRunFile(ctx, story, name, strconv.Itoa(blocks+1)+"\n"); err != nil {
		return false, fmt.Errorf("counting the guard's blocks of %s: %w", story, err)
	}
	fmt.Fprint(g.Out, g.message(story, root, left, blocks+1))
	return true, nil
}

// message is what the session is told: each open step by id and title, and the
// two ways to leave it.
func (g OpenStepsGuard) message(story, root string, open []FormulaStep, block int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Do not end yet: %d formula step(s) of %s are still open (block %d of %d; after the last the session may end and the close-out refuses it).\n",
		len(open), root, block, OpenStepsGuardMaxBlocks)
	b.WriteString(stepList(open) + "\n\n")
	bd := "bd"
	if g.Vault != "" {
		bd = "bd -C " + g.Vault
	}
	b.WriteString("For each one: if you have done it, close it (`" + bd + " close <step>`), after running what it asks. ")
	b.WriteString("If only the Governor's own hands can do it, mark it the Governor's with the reason only he can do it (`mw step governor " + story + " <step> --why \"<reason>\"`).\n")
	if len(open) > 0 {
		fmt.Fprintf(&b, "For %s: `%s close %s` or `mw step governor %s %s --why \"<reason>\"`.\n", open[0].ID, bd, open[0].ID, story, open[0].ID)
	}
	b.WriteString("A step you did not do and cannot name a reason for is a step to do now, not to leave.\n")
	return b.String()
}

// GovernorsStepMark is `mw step governor`: it marks an open step of a story's
// formula as the Governor's, with the reason.
type GovernorsStepMark struct {
	Tracker GuardTracker
	Notes   GovernorsStepNotes
	Now     func() time.Time
}

// Run keeps the mark. The step must be one the story's formula has open, and
// the reason must say something.
func (m GovernorsStepMark) Run(ctx context.Context, story, step, reason string) (GovernorsStep, error) {
	reason = oneLine(reason)
	if strings.TrimSpace(reason) == "" {
		return GovernorsStep{}, fmt.Errorf("marking %s the Governor's needs the reason only he can do it: --why \"<reason>\"", step)
	}
	root, open, err := openStepsOf(ctx, m.Tracker, story)
	if err != nil {
		return GovernorsStep{}, err
	}
	found := false
	for _, s := range open {
		found = found || s.ID == step
	}
	if !found {
		return GovernorsStep{}, fmt.Errorf("%s is not an open step of %s's formula (%s): close it, or name one of %s",
			step, story, formulaOrNone(root), stepIDs(open))
	}
	marked := GovernorsStep{Story: story, Step: step, Reason: reason, At: m.Now().UTC().Format(time.RFC3339)}
	raw, err := json.Marshal(marked)
	if err != nil {
		return GovernorsStep{}, err
	}
	if err := m.Notes.SetNote(ctx, GovernorsStepKey(story, step), string(raw)); err != nil {
		return GovernorsStep{}, fmt.Errorf("keeping the mark of %s: %w", step, err)
	}
	return marked, nil
}

func formulaOrNone(root string) string {
	if root == "" {
		return "no formula poured"
	}
	return root
}

func stepIDs(steps []FormulaStep) string {
	ids := make([]string, 0, len(steps))
	for _, s := range steps {
		ids = append(ids, s.ID)
	}
	return strings.Join(ids, " ")
}

// governorsSaid is the words a refusal for open steps ends its reason on when
// some of the open steps are marked the Governor's: each, with the reason the
// session gave, so that the Mayor's mail names them. Empty when none is marked,
// or the notes cannot be read: the refusal stands as it was.
func (n Next) governorsSaid(ctx context.Context, story string, open []FormulaStep) string {
	notes, ok := n.Tracker.(GuardNotes)
	if !ok {
		return ""
	}
	marked, err := GovernorsSteps(ctx, notes, story)
	if err != nil || len(marked) == 0 {
		return ""
	}
	var said []string
	for _, step := range open {
		if m, governors := marked[step.ID]; governors {
			said = append(said, fmt.Sprintf("%s · %s: %s", step.ID, strings.TrimSpace(step.Title), m.Reason))
		}
	}
	if len(said) == 0 {
		return ""
	}
	return fmt.Sprintf("; waiting on the Governor, marked his by the session: %s", strings.Join(said, "; "))
}

// currentOpenSteps tells whether a mark of a step is of the story's current
// formula, as the report counts the stories that wait on the Governor. A retry
// pours a fresh formula, and a mark of the earlier attempt's step is none of
// its steps (mw-gq6.1680). A mark counts when it cannot be told otherwise: the
// tracker reads no steps, the story has no formula or is not known, or the
// formula has no step open (a story that went on to land closed them all).
type currentOpenSteps struct {
	tracker any
	root    func(story string) string
	read    map[string][]FormulaStep
}

func (c *currentOpenSteps) has(ctx context.Context, mark GovernorsStep) (bool, error) {
	steps, ok := c.tracker.(interface {
		OpenSteps(ctx context.Context, moleculeID string) ([]FormulaStep, error)
	})
	root := c.root(mark.Story)
	if !ok || root == "" {
		return true, nil
	}
	open, seen := c.read[root]
	if !seen {
		var err error
		if open, err = steps.OpenSteps(ctx, root); err != nil {
			return false, fmt.Errorf("reading the steps of %s: %w", root, err)
		}
		if c.read == nil {
			c.read = map[string][]FormulaStep{}
		}
		c.read[root] = open
	}
	if len(open) == 0 {
		return true, nil
	}
	for _, s := range open {
		if s.ID == mark.Step {
			return true, nil
		}
	}
	return false, nil
}
