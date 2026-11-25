package application

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// changelogCheckRefusal runs the rig's changelog_check on the story's What's
// new line as the landing will write it, and says why the story must not go on
// when it did not pass (mw-gq6.1610). The landing writes that line into the
// rig's changelog files after the story's own gate, so a test of the rig's shown
// text that refuses a word in it would first fail on the batch, and hold it red.
//
// The line is written, with the version, into a throwaway landing worktree cut
// at the story's branch, by the call the landing writes it with (BumpVersion),
// so that nothing is written on the branch or in the story's worktree, and no
// changelog file of two stories comes to conflict. The command is run there
// whatever earlier pass of the gate is trusted, for none of them saw the line.
//
// It is false when the rig names no changelog_check, keeps no changelog or
// raises no version, when the story says "What's new: none", and when the
// command passed. A story with no What's new line has its title checked, as the
// title is what the landing writes. A rig's file that cannot be read is a note:
// the landing reads it again, and says what is wrong with it there.
func (n Next) changelogCheckRefusal(ctx context.Context, c *closeOut, report *NextReport, base string) (Refusal, bool) {
	if n.Rules == nil {
		return Refusal{}, false
	}
	rules, _, err := n.landingRequirements(ctx, c.path.Rig)
	if err != nil {
		report.Notes = appendNew(report.Notes, fmt.Sprintf("the changelog check was not run, because %s/%s.toml could not be read: %v", RigsDir, c.path.Rig, err))
		return Refusal{}, false
	}
	if rules.ChangelogCheck == "" || len(rules.VersionFiles) == 0 || len(rules.ChangelogFiles) == 0 {
		return Refusal{}, false
	}
	notRun := func(why string, err error) (Refusal, bool) {
		return Refusal{ReasonTestsNotRun, fmt.Sprintf("the rig's changelog check (`%s`) could not be run, %s: %v", rules.ChangelogCheck, why, err), ""}, true
	}
	entry, fromTitle, err := n.changelogNote(ctx, c, report)
	if err != nil {
		return notRun("the story's What's new line being unread", err)
	}
	if entry.Text == "" {
		return Refusal{}, false
	}
	if n.GateParts == nil {
		report.Notes = appendNew(report.Notes, "the changelog check was not run: this mw has nothing to run a command line with")
		return Refusal{}, false
	}

	dir, err := n.Landing.OpenLanding(ctx, c.rigDir, c.branch)
	if err != nil {
		return notRun("a worktree of "+c.branch+" could not be opened for it", err)
	}
	defer func() {
		if closeErr := n.Landing.CloseLanding(ctx, c.rigDir, dir); closeErr != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the landing worktree %s could not be taken away: %v", dir, closeErr))
		}
	}()
	if _, err := n.Landing.BumpVersion(ctx, dir, VersionBump{
		Files: rules.VersionFiles, Before: base, Branch: c.branch, StoryID: c.id,
		Minor: c.path.Bump == domain.BumpMinor, Changelog: rules.ChangelogFiles, Entry: entry,
	}); err != nil {
		return notRun("the line could not be written into "+strings.Join(rules.ChangelogFiles, ", "), err)
	}
	checked, err := n.GateParts.RunLine(WithTestRunFor(ctx, c.id), c.path.Rig, dir, rules.ChangelogCheck)
	if err != nil {
		return notRun("in "+dir, err)
	}
	if checked.Passed {
		return Refusal{}, false
	}
	if checked.NotRun {
		return Refusal{ReasonTestsNotRun, fmt.Sprintf("the rig's changelog check could not be run: `%s` did not start, so this host is missing something the command needs (a toolchain not on its PATH?)", checked.Command),
			"The last lines of `" + checked.Command + "` in " + dir + ":\n\n" + fenced(checked.Tail(CheckLines))}, true
	}
	said := "its What's new line"
	if fromTitle {
		said = "its title, the note for want of a What's new line,"
	}
	why := fmt.Sprintf("the rig's changelog check fails with the story's What's new line written into %s: `%s` did not pass on %s %q; a new closing comment with another What's new: line changes it",
		strings.Join(rules.ChangelogFiles, ", "), checked.Command, said, entry.Text)
	return Refusal{ReasonTestsFail, why, n.failedTestsDetail(ctx, c, report, checked, dir)}, true
}

// changelogEntry is the note a landing writes into the rig's changelog files:
// the story's newest What's new: line, or its title when it has none, under
// today's date (UTC). Its Text is empty when the story says "What's new: none".
// The version is the landing's to fill in.
func (n Next) changelogEntry(ctx context.Context, c *closeOut, report *NextReport) (domain.ChangelogEntry, error) {
	entry, _, err := n.changelogNote(ctx, c, report)
	return entry, err
}

// changelogNote is changelogEntry, and says whether the sentence is the
// story's title for want of a What's new: line, which a batch's fixer is told
// (mw-gq6.942).
func (n Next) changelogNote(ctx context.Context, c *closeOut, report *NextReport) (entry domain.ChangelogEntry, fromTitle bool, err error) {
	comments, err := readRetrying(ctx, n, report, "the comments", func() ([]Comment, error) { return n.Tracker.StoryComments(ctx, c.id) })
	if err != nil {
		return domain.ChangelogEntry{}, false, fmt.Errorf("the comments on %s could not be read for its What's new line: %w", c.id, err)
	}
	texts := make([]string, len(comments))
	for i, comment := range comments {
		texts[i] = comment.Text
	}
	bug := domain.IsBugStory(c.detail.Type, c.detail.Story.Title) || slices.Contains(c.detail.Labels, "bug")
	kind := domain.ChangelogNew
	if bug {
		kind = domain.ChangelogFixed
	}
	note, found := domain.NewestWhatsNew(texts)
	switch {
	case found && note.None:
		return domain.ChangelogEntry{}, false, nil
	case found:
		if note.Kind != "" {
			kind = note.Kind
		}
	default:
		note.Text, fromTitle = domain.NoteFromTitle(c.detail.Story.Title, c.path.Rig), true
	}
	return domain.ChangelogEntry{Date: n.now().UTC().Format("2006-01-02"), Story: c.id, Kind: kind, Text: note.Text}, fromTitle, nil
}
