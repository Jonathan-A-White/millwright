package domain

import (
	"fmt"
	"sort"
	"strings"
)

// EpicRequirements is what a rig asks of every epic that is worked in it, as
// the vault's file for the rig says it. Both lists are optional and both grow
// by adding a name to them: no code names "Demo" or "demo".
type EpicRequirements struct {
	// Sections are the headings the epic's description must contain, each as a
	// markdown heading or as a line that starts "<Name>:".
	Sections []string
	// LastStoryLabels are the labels the epic must carry on a story that waits
	// on every other story of the epic, so that it is the last one worked: one
	// such story for each label.
	LastStoryLabels []string
	// Guest, when it is not empty, names the owner of a guest rig: a rig whose
	// owner is not the Governor, worked by the factory only when its owner asks.
	// It is no requirement of an epic, so Any does not count it.
	Guest string
	// VersionFiles, when it is not empty, are the files of the rig, relative to
	// its root, that carry its version: a landing raises the patch of the first
	// one's version and writes it into all of them. It is no requirement of an
	// epic either, so Any does not count it.
	VersionFiles []string
	// ChangelogFiles, when it is not empty, are the files of the rig that keep
	// its changelog, relative to its root: a .json one and a .md one. A landing
	// writes the story's What's new: note into them in the commit that carries
	// the version. It is no requirement of an epic either, so Any does not count it.
	ChangelogFiles []string
}

// Any reports whether the rig asks anything of its epics at all. A rig that
// asks nothing is unchecked.
func (r EpicRequirements) Any() bool {
	return len(r.Sections) > 0 || len(r.LastStoryLabels) > 0
}

// Names are every name the rig requires, sections first, as a waiver may name
// them.
func (r EpicRequirements) Names() []string {
	return append(append([]string(nil), r.Sections...), r.LastStoryLabels...)
}

// EpicShape is an epic as the requirements read it: its description and its
// stories. A plan about to be filed and an epic read back from the tracker are
// both turned into one, so that a plan is held to exactly what the epic it
// becomes will be held to.
type EpicShape struct {
	Description string
	Stories     []StoryShape
}

// StoryShape is one story of an EpicShape. Key is whatever the stories name each
// other by — a plan's key, or a tracker's id — and Needs are the Keys it waits
// on; a Need that is no story of the epic is not a sibling and is ignored.
type StoryShape struct {
	Key    string
	Labels []string
	Needs  []string
}

// Shortfall is one requirement an epic does not meet: the name the rig
// requires, whether it is a section or a last-story label, and in a phrase
// what is wrong.
type Shortfall struct {
	Name    string
	Section bool
	Why     string
}

// String is the shortfall as a person reads it.
func (s Shortfall) String() string {
	if s.Section {
		return fmt.Sprintf("the %s section: %s", s.Name, s.Why)
	}
	return fmt.Sprintf("the last story labelled %s: %s", s.Name, s.Why)
}

// Check is every requirement the epic does not meet, sections first, in the
// order the rig lists them. An epic that meets them all has none.
func (r EpicRequirements) Check(epic EpicShape) []Shortfall {
	var short []Shortfall
	for _, name := range r.Sections {
		if !HasSection(epic.Description, name) {
			short = append(short, Shortfall{Name: name, Section: true,
				Why: "the description has no heading or line \"" + name + ":\" for it"})
		}
	}
	for _, label := range r.LastStoryLabels {
		if why := lastStoryProblem(epic.Stories, label); why != "" {
			short = append(short, Shortfall{Name: label, Why: why})
		}
	}
	return short
}

// HasSection reports whether a description contains a section called name: a
// markdown heading whose text is the name (a trailing colon allowed), or any
// line that starts "<name>:". Case is not significant.
func HasSection(description, name string) bool {
	name = strings.TrimSpace(name)
	for _, line := range strings.Split(description, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
			if strings.EqualFold(strings.TrimSpace(strings.TrimSuffix(line, ":")), name) {
				return true
			}
		}
		if len(line) > len(name) && strings.EqualFold(line[:len(name)], name) && line[len(name)] == ':' {
			return true
		}
	}
	return false
}

// lastStoryProblem is why no story of the epic is the last one carrying label,
// or "" when one is: a story carrying it that waits, directly or through
// others, on every other story of the epic.
func lastStoryProblem(stories []StoryShape, label string) string {
	keys := make(map[string]bool, len(stories))
	for _, story := range stories {
		keys[story.Key] = true
	}

	var carriers []StoryShape
	for _, story := range stories {
		for _, have := range story.Labels {
			if strings.EqualFold(strings.TrimSpace(have), strings.TrimSpace(label)) {
				carriers = append(carriers, story)
				break
			}
		}
	}
	if len(carriers) == 0 {
		return "no story of the epic carries that label"
	}

	var best []string
	for i, carrier := range carriers {
		waits := waitsOn(stories, keys, carrier.Key)
		var missing []string
		for _, story := range stories {
			if story.Key != carrier.Key && !waits[story.Key] {
				missing = append(missing, story.Key)
			}
		}
		if len(missing) == 0 {
			return ""
		}
		if i == 0 || len(missing) < len(best) {
			best = missing
		}
	}
	sort.Strings(best)
	return "no story carrying it waits on every other story (the closest does not wait on " + strings.Join(best, ", ") + ")"
}

// waitsOn is every story of the epic the story named by key waits on, directly
// or through the stories it waits on.
func waitsOn(stories []StoryShape, keys map[string]bool, key string) map[string]bool {
	needs := make(map[string][]string, len(stories))
	for _, story := range stories {
		needs[story.Key] = story.Needs
	}
	seen := map[string]bool{}
	queue := []string{key}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, need := range needs[next] {
			if keys[need] && !seen[need] {
				seen[need] = true
				queue = append(queue, need)
			}
		}
	}
	return seen
}

// Match reports which requirements of the rig carry exactly this name: a
// section, a last-story label, or both. Case matters: "Demo" the section and
// "demo" the label are two requirements, and a waiver names one of them.
func (r EpicRequirements) Match(name string) (section, label bool) {
	for _, have := range r.Sections {
		section = section || have == name
	}
	for _, have := range r.LastStoryLabels {
		label = label || have == name
	}
	return section, label
}

// WaiverLabelPrefix starts the label an epic carries for each requirement the
// Governor waived on it.
const WaiverLabelPrefix = "epic-waiver:"

// WaiverLabel is the label that records the waiver of one requirement on an
// epic: the kind of requirement it was, and its name in lower case with dashes
// for spaces, since a label has neither case nor spaces to keep.
func WaiverLabel(section bool, name string) string {
	kind := "story:"
	if section {
		kind = "section:"
	}
	return WaiverLabelPrefix + kind + strings.ToLower(strings.Join(strings.Fields(name), "-"))
}

// Waives reports whether labels, an epic's, record a waiver of the requirement
// the shortfall is for.
func Waives(labels []string, lack Shortfall) bool {
	want := WaiverLabel(lack.Section, lack.Name)
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), want) {
			return true
		}
	}
	return false
}
