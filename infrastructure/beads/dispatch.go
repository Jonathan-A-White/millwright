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

// ReadyWithLabel implements application.WorkTracker. It is `bd ready` narrowed
// by a label, so bd applies its own idea of blocked, held and claimed, and
// unlike ReadyForHost it neither asks for unassigned beads nor drops the ones
// whose Path names no host: a bead for the Governor is nobody's to dispatch.
func (g *Gateway) ReadyWithLabel(ctx context.Context, label string) ([]application.StoryDetail, error) {
	if strings.TrimSpace(label) == "" {
		return nil, fmt.Errorf("which label are the ready stories carrying?")
	}
	out, err := g.call(ctx, "ready", "--label", label, "--exclude-type", "epic", "--limit", "0", "--json")
	if err != nil {
		return nil, err
	}
	beads, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading what is ready under the label %s: %w", label, err)
	}
	return g.overlaid(ctx, beads)
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

// WorkInHand implements application.WorkTracker: everything ready and
// unclaimed, and everything claimed and unfinished, on every host. It is the
// two listings a dispatcher makes, read once each with every distinct epic's
// defaults read once between them: two bd calls, plus one per distinct epic
// whose defaults a story inherits and bd did not inline.
//
// Nothing here narrows to a host, and nothing judges whether a host is awake:
// mw status does both, from what this returns and the note the host left.
func (g *Gateway) WorkInHand(ctx context.Context) (application.WorkInHand, error) {
	out, err := g.call(ctx, "ready", "--unassigned", "--exclude-type", "epic", "--json")
	if err != nil {
		return application.WorkInHand{}, err
	}
	ready, err := decodeBeads(out)
	if err != nil {
		return application.WorkInHand{}, fmt.Errorf("reading what is ready: %w", err)
	}

	out, err = g.call(ctx, "list", "--status", StatusInProgress, "--exclude-type", "epic", "--limit", "0", "--json")
	if err != nil {
		return application.WorkInHand{}, err
	}
	claimed, err := decodeBeads(out)
	if err != nil {
		return application.WorkInHand{}, fmt.Errorf("reading what is claimed: %w", err)
	}

	n := len(ready)
	details, err := g.overlaid(ctx, append(ready, claimed...))
	if err != nil {
		return application.WorkInHand{}, err
	}
	return application.WorkInHand{Ready: details[:n:n], Running: details[n:]}, nil
}

// onHost narrows beads to the stories worked on one host, with each story's
// epic defaults overlaid.
func (g *Gateway) onHost(ctx context.Context, stories []bead, host string) ([]application.StoryDetail, error) {
	details, err := g.overlaid(ctx, stories)
	if err != nil {
		return nil, err
	}
	var on []application.StoryDetail
	for _, detail := range details {
		if merged := detail.Merged().Host; merged == "" || merged != host {
			continue
		}
		on = append(on, detail)
	}
	return on, nil
}

// overlaid is beads read as stories, each with its epic's default Path
// overlaid. Every distinct epic is read once, however many stories hang from
// it, and not at all when bd inlined the epic with the story.
func (g *Gateway) overlaid(ctx context.Context, stories []bead) ([]application.StoryDetail, error) {
	defaults := map[string]domain.Path{}
	var details []application.StoryDetail

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
		details = append(details, story.detail(path))
	}
	return details, nil
}

// BlockedForHost implements application.WorkTracker. `bd blocked --json` finds
// the stories a dependency holds back, but not their epic: its rows carry
// neither a parent nor metadata, unlike `bd list` and `bd ready`. So each
// candidate is read back with ShowStory, which already knows how to overlay an
// epic's defaults onto a story read on its own — the same call `mw status`
// makes for a story by id, reused here rather than a second way of doing it.
func (g *Gateway) BlockedForHost(ctx context.Context, host string) ([]application.StoryDetail, error) {
	if host == "" {
		return nil, fmt.Errorf("which host are the blocked stories for?")
	}
	out, err := g.call(ctx, "blocked", "--json")
	if err != nil {
		return nil, err
	}
	candidates, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading what is blocked on %s: %w", host, err)
	}

	var blocked []application.StoryDetail
	for _, story := range candidates {
		if story.Type == TypeEpic || story.Status != StatusOpen || story.Assignee != "" {
			continue
		}
		detail, err := g.ShowStory(ctx, story.ID)
		if err != nil {
			return nil, fmt.Errorf("reading the blocked story %s: %w", story.ID, err)
		}
		if detail.Merged().Host != host {
			continue
		}
		blocked = append(blocked, detail)
	}
	return blocked, nil
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

// StoryState implements application.WorkTracker. A dimension nobody has ever
// set is not a failure here — a story nobody has recorded anything about simply
// has no state, which is "". bd says so in two ways, depending on the version:
// by exiting non-zero, and by printing "(no <dimension> state set)" and exiting
// zero (verified on bd 1.3.0). Both are read as nothing.
func (g *Gateway) StoryState(ctx context.Context, id, dimension string) (string, error) {
	switch {
	case strings.TrimSpace(id) == "":
		return "", fmt.Errorf("reading a state: which story?")
	case strings.TrimSpace(dimension) == "":
		return "", fmt.Errorf("reading the state of %s: which dimension?", id)
	}
	out, _, err := g.run(ctx, "state", id, dimension)
	if err != nil {
		return "", nil
	}
	said := strings.TrimSpace(string(out))
	if strings.HasPrefix(said, "(no ") && strings.HasSuffix(said, "state set)") {
		return "", nil
	}
	return said, nil
}

// OpenSteps implements application.WorkTracker: the step beads of a poured
// formula that are not closed. They come back in the order they are worked, so
// that the first one still open is the first thing the session did not do.
func (g *Gateway) OpenSteps(ctx context.Context, moleculeID string) ([]application.FormulaStep, error) {
	if strings.TrimSpace(moleculeID) == "" {
		return nil, nil
	}
	out, err := g.call(ctx, "list", "--parent", moleculeID, "--limit", "0", "--json")
	if err != nil {
		return nil, err
	}
	poured, err := decodeBeads(out)
	if err != nil {
		return nil, fmt.Errorf("reading the steps of %s: %w", moleculeID, err)
	}

	open := make([]bead, 0, len(poured))
	for _, step := range poured {
		if step.Status != StatusClosed {
			open = append(open, step)
		}
	}
	return inWorkedOrder(open), nil
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
