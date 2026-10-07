package application

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// EpicRules is where a rig's requirements of its epics are read from: the
// vault's file for the rig, which both hosts read alike. A rig with no file, or
// whose file lists neither key, asks nothing and comes back with the zero
// EpicRequirements. Reading changes nothing.
type EpicRules interface {
	EpicRequirements(ctx context.Context, rig string) (domain.EpicRequirements, error)
}

// EpicWaiver is the Governor's word that an epic is excused from some of what
// its rig requires: the names excused, and what he said, quoted. Only a
// waiver carrying his words is a waiver.
type EpicWaiver struct {
	Names   []string
	Because string
}

// None reports whether nothing is waived.
func (w EpicWaiver) None() bool { return len(w.Names) == 0 && strings.TrimSpace(w.Because) == "" }

// Comment is the record written on the epic: what was waived, and the words.
func (w EpicWaiver) Comment() string {
	return fmt.Sprintf("Epic requirement waived: %s. The Governor's word: %q", strings.Join(w.Names, ", "), strings.TrimSpace(w.Because))
}

// requirementsOf is what the plan's rig asks of the plan's epic, and the
// refusal when the plan does not meet it and the waiver does not cover the
// rest. Nothing is written either way.
func (f File) requirementsOf(ctx context.Context, plan domain.Plan) (domain.EpicRequirements, error) {
	rig := strings.TrimSpace(plan.Epic.Defaults.Rig)

	var rules domain.EpicRequirements
	if f.Rules != nil && rig != "" {
		var err error
		if rules, err = f.Rules.EpicRequirements(ctx, rig); err != nil {
			return rules, fmt.Errorf("reading what the rig %s requires of its epics: %w", rig, err)
		}
	}

	var reasons []string
	waivedSection, waivedLabel := map[string]bool{}, map[string]bool{}
	switch {
	case !f.Waive.None() && len(f.Waive.Names) == 0:
		reasons = append(reasons, "--because says what the Governor said but no --waive names what it waives")
	case len(f.Waive.Names) > 0 && strings.TrimSpace(f.Waive.Because) == "":
		reasons = append(reasons, "--waive needs --because \"<the Governor's words>\": only his word waives a requirement")
	}
	for _, name := range f.Waive.Names {
		section, label := rules.Match(name)
		if !section && !label {
			reasons = append(reasons, fmt.Sprintf("the rig %s does not require %s, so there is nothing to waive (it requires: %s; names are case-sensitive)",
				rig, name, listOrNothing(rules.Names())))
			continue
		}
		waivedSection[name], waivedLabel[name] = section, label
	}

	for _, short := range rules.Check(plan.Shape()) {
		if short.Section && !waivedSection[short.Name] || !short.Section && !waivedLabel[short.Name] {
			reasons = append(reasons, fmt.Sprintf("the rig %s requires %s", rig, short))
		}
	}
	if len(reasons) > 0 {
		return rules, &domain.PlanRefused{Reasons: reasons}
	}
	return rules, nil
}

// recordWaiver writes the Governor's waiver on the epic it excused: a label for
// each name waived, which mw status reads, and a comment quoting his words,
// which a person reads.
func (f File) recordWaiver(ctx context.Context, epicID string, rules domain.EpicRequirements) error {
	for _, name := range f.Waive.Names {
		section, label := rules.Match(name)
		for _, kind := range []struct{ section, applies bool }{{true, section}, {false, label}} {
			if !kind.applies {
				continue
			}
			if err := f.Tracker.AddLabel(ctx, epicID, domain.WaiverLabel(kind.section, name)); err != nil {
				return fmt.Errorf("recording the waiver of %s on %s: %w", name, epicID, err)
			}
		}
	}
	if err := f.Tracker.CommentOnStory(ctx, epicID, f.Waive.Comment()); err != nil {
		return fmt.Errorf("recording the Governor's words for the waiver on %s: %w", epicID, err)
	}
	return nil
}

func listOrNothing(names []string) string {
	if len(names) == 0 {
		return "nothing"
	}
	return strings.Join(names, ", ")
}

// EpicShortfall is an open epic that does not meet what its rig requires.
type EpicShortfall struct {
	ID    string
	Title string
	Rig   string
	// Missing are the requirements it does not meet, Waived the ones it does
	// not meet but the Governor waived.
	Missing []domain.Shortfall
	Waived  []domain.Shortfall
}

// epicShortfalls reads every live epic of a rig that requires something and
// reports the ones that do not meet it; a bead labelled wayfinder:map is a map,
// not an epic to hold to them. The epics are read in one batch, and the stories
// of one only when its rig asks for a last story. Nothing is written.
func (s Status) epicShortfalls(ctx context.Context) ([]EpicShortfall, error) {
	if s.Rules == nil {
		return nil, nil
	}
	ids, err := s.Tracker.LiveEpics(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing the live epics: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	beads, err := s.Tracker.ShowBeads(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("reading the live epics: %w", err)
	}

	rules := map[string]domain.EpicRequirements{}
	var found []EpicShortfall
	for _, bead := range beads {
		if slices.Contains(bead.Labels, "wayfinder:map") {
			continue
		}
		rig := strings.TrimSpace(bead.Merged().Rig)
		if rig == "" {
			continue
		}
		required, read := rules[rig]
		if !read {
			if required, err = s.Rules.EpicRequirements(ctx, rig); err != nil {
				return nil, fmt.Errorf("reading what the rig %s requires of its epics: %w", rig, err)
			}
			rules[rig] = required
		}
		if !required.Any() {
			continue
		}

		shape := domain.EpicShape{Description: bead.Description}
		if len(required.LastStoryLabels) > 0 {
			epic, err := s.Tracker.ShowEpic(ctx, bead.Story.ID)
			if err != nil {
				return nil, fmt.Errorf("reading the stories of %s: %w", bead.Story.ID, err)
			}
			for _, story := range epic.Stories {
				if story.IsEpic {
					continue
				}
				shape.Stories = append(shape.Stories, domain.StoryShape{Key: story.Story.ID, Labels: story.Labels, Needs: story.Needs})
			}
		}

		short := EpicShortfall{ID: bead.Story.ID, Title: bead.Story.Title, Rig: rig}
		for _, lack := range required.Check(shape) {
			if domain.Waives(bead.Labels, lack) {
				short.Waived = append(short.Waived, lack)
			} else {
				short.Missing = append(short.Missing, lack)
			}
		}
		if len(short.Missing) > 0 || len(short.Waived) > 0 {
			found = append(found, short)
		}
	}
	return found, nil
}
