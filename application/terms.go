package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// TermsLinePrefix opens the line a story of a rig with terms_title must hold
// before it is released: the host's terms and robots.txt read, the clause
// quoted, and whether it allows the crawl (mw-gq6.1726).
const TermsLinePrefix = "TERMS:"

// termsLine is a line, in a description or a comment, that begins TERMS:.
var termsLine = regexp.MustCompile(`(?m)^[ \t]*` + TermsLinePrefix)

// hasTermsLine is whether the description or any of the comments holds a line
// beginning TERMS:.
func hasTermsLine(description string, comments []Comment) bool {
	if termsLine.MatchString(description) {
		return true
	}
	for _, c := range comments {
		if termsLine.MatchString(c.Text) {
			return true
		}
	}
	return false
}

// termsGuarded is whether the rig's file sets terms_title and the title matches
// it: the story crawls a new web source and may not be released unguarded. No
// rules, no rig or no key guards nothing; a terms_title that is no regular
// expression is an error, never a pass.
func termsGuarded(ctx context.Context, rules EpicRules, rig, title string) (bool, error) {
	if rules == nil || strings.TrimSpace(rig) == "" {
		return false, nil
	}
	required, err := rules.EpicRequirements(ctx, rig)
	if err != nil {
		return false, fmt.Errorf("reading whether the rig %s guards the release of a web source (terms_title): %w", rig, err)
	}
	if strings.TrimSpace(required.TermsTitle) == "" {
		return false, nil
	}
	pattern, err := regexp.Compile(required.TermsTitle)
	if err != nil {
		return false, fmt.Errorf("terms_title of the rig %s is no regular expression: %w", rig, err)
	}
	return pattern.MatchString(title), nil
}

// termsRefusal is why a story is not released, or "" when it may be: it is a
// story of a rig that sets terms_title, its title matches, and neither its
// description nor a comment holds a TERMS: line. comments is nil to say they
// are not known yet; a reader is then asked for them, and only when the
// guard applies.
func termsRefusal(ctx context.Context, rules EpicRules, read func(context.Context, string) ([]Comment, error),
	id, rig, title, description string, comments []Comment) (string, error) {
	guarded, err := termsGuarded(ctx, rules, rig, title)
	if err != nil || !guarded {
		return "", err
	}
	if termsLine.MatchString(description) {
		return "", nil
	}
	if comments == nil && read != nil {
		if comments, err = read(ctx, id); err != nil {
			return "", fmt.Errorf("reading the comments of %s to look for its TERMS: line: %w", id, err)
		}
	}
	if hasTermsLine(description, comments) {
		return "", nil
	}
	return fmt.Sprintf("%s: a web source is to be crawled (rig %s sets terms_title and the title matches) and neither its description nor a comment "+
		"holds a line beginning %s: read the host's terms and robots.txt, quote the clause and say whether it allows the crawl, as docs/adding-a-source.md says, "+
		"then put it in a comment on the story as a line beginning %s",
		id, rig, TermsLinePrefix, TermsLinePrefix), nil
}

// checkTerms refuses the release of any of these held stories that crawls a new
// web source (terms_title) and holds no TERMS: line, naming each; nothing has
// been released when it refuses. There is no --force: the way out is the line.
func (r Release) checkTerms(ctx context.Context, stories []StoryDetail) error {
	var refusals []string
	for _, story := range stories {
		line, err := termsRefusal(ctx, r.Rules, r.Tracker.StoryComments, story.Story.ID, story.Merged().Rig, story.Story.Title, story.Description, nil)
		if err != nil {
			return err
		}
		if line != "" {
			refusals = append(refusals, line)
		}
	}
	if len(refusals) == 0 {
		return nil
	}
	return fmt.Errorf("releasing refused, nothing released: %s", strings.Join(refusals, "\n"))
}
