package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// ReleasedOnPathSet is the comment a story carries when mw path set released it
// (mw-t5shinh.16): the mark the trial of release_on_path is measured by.
const ReleasedOnPathSet = "released on path set"

// afterIDs are the stories --after names, trimmed and once each, in the order
// given. Every one must be a story the tracker has, and none may be the story
// itself; nothing is written when one is not. A nil After names none, and so
// does an empty one (--after none): the difference is whether --after was given.
func (s PathSet) afterIDs(ctx context.Context, id string) ([]string, error) {
	var ids []string
	for _, after := range s.After {
		after = strings.TrimSpace(after)
		switch {
		case after == "":
			return nil, fmt.Errorf("--after names an empty story id: give story ids separated by commas, or none: nothing was written")
		case after == id:
			return nil, fmt.Errorf("--after names %s, the story itself: a story cannot wait on itself: nothing was written", id)
		}
		if !slices.Contains(ids, after) {
			ids = append(ids, after)
		}
	}
	for _, after := range ids {
		if _, err := s.Tracker.ShowStory(ctx, after); err != nil {
			return nil, fmt.Errorf("reading the story %s that %s is to wait on (--after): %w: nothing was written", after, id, err)
		}
	}
	return ids, nil
}

// releasesOnPath is whether this story's rig asks, in its file, that mw path set
// --after release the story it completes. A path set with no --after, with a
// --hold, or with no rules to read releases none.
func (s PathSet) releasesOnPath(ctx context.Context, rig string) (bool, error) {
	if s.After == nil || strings.TrimSpace(s.Hold) != "" {
		return false, nil
	}
	return s.rigReleasesOnPath(ctx, rig)
}

// rigReleasesOnPath is whether the rig's file says release_on_path = true; no
// rules to read, or no rig, says no.
func (s PathSet) rigReleasesOnPath(ctx context.Context, rig string) (bool, error) {
	if s.Rules == nil || strings.TrimSpace(rig) == "" {
		return false, nil
	}
	required, err := s.Rules.EpicRequirements(ctx, rig)
	if err != nil {
		return false, fmt.Errorf("reading whether the rig %s releases a story on path set: %w", rig, err)
	}
	return required.ReleaseOnPath, nil
}

// releaseOnPathSet releases the held story that mw path set has just completed,
// by the one-story release of mw release, unless it is held on purpose: then it
// stays held and one held: line says why. It never releases over a hold, and
// never a story that dispatch would only pass over. story is the story as it was
// read before the write and path the path just written.
func (s PathSet) releaseOnPathSet(ctx context.Context, id string, story StoryDetail, path domain.Path) error {
	if !story.Held() {
		return nil
	}
	written := story
	written.Story.Overrides = story.Story.Overrides.Overlay(path)
	why, err := s.whyStayHeld(ctx, id, story, written)
	if err != nil {
		return err
	}
	if why != "" {
		if s.Out != nil {
			fmt.Fprintf(s.Out, "held: %s: %s\n", id, why)
		}
		return nil
	}
	// A story over its size's test callers, with no callers_ok, is not released:
	// the Mayor sizes it up or says why the callers are fine, and runs it again.
	if held, err := s.holdForCallers(ctx, id, written); err != nil || held {
		return err
	}
	// The release of `mw release <id>`, which reads the story as the write left
	// it and looks once more for a hold on the Governor's word.
	if _, err := (Release{Tracker: s.Tracker, Rules: s.Rules, Rigs: s.Rigs, RigFiles: s.RigFiles}).story(ctx, id); err != nil {
		return fmt.Errorf("releasing %s after its path was set: %w", id, err)
	}
	if err := s.Tracker.CommentOnStory(ctx, id, ReleasedOnPathSet); err != nil {
		return fmt.Errorf("writing the comment that %s was released on path set: %w", id, err)
	}
	if s.Out != nil {
		fmt.Fprintf(s.Out, "%s: %s\n", ReleasedOnPathSet, id)
	}
	return nil
}

// callersHeldWord opens the reason (after HELD:) a story is kept held for its
// test callers; whyStayHeld does not count such a comment as a hold of its own,
// or a story sized up could never be released by the next mw path set.
const callersHeldWord = "test callers"

// isCallersHold is whether a comment is the one holdForCallers writes.
func isCallersHold(text string) bool {
	rest := strings.TrimSpace(text)
	return IsHoldComment(rest) && strings.HasPrefix(strings.TrimSpace(rest[len(HeldCommentPrefix):]), callersHeldWord+" ")
}

// holdForCallers keeps the story just pathed held, with a HELD: comment naming
// the count and the limit and one line on stdout, when its test callers are more
// than its size allows and it carries no callers_ok. It reports whether it did.
// A story whose count cannot be taken is not held for it.
func (s PathSet) holdForCallers(ctx context.Context, id string, written StoryDetail) (bool, error) {
	callers, ok := testCallersOf(s.Rigs, s.RigFiles, id, written.Merged())
	if !ok || !callers.Unwaived() {
		return false, nil
	}
	why := fmt.Sprintf("%s %d for size %s (allows %d)", callersHeldWord, callers.Count, callers.Size, callers.Allows())
	text := fmt.Sprintf("%s %s: size it up or give callers_ok=\"<why>\", then run mw path set again", HeldCommentPrefix, why)
	comments, err := s.Tracker.StoryComments(ctx, id)
	if err != nil {
		return false, fmt.Errorf("reading the comments of %s to write why it stays held: %w", id, err)
	}
	if !slices.ContainsFunc(comments, func(c Comment) bool { return strings.TrimSpace(c.Text) == text }) {
		if err := s.Tracker.CommentOnStory(ctx, id, text); err != nil {
			return false, fmt.Errorf("writing the reason %s is held: %w", id, err)
		}
	}
	if s.Out != nil {
		fmt.Fprintf(s.Out, "held: %s: NOT released: %s; size it up or give callers_ok=\"<why>\", then run mw path set again\n", id, why)
	}
	return true, nil
}

// holdOnPathSet writes the reason --hold gives as a HELD: comment on the held
// story, which stays held.
func (s PathSet) holdOnPathSet(ctx context.Context, id string) error {
	why := strings.Join(strings.Fields(s.Hold), " ")
	if err := s.Tracker.CommentOnStory(ctx, id, HeldCommentPrefix+" "+why); err != nil {
		return fmt.Errorf("writing the reason %s is held: %w", id, err)
	}
	if s.Out != nil {
		fmt.Fprintf(s.Out, "held: %s: %s; release it with: mw release %s\n", id, why, id)
	}
	return nil
}

// whyStayHeld is the first reason a held story is not released, or "" when none
// holds it: a hold label, a HELD: comment (of its own, or of its epic's that
// names it, on the Governor's word), no rig or target branch, and what mw
// dispatch would pass it over for.
func (s PathSet) whyStayHeld(ctx context.Context, id string, story, written StoryDetail) (string, error) {
	for _, label := range story.Labels {
		if hasHoldLabel([]string{label}) {
			if strings.EqualFold(label, LabelIcebox) {
				return fmt.Sprintf("it has the label %s; thaw it with: mw thaw %s", label, id), nil
			}
			return fmt.Sprintf("it has the label %s; release it with: mw release %s", label, id), nil
		}
	}
	ids := []string{id}
	if story.EpicID != "" {
		ids = append(ids, story.EpicID)
	}
	comments, err := s.Tracker.StoriesComments(ctx, ids)
	if err != nil {
		return "", fmt.Errorf("reading the comments of %s to look for a hold: %w", strings.Join(ids, ", "), err)
	}
	if hold, on, ok := wordHold(id, comments[id], comments[story.EpicID], story.EpicID); ok {
		return fmt.Sprintf("it is held on the Governor's word, by this comment on %s: %s; release it with: mw release %s --over-hold \"<why>\"", on, heldLine(hold.Text), id), nil
	}
	for _, c := range comments[id] {
		if holdsAReason([]Comment{c}) && !isCallersHold(c.Text) {
			return fmt.Sprintf("it has a HELD: comment: %s; release it with: mw release %s", heldLine(c.Text), id), nil
		}
	}
	merged := written.Merged()
	if strings.TrimSpace(merged.Rig) == "" || strings.TrimSpace(merged.Branch) == "" {
		return fmt.Sprintf("it has no path yet (no rig or no target branch); give it one with: mw path set %s rig=<rig> branch=<branch>", id), nil
	}
	lacking, err := (&filesExpected{rules: s.Rules}).lacking(ctx, written)
	if err != nil {
		return fmt.Sprintf("whether it needs a files key could not be read: %v", err), nil
	}
	if len(lacking) > 0 {
		return fmt.Sprintf("no files key, and %s expects one: %s", lacking[0].Rig, FilesKeyCommand(id)), nil
	}
	if lines := dispatchRefusals(written); len(lines) > 0 {
		return lines[0] + "; put that right in the story, then run mw path set " + id + " again", nil
	}
	return s.whyOverThePace(ctx, id, story)
}

// whyOverThePace is why a story labelled model-story is not released now, by the
// pace of mw release-paced (checkPace, the one check of both): the usage reading
// missing, stale or over the line, or ReleasePacedMax model stories of its epic
// already open or running. A story without the label is never paced, so "".
func (s PathSet) whyOverThePace(ctx context.Context, id string, story StoryDetail) (string, error) {
	if !hasLabel(story.Labels, LabelModelStory) {
		return "", nil
	}
	var active int
	if story.EpicID != "" {
		epic, err := s.Tracker.ShowEpic(ctx, story.EpicID)
		if err != nil {
			return "", fmt.Errorf("reading the model stories of %s to count the pace: %w", story.EpicID, err)
		}
		for _, other := range epic.Stories {
			if other.Story.ID != id && hasLabel(other.Labels, LabelModelStory) && !other.Held() && !other.Closed() {
				active++
			}
		}
	}
	now := time.Now() //mw:realclock the default clock, taken before an injected Now replaces it
	if s.Now != nil {
		now = s.Now()
	}
	pace, err := checkPace(ctx, s.Readings, s.UsageLine, now.UTC(), active, "its epic's model stories")
	if err != nil {
		return "", err
	}
	return pace.Held(), nil
}

// heldLine is the first line of a comment, cut at 160 characters.
func heldLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if runes := []rune(line); len(runes) > 160 {
		line = string(runes[:160]) + "…"
	}
	return line
}
