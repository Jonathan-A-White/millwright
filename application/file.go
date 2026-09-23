package application

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// File files one of the Mayor's plans: it reads the whole plan before it writes
// any of it, files the epic and its stories through the work tracker, prints
// the tree it filed, and releases the stories only if the plan is approved.
//
// Nothing is written until the whole plan is filable. A plan with one story
// that has no path, or stories that wait on each other, leaves the tracker
// exactly as it was found — half a plan in the tracker is worse than none,
// because the half that is there looks like work somebody meant.
//
// Every story is filed held, and only approval releases it. That is the point
// of the command: filing is cheap and reversible, dispatching is neither, and
// the gap between them is where the Governor says yes.
type File struct {
	Tracker WorkTracker

	// Out is where the tree is printed. A nil Out prints nothing.
	Out io.Writer

	// Approve is asked, once the tree has been printed, whether the stories may
	// be released. A nil Approve leaves them held — which is what an unattended
	// run does, since nobody is there to say yes.
	Approve func(context.Context, FiledPlan) (bool, error)
}

// FiledPlan is what came of filing a plan: the ids the tracker gave the epic
// and its stories, each story's Path and what it waits on, and whether they
// were released.
type FiledPlan struct {
	EpicID   string
	Title    string
	Defaults domain.Path
	Stories  []FiledStory
	// Epics are the epics filed directly under this one, which a plan read back
	// from the tracker may have and a plan just filed never does. They are not
	// stories: nothing here releases them, and the tree names them as epics.
	Epics    []FiledEpic
	Released bool
}

// FiledEpic is an epic found under a filed epic: its id, what it is called, and
// the word the tree uses for what the tracker says it is (see FiledStory.State).
// Its own stories are not read.
type FiledEpic struct {
	ID    string
	Title string
	State string
}

// FiledStory is one story as it was filed: the key it had in the plan, the id
// it has now, the Path it is worked by, the ids it still waits on, and what the
// tracker says it is now. A story just filed is held; a story read back later
// may be anything.
type FiledStory struct {
	ID              string
	Key             string
	Title           string
	Path            domain.Path
	EstimateMinutes int
	Needs           []string
	// State is the word the tree uses for what the tracker says this story is:
	// held, open, in progress or closed. An empty State reads as held, which is
	// what a story that has only just been filed is.
	State string
}

// state is what the tree calls this story, defaulting to held.
func (s FiledStory) state() string {
	if s.State == "" {
		return StateHeld
	}
	return s.State
}

// The words a tree uses for what the tracker says a story is. They are the
// factory's words, not beads': a story beads has deferred is one the factory is
// holding until somebody approves it.
const (
	StateHeld       = "held"
	StateOpen       = "open"
	StateInProgress = "in progress"
	StateClosed     = "closed"
)

// StateOf is the word a tree uses for a status the tracker reported. A status
// the factory does not know is printed as it came, rather than guessed at.
func StateOf(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return StateHeld
	case StatusHeld:
		return StateHeld
	case StatusOpen:
		return StateOpen
	case StatusInProgress:
		return StateInProgress
	case StatusClosed:
		return StateClosed
	}
	return status
}

// Run files the plan and reports what it filed.
func (f File) Run(ctx context.Context, plan domain.Plan) (FiledPlan, error) {
	if f.Tracker == nil {
		return FiledPlan{}, fmt.Errorf("filing a plan: there is no work tracker to file it in")
	}
	if err := plan.Validate(); err != nil {
		return FiledPlan{}, err
	}
	order, err := plan.Order()
	if err != nil {
		return FiledPlan{}, err
	}
	if err := f.validateFormulas(ctx, plan, order); err != nil {
		return FiledPlan{}, err
	}

	epicID, err := f.Tracker.CreateEpic(ctx, NewEpic{
		Title:           plan.Epic.Title,
		Description:     plan.Epic.Description,
		SuccessCriteria: plan.Epic.Success,
		Priority:        plan.Epic.Priority,
		Defaults:        plan.Epic.Defaults,
	})
	if err != nil {
		return FiledPlan{}, fmt.Errorf("filing the epic %q: %w", plan.Epic.Title, err)
	}
	filed := FiledPlan{EpicID: epicID, Title: plan.Epic.Title, Defaults: plan.Epic.Defaults}

	// The stories go in the order nothing comes before what it waits on, so
	// that a story can name what it waits on by the id it already has.
	ids := make(map[string]string, len(order))
	for _, story := range order {
		path, err := story.PathFrom(plan.Epic.Defaults)
		if err != nil {
			return filed, fmt.Errorf("filing story %s under %s: %w", story.Key, epicID, err)
		}
		needs := make([]string, 0, len(story.Needs))
		for _, need := range story.Needs {
			needs = append(needs, ids[need])
		}

		id, err := f.Tracker.CreateStory(ctx, NewStory{
			EpicID:          epicID,
			Title:           story.Title,
			Description:     story.Description,
			Acceptance:      story.Acceptance,
			Priority:        story.Priority,
			EstimateMinutes: story.Estimate,
			Overrides:       story.Overrides,
			Needs:           needs,
		})
		if err != nil {
			return filed, fmt.Errorf("filing story %s under %s (the epic and %d of its stories are filed and held): %w",
				story.Key, epicID, len(filed.Stories), err)
		}
		ids[story.Key] = id
		filed.Stories = append(filed.Stories, FiledStory{
			ID:              id,
			Key:             story.Key,
			Title:           story.Title,
			Path:            path,
			EstimateMinutes: story.Estimate,
			Needs:           needs,
			State:           StateHeld,
		})
	}

	f.print(filed.Tree())

	if f.Approve == nil {
		f.print(filed.holdings())
		return filed, nil
	}
	approved, err := f.Approve(ctx, filed)
	if err != nil {
		f.print(filed.holdings())
		return filed, fmt.Errorf("the plan is filed and held under %s, but nobody said whether to release it: %w", epicID, err)
	}
	if !approved {
		f.print(filed.holdings())
		return filed, nil
	}

	for _, story := range filed.Stories {
		if err := f.Tracker.ReleaseStory(ctx, story.ID); err != nil {
			return filed, fmt.Errorf("releasing story %s of %s: %w", story.ID, epicID, err)
		}
	}
	filed.Released = true
	f.print(filed.releases())
	return filed, nil
}

// validateFormulas refuses the whole plan, naming the story key, when a
// story's path names a formula this tracker has not installed — before
// anything is written, the same as plan.Validate does for the rest of a
// path. The tracker is asked only when some story in the plan names a
// formula at all, so a plan that never mentions one costs nothing extra.
func (f File) validateFormulas(ctx context.Context, plan domain.Plan, order []domain.PlanStory) error {
	var needsCheck bool
	for _, story := range order {
		path, err := story.PathFrom(plan.Epic.Defaults)
		if err == nil && path.Formula != "" {
			needsCheck = true
			break
		}
	}
	if !needsCheck {
		return nil
	}

	installed, err := f.Tracker.Formulas(ctx)
	if err != nil {
		return fmt.Errorf("checking which formulas are installed: %w", err)
	}
	for _, story := range order {
		path, err := story.PathFrom(plan.Epic.Defaults)
		if err != nil {
			continue
		}
		if err := path.ValidateFormula(installed); err != nil {
			return fmt.Errorf("filing story %s: %w", story.Key, err)
		}
	}
	return nil
}

// print writes one block of the report, when there is somewhere to write it.
func (f File) print(block string) {
	if f.Out == nil {
		return
	}
	fmt.Fprint(f.Out, block)
}

// Tree is the plan as a person reads it: the epic and the default Path its
// stories inherit, then every story with the Path it is worked by, what it
// still waits on, and what the tracker says it is. It is printed by the command
// that files a plan and by the one that releases it later, so that the two show
// the same thing.
func (p FiledPlan) Tree() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n", p.EpicID, p.Title)
	fmt.Fprintf(&b, "    default path %s\n\n", p.Defaults.Summary())

	for _, story := range p.Stories {
		fmt.Fprintf(&b, "  %s · %s\n", story.ID, story.Title)
		fmt.Fprintf(&b, "      path %s\n", story.Path.Summary())

		state := []string{story.state()}
		if story.EstimateMinutes > 0 {
			state = append(state, fmt.Sprintf("%dm", story.EstimateMinutes))
		}
		if len(story.Needs) == 0 {
			state = append(state, "waits on nothing")
		} else {
			state = append(state, "waits on "+strings.Join(story.Needs, ", "))
		}
		fmt.Fprintf(&b, "      %s\n", strings.Join(state, " · "))
	}
	for _, epic := range p.Epics {
		fmt.Fprintf(&b, "\n  %s · %s\n", epic.ID, epic.Title)
		fmt.Fprintf(&b, "      an epic, %s: its stories are not shown here, mw show %s prints them\n", epic.State, epic.ID)
	}
	return b.String()
}

// Unblocked reports the ids of the stories that wait on nothing: the ones a
// release makes ready straight away.
func (p FiledPlan) Unblocked() []string {
	var free []string
	for _, story := range p.Stories {
		if len(story.Needs) == 0 {
			free = append(free, story.ID)
		}
	}
	return free
}

// holdings is what a person is told about a plan nobody released: that none of
// it can be dispatched, and how to release it without filing it twice.
func (p FiledPlan) holdings() string {
	return fmt.Sprintf("\nAll %d stories of %s are held: nothing here can be dispatched.\n"+
		"Filing this plan again would file a second copy of it, so when it is approved, release this one:\n"+
		"  mw release %s\n", len(p.Stories), p.EpicID, p.EpicID)
}

// releases is what a person is told about a plan that was approved: what was
// released, and what a dispatcher can take right now.
func (p FiledPlan) releases() string {
	ready := p.Unblocked()
	if len(ready) == 0 {
		return fmt.Sprintf("\nReleased all %d stories of %s. None of them is ready: they all wait on another.\n",
			len(p.Stories), p.EpicID)
	}
	return fmt.Sprintf("\nReleased all %d stories of %s. Ready now: %s.\n",
		len(p.Stories), p.EpicID, strings.Join(ready, ", "))
}
