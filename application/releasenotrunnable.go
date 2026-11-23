package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// ForceReleaseMarker opens the comment a release writes on a story it lets
// through although its acceptance holds a criterion no Builder can meet before
// landing (mw-gq6.1686).
const ForceReleaseMarker = "RELEASED WITH --force:"

// theMayorsCheckLine opens the paragraph that already says a check is the Mayor's
// after landing ("Verify after landing (the Mayor's check ...", "Verify strings:",
// "VERIFY AFTER LANDING"). The criterion splitter reads such a paragraph under the
// last numbered criterion, so it is cut off before that criterion is judged: it is
// the right place for a live URL, and no refusal of it.
var theMayorsCheckLine = regexp.MustCompile(`(?im)^[ \t]*verify (after landing|strings:)`)

// liveURLCriteria are the criteria of an acceptance that name a live URL of a
// served rig (an allmymind.org address or the rig's own site_url) or say they are
// judged after the fix lands, leaving out one that names a test or a fixture. It
// is what mw path set warns of (PathSet.liveURLWarning) and mw release refuses.
func liveURLCriteria(ctx context.Context, rules EpicRules, rig, acceptance string) []domain.Criterion {
	site := ""
	if rules != nil && strings.TrimSpace(rig) != "" {
		if requirements, err := rules.EpicRequirements(ctx, rig); err == nil {
			site = strings.ToLower(strings.TrimRight(strings.TrimSpace(requirements.SiteURL), "/"))
		}
	}
	var found []domain.Criterion
	for _, criterion := range domain.AcceptanceCriteria(acceptance) {
		if cut := theMayorsCheckLine.FindStringIndex(criterion.Text); cut != nil {
			criterion.Text = criterion.Text[:cut[0]]
		}
		if aTestOrFixture.MatchString(criterion.Text) {
			continue
		}
		if liveServedURL.MatchString(criterion.Text) || afterLandingWords.MatchString(criterion.Text) ||
			(site != "" && strings.Contains(strings.ToLower(criterion.Text), site)) {
			found = append(found, criterion)
		}
	}
	return found
}

// notRunnableRefusal is why a story is not released, or "" when none of its
// criteria needs its own landing: each such criterion is named with its first
// words, and the way out is said (mw-gq6.1686).
func notRunnableRefusal(ctx context.Context, rules EpicRules, id, rig, acceptance string) string {
	found := liveURLCriteria(ctx, rules, rig, acceptance)
	if len(found) == 0 {
		return ""
	}
	var named []string
	for _, c := range found {
		named = append(named, fmt.Sprintf("criterion %d (%q)", c.Number, heldLine(truncateWords(c.Text, 90))))
	}
	return fmt.Sprintf("%s: %s of its acceptance names a live URL of a served rig or says it is judged after the fix lands, "+
		"which no Builder can meet before landing; move it to a 'Verify strings:' line of the description or to mw verify",
		id, strings.Join(named, "; "))
}

// truncateWords cuts text to at most n runes, adding an ellipsis when it cut.
func truncateWords(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > n {
		return string(runes[:n]) + "…"
	}
	return text
}

// checkRunnable refuses the release of any of these held stories whose
// acceptance holds a criterion no Builder can meet before landing, naming it,
// unless Force says why not; nothing has been released when it refuses. With
// Force it writes the reason on each such story.
func (r Release) checkRunnable(ctx context.Context, stories []StoryDetail) error {
	var refusals, flagged []string
	for _, story := range stories {
		if line := notRunnableRefusal(ctx, r.Rules, story.Story.ID, story.Merged().Rig, story.Acceptance); line != "" {
			refusals = append(refusals, line)
			flagged = append(flagged, story.Story.ID)
		}
	}
	if len(flagged) == 0 {
		return nil
	}
	if strings.TrimSpace(r.Force) == "" {
		return fmt.Errorf("releasing refused, nothing released: %s\nRelease anyway with --force \"<why>\"", strings.Join(refusals, "\n"))
	}
	note := fmt.Sprintf("%s %s", ForceReleaseMarker, strings.Join(strings.Fields(r.Force), " "))
	for _, id := range flagged {
		if err := r.Tracker.CommentOnStory(ctx, id, note); err != nil {
			return fmt.Errorf("writing the reason for releasing %s with --force: %w", id, err)
		}
	}
	return nil
}
