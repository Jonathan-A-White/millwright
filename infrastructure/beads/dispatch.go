package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// ReadyForHost implements application.WorkTracker. bd is asked once for
// everything ready and unclaimed anywhere in the database; each story's epic is
// then read for the defaults it inherits, once per epic however many stories
// hang from it.
//
// A story whose Path names no host is not offered to any host. A dispatcher
// that took one would be guessing, and the cheapest guess to get wrong on a
// two-host factory is which machine a story belongs on.
func (g *Gateway) ReadyForHost(ctx context.Context, host string) ([]application.StoryDetail, error) {
	if host == "" {
		return nil, fmt.Errorf("which host are the ready stories for?")
	}
	out, err := g.call(ctx, "ready", "--unassigned", "--exclude-type", "epic", "--json")
	if err != nil {
		return nil, err
	}
	stories, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading what is ready on %s: %w", host, err)
	}
	return g.onHost(ctx, stories, host)
}

// RunningStories implements application.WorkTracker: what this host has in
// flight, which is what a concurrency cap counts.
func (g *Gateway) RunningStories(ctx context.Context, host string) ([]application.StoryDetail, error) {
	if host == "" {
		return nil, fmt.Errorf("which host's running stories?")
	}
	out, err := g.call(ctx, "list", "--status", StatusInProgress, "--exclude-type", "epic", "--limit", "0", "--json")
	if err != nil {
		return nil, err
	}
	stories, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading what is running on %s: %w", host, err)
	}
	return g.onHost(ctx, stories, host)
}

// onHost narrows beads to the stories worked on one host, with each story's
// epic defaults overlaid. Every distinct epic is read once.
func (g *Gateway) onHost(ctx context.Context, stories []bead, host string) ([]application.StoryDetail, error) {
	defaults := map[string]domain.Path{}
	var on []application.StoryDetail

	for _, story := range stories {
		path, found := story.parentPath()
		if !found && story.Parent != "" {
			var known bool
			if path, known = defaults[story.Parent]; !known {
				epic, err := g.showOne(ctx, story.Parent)
				if err != nil {
					return nil, fmt.Errorf("reading the epic %s of story %s: %w", story.Parent, story.ID, err)
				}
				path = domain.PathFromMetadata(epic.pathMetadata())
				defaults[story.Parent] = path
			}
		}
		detail := story.detail(path)
		if merged := detail.Merged().Host; merged == "" || merged != host {
			continue
		}
		on = append(on, detail)
	}
	return on, nil
}

// ReleaseClaim implements application.WorkTracker. The story goes back to open
// and to nobody: bd offers only unassigned stories as ready, so leaving the
// assignee on a released story would hide it from every dispatcher.
func (g *Gateway) ReleaseClaim(ctx context.Context, id string) error {
	_, err := g.call(ctx, "update", id, "--status", StatusOpen, "--assignee", "")
	return err
}

// SetStoryState implements application.WorkTracker. bd records the change as an
// event bead and keeps a `<dimension>:<value>` label on the story for looking
// it up cheaply — so a person, or a later mw, can see at a glance what this
// story is doing without reading its history.
func (g *Gateway) SetStoryState(ctx context.Context, id, dimension, value, reason string) error {
	if dimension == "" {
		return fmt.Errorf("recording the state of %s: which dimension?", id)
	}
	args := []string{"set-state", id, dimension + "=" + value}
	if reason != "" {
		args = append(args, "--reason", reason)
	}
	_, err := g.call(ctx, args...)
	return err
}

// Formulas implements application.WorkTracker: the formulas bd can pour here,
// which are the files installed under the database's own formulas directory.
func (g *Gateway) Formulas(ctx context.Context) ([]string, error) {
	out, err := g.call(ctx, "formula", "list", "--json")
	if err != nil {
		return nil, err
	}
	var installed []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &installed); err != nil {
		return nil, fmt.Errorf("reading the installed formulas: %w", err)
	}
	names := make([]string, 0, len(installed))
	for _, formula := range installed {
		if formula.Name != "" {
			names = append(names, formula.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// PourFormula implements application.WorkTracker. Pouring is two calls: bd
// makes the beads and says which id it gave each step, and then the steps are
// read back for what they say, because the pouring says only their ids.
func (g *Gateway) PourFormula(ctx context.Context, formula, storyID, title string) (application.Molecule, error) {
	switch {
	case strings.TrimSpace(formula) == "":
		return application.Molecule{}, fmt.Errorf("pouring a formula for %s: which formula?", storyID)
	case strings.TrimSpace(storyID) == "":
		return application.Molecule{}, fmt.Errorf("pouring %s: which story?", formula)
	}

	out, err := g.call(ctx, "mol", "pour", formula,
		"--var", "story="+storyID, "--var", "title="+title, "--json")
	if err != nil {
		return application.Molecule{}, err
	}
	var poured struct {
		Root string `json:"new_epic_id"`
	}
	if err := json.Unmarshal(out, &poured); err != nil {
		return application.Molecule{}, fmt.Errorf("reading what pouring %s for %s made: %w", formula, storyID, err)
	}
	if poured.Root == "" {
		return application.Molecule{}, fmt.Errorf("%s poured %s for %s without saying what it made", g.program, formula, storyID)
	}

	steps, err := g.steps(ctx, poured.Root)
	if err != nil {
		return application.Molecule{}, fmt.Errorf("reading the steps of %s poured for %s: %w", formula, storyID, err)
	}
	return application.Molecule{Formula: formula, RootID: poured.Root, Steps: steps}, nil
}

// steps reads a molecule's step beads and puts them in the order they are
// worked. bd lists them in no particular order, but each step waits on the one
// before it, so the order is in the dependencies.
func (g *Gateway) steps(ctx context.Context, root string) ([]application.FormulaStep, error) {
	out, err := g.call(ctx, "list", "--parent", root, "--limit", "0", "--json")
	if err != nil {
		return nil, err
	}
	poured, err := decodeBeads(out)
	if err != nil {
		return nil, err
	}
	return inWorkedOrder(poured), nil
}

// inWorkedOrder sorts a molecule's steps into the order they are worked: a step
// comes after every sibling it waits on. Steps that wait on nothing, and steps
// whose waiting cannot be untangled, come out in id order, so that the result
// is the same however bd happened to list them.
func inWorkedOrder(steps []bead) []application.FormulaStep {
	sort.Slice(steps, func(i, j int) bool { return steps[i].ID < steps[j].ID })

	sibling := make(map[string]bool, len(steps))
	for _, step := range steps {
		sibling[step.ID] = true
	}
	waitingOn := make(map[string][]string, len(steps))
	for _, step := range steps {
		for _, edge := range step.edges() {
			if edge.Kind != "parent-child" && sibling[edge.DependsOn] && edge.Issue == step.ID {
				waitingOn[step.ID] = append(waitingOn[step.ID], edge.DependsOn)
			}
		}
	}

	done := make(map[string]bool, len(steps))
	ordered := make([]application.FormulaStep, 0, len(steps))
	for len(ordered) < len(steps) {
		took := false
		for _, step := range steps {
			if done[step.ID] || !ready(waitingOn[step.ID], done) {
				continue
			}
			done[step.ID] = true
			ordered = append(ordered, application.FormulaStep{
				ID: step.ID, Title: step.Title, Description: step.Description,
			})
			took = true
		}
		if took {
			continue
		}
		// Every step left waits on another that is itself waiting: a circle bd
		// should not have poured. Take them in id order rather than loop.
		for _, step := range steps {
			if done[step.ID] {
				continue
			}
			done[step.ID] = true
			ordered = append(ordered, application.FormulaStep{
				ID: step.ID, Title: step.Title, Description: step.Description,
			})
		}
	}
	return ordered
}

// ready reports whether every step this one waits on has been taken already.
func ready(waitingOn []string, done map[string]bool) bool {
	for _, on := range waitingOn {
		if !done[on] {
			return false
		}
	}
	return true
}
