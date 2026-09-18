package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Plan is the Mayor's plan for one epic, as it is written down before it is
// filed: the epic with the default Path its stories inherit, and the stories,
// in the order the Mayor wrote them. It is a document, not a tracker: the
// stories name each other by the keys the plan itself gives them, because
// nothing in it has a bead id until it is filed.
type Plan struct {
	Epic    PlanEpic    `json:"epic"`
	Stories []PlanStory `json:"stories"`
}

// PlanEpic is the epic a plan delivers. Defaults is the Path its stories are
// worked by except where a story overrides it. A Priority of zero means the
// plan does not say, and the tracker's own default stands.
type PlanEpic struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Priority    int    `json:"priority"`
	Description string `json:"description"`
	Success     string `json:"success_criteria"`
	Defaults    Path   `json:"defaults"`
}

// PlanStory is one story of a plan. Overrides are the fields of the epic's
// default Path this story departs from; Needs are the keys of the stories that
// must be finished before this one can be worked. A Priority or an Estimate of
// zero means the plan does not say.
type PlanStory struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Priority    int      `json:"priority"`
	Estimate    int      `json:"estimate"`
	Description string   `json:"description"`
	Acceptance  string   `json:"acceptance"`
	Overrides   Path     `json:"path"`
	Needs       []string `json:"needs"`
}

// PathFrom is the Path this planned story would be worked by: the epic's
// defaults with the story's own overrides on top, or the reason it is no Path.
func (s PlanStory) PathFrom(defaults Path) (Path, error) {
	return Story{ID: s.Key, Title: s.Title, Overrides: s.Overrides}.PathFrom(defaults)
}

// PlanRefused is a plan that cannot be filed, carrying every reason it cannot
// rather than only the first: a plan is read and rewritten by a person, and one
// reason at a time is one round trip at a time.
type PlanRefused struct {
	Reasons []string
}

// Error is the whole refusal, one reason to a line.
func (r *PlanRefused) Error() string {
	return "this plan cannot be filed:\n  - " + strings.Join(r.Reasons, "\n  - ")
}

// ParsePlan reads a plan from the JSON the Mayor wrote. A key the format does
// not know is a refusal rather than a silence: a misspelt "brnach" would
// otherwise be dropped without a word and become a story with no path.
func ParsePlan(written []byte) (Plan, error) {
	decoder := json.NewDecoder(bytes.NewReader(written))
	decoder.DisallowUnknownFields()

	var plan Plan
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("this is not a plan: %w", err)
	}
	return plan, nil
}

// Validate reports every reason this plan cannot be filed: an epic with no
// title, a story with no key, no title, no acceptance criteria or no Path, a
// story waiting on a key no story in the plan has, and stories waiting on each
// other. It comes back as a *PlanRefused, or nil when the plan is filable.
func (p Plan) Validate() error {
	var reasons []string

	if strings.TrimSpace(p.Epic.Title) == "" {
		reasons = append(reasons, "the epic has no title")
	}
	if len(p.Stories) == 0 {
		reasons = append(reasons, "the plan has no stories")
	}

	seen := map[string]bool{}
	for i, story := range p.Stories {
		name := story.name(i)
		switch {
		case strings.TrimSpace(story.Key) == "":
			reasons = append(reasons, name+" has no key, so no other story can wait on it")
		case seen[story.Key]:
			reasons = append(reasons, fmt.Sprintf("two stories share the key %s", story.Key))
		}
		seen[story.Key] = true

		if strings.TrimSpace(story.Title) == "" {
			reasons = append(reasons, name+" has no title")
		}
		if strings.TrimSpace(story.Acceptance) == "" {
			reasons = append(reasons, name+" has no acceptance criteria, so nobody could say it was done")
		}
		if _, err := story.PathFrom(p.Epic.Defaults); err != nil {
			reasons = append(reasons, fmt.Sprintf("%s has no path: %s", name, err))
		}
	}

	for i, story := range p.Stories {
		name := story.name(i)
		for _, need := range story.Needs {
			switch {
			case need == story.Key:
				reasons = append(reasons, name+" waits on itself")
			case !seen[need]:
				reasons = append(reasons, fmt.Sprintf("%s waits on %s, which no story in this plan is", name, need))
			}
		}
	}

	if cycle := p.cycle(); len(cycle) > 0 {
		reasons = append(reasons, "these stories wait on each other: "+strings.Join(cycle, " needs "))
	}

	if len(reasons) == 0 {
		return nil
	}
	return &PlanRefused{Reasons: reasons}
}

// Order is the plan's stories in the order they can be filed: no story comes
// before a story it waits on, and stories that wait on nothing keep the order
// the Mayor wrote them in. Stories that wait on each other have no such order,
// and say so.
func (p Plan) Order() ([]PlanStory, error) {
	filed := make(map[string]bool, len(p.Stories))
	order := make([]PlanStory, 0, len(p.Stories))

	for len(order) < len(p.Stories) {
		placed := false
		for _, story := range p.Stories {
			if filed[story.Key] || !canBeFiled(story, filed) {
				continue
			}
			order = append(order, story)
			filed[story.Key] = true
			placed = true
		}
		if !placed {
			if loop := p.cycle(); len(loop) > 0 {
				return nil, &PlanRefused{Reasons: []string{
					"these stories wait on each other: " + strings.Join(loop, " needs "),
				}}
			}
			var stuck []string
			for i, story := range p.Stories {
				if !filed[story.Key] {
					stuck = append(stuck, story.name(i))
				}
			}
			return nil, &PlanRefused{Reasons: []string{
				"these stories wait on stories this plan does not have: " + strings.Join(stuck, ", "),
			}}
		}
	}
	return order, nil
}

// cycle is one loop of stories that wait on each other, written as the way
// round it goes and ending where it started, or nothing when there is no loop.
// A story waiting on a key no story has is not a loop: Validate says that.
func (p Plan) cycle() []string {
	needs := make(map[string][]string, len(p.Stories))
	for _, story := range p.Stories {
		needs[story.Key] = story.Needs
	}

	const (
		walking = 1
		done    = 2
	)
	state := map[string]int{}
	var path []string

	var walk func(key string) []string
	walk = func(key string) []string {
		if _, known := needs[key]; !known || state[key] == done {
			return nil
		}
		if state[key] == walking {
			// The loop is the tail of the path from where this key first
			// appeared, closed by the key itself.
			for i, seen := range path {
				if seen == key {
					return append(append([]string{}, path[i:]...), key)
				}
			}
			return []string{key, key}
		}

		state[key] = walking
		path = append(path, key)
		for _, need := range needs[key] {
			if loop := walk(need); len(loop) > 0 {
				return loop
			}
		}
		path = path[:len(path)-1]
		state[key] = done
		return nil
	}

	for _, story := range p.Stories {
		if loop := walk(story.Key); len(loop) > 0 {
			return loop
		}
	}
	return nil
}

// name is what a reason calls this story: its key, or its place in the plan
// when it has no key to be called by.
func (s PlanStory) name(i int) string {
	if key := strings.TrimSpace(s.Key); key != "" {
		return "story " + key
	}
	if title := strings.TrimSpace(s.Title); title != "" {
		return fmt.Sprintf("story %d (%s)", i+1, title)
	}
	return fmt.Sprintf("story %d", i+1)
}

// canBeFiled reports whether every story this one waits on is filed already.
func canBeFiled(story PlanStory, filed map[string]bool) bool {
	for _, need := range story.Needs {
		if !filed[need] {
			return false
		}
	}
	return true
}
