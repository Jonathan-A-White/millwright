package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// guestRig is a rig of a plan whose owner is not the Governor.
type guestRig struct {
	Rig   string
	Owner string
}

// guestRigsOf is the guest rigs a plan works in, in the order the epic's
// default rig and then the stories name them: a story may override the rig its
// epic names. A File with no Rules knows no guest.
func (f File) guestRigsOf(ctx context.Context, plan domain.Plan) ([]guestRig, error) {
	if f.Rules == nil {
		return nil, nil
	}
	rigs := []string{strings.TrimSpace(plan.Epic.Defaults.Rig)}
	for _, story := range plan.Stories {
		if path, err := story.PathFrom(plan.Epic.Defaults); err == nil {
			rigs = append(rigs, strings.TrimSpace(path.Rig))
		}
	}

	var guests []guestRig
	seen := map[string]bool{}
	for _, rig := range rigs {
		if rig == "" || seen[rig] {
			continue
		}
		seen[rig] = true
		rules, err := f.Rules.EpicRequirements(ctx, rig)
		if err != nil {
			return nil, fmt.Errorf("reading whether the rig %s is a guest: %w", rig, err)
		}
		if rules.Guest != "" {
			guests = append(guests, guestRig{Rig: rig, Owner: rules.Guest})
		}
	}
	return guests, nil
}

// checkGuests refuses a plan that works in a guest rig unless the owner's words
// came with it (File.GuestAsk). Nothing is written either way.
func (f File) checkGuests(guests []guestRig) error {
	if len(guests) == 0 || strings.TrimSpace(f.GuestAsk) != "" {
		return nil
	}
	var reasons []string
	for _, g := range guests {
		reasons = append(reasons, fmt.Sprintf("the rig %s is a guest repo, owned by %s: the factory works it only when %s asks; "+
			"file it with --guest-ask \"<their words>\" once they have", g.Rig, g.Owner, g.Owner))
	}
	return &domain.PlanRefused{Reasons: reasons}
}

// recordGuestAsk writes the owner's words on the epic, as recordWaiver writes
// the Governor's.
func (f File) recordGuestAsk(ctx context.Context, epicID string, guests []guestRig) error {
	if len(guests) == 0 {
		return nil
	}
	var named []string
	for _, g := range guests {
		named = append(named, fmt.Sprintf("%s (%s)", g.Rig, g.Owner))
	}
	comment := fmt.Sprintf("Guest rig worked at its owner's ask: %s. Their words: %q", strings.Join(named, ", "), strings.TrimSpace(f.GuestAsk))
	if err := f.Tracker.CommentOnStory(ctx, epicID, comment); err != nil {
		return fmt.Errorf("recording the owner's words for the guest rig on %s: %w", epicID, err)
	}
	return nil
}

// markGuests sets Guest on each story whose rig is a guest rig, reading each
// rig's file once. A nil rules marks nothing.
func markGuests(ctx context.Context, rules EpicRules, details ...*StoryDetail) error {
	if rules == nil {
		return nil
	}
	owners := map[string]string{}
	for _, d := range details {
		rig := strings.TrimSpace(d.Merged().Rig)
		if rig == "" {
			continue
		}
		owner, read := owners[rig]
		if !read {
			required, err := rules.EpicRequirements(ctx, rig)
			if err != nil {
				return fmt.Errorf("reading whether the rig %s is a guest: %w", rig, err)
			}
			owner = required.Guest
			owners[rig] = owner
		}
		d.Guest = owner
	}
	return nil
}
