package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// The labels a bug found by the whole suite carries: bug, as every bug filed by
// hand does, and suite, which the guard against a second bug for the same file
// looks for.
const (
	LabelBug   = "bug"
	LabelSuite = "suite"
)

// flakyOnMain is what a flaky file's bug title says of it after the file's name.
const flakyOnMain = " is flaky on main"

// SuiteFoundBy ends the description of every bug the whole suite files.
const SuiteFoundBy = "Found by the nightly whole suite (mw suite); no story's gate ran this file."

// SuiteFiled is what --file did with one failing file: the bead it is on and
// whether the bead was filed for it now (New) or already open, and was written
// on instead.
type SuiteFiled struct {
	ID   string
	File string
	New  bool
}

// SuiteFiler is what mw suite --file does after a run: one held bug under the
// rig's suite_epic for each failing file, a dated comment instead for a file
// that already has an open one, and one mail to the Mayor.
type SuiteFiler struct {
	Tracker WorkTracker
	Rules   EpicRules
	Mailbox Mailbox
	Host    string
}

// Epic is the rig's suite_epic, or the refusal that it names none. It reads
// nothing but the rig's epic rules, so that a run can be refused before it
// starts.
func (f SuiteFiler) Epic(ctx context.Context, rig string) (string, error) {
	if f.Rules == nil {
		return "", fmt.Errorf("--file needs to know the rig's suite_epic, and no epic rules were given")
	}
	rules, err := f.Rules.EpicRequirements(ctx, rig)
	if err != nil {
		return "", fmt.Errorf("reading what the rig %s requires of its epics: %w", rig, err)
	}
	if strings.TrimSpace(rules.SuiteEpic) == "" {
		return "", fmt.Errorf("the rig %s has no suite_epic in its rules file in the vault: --file has no epic to file the failing files under", rig)
	}
	return strings.TrimSpace(rules.SuiteEpic), nil
}

// File writes down what report found, under epic: for each failing file the
// bead that holds it, and then the one mail to the Mayor. A file that could not
// be filed does not stop the others; its failure comes back with what was done.
func (f SuiteFiler) File(ctx context.Context, epic string, report SuiteReport) ([]SuiteFiled, error) {
	result := report.Result
	var filed []SuiteFiled
	var failures []string
	if len(result.Failing) > 0 {
		detail, err := f.Tracker.ShowEpic(ctx, epic)
		if err != nil {
			return nil, fmt.Errorf("reading the suite epic %s: %w", epic, err)
		}
		for _, file := range result.Failing {
			one, err := f.fileOne(ctx, detail, report, file)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", file.File, err))
				continue
			}
			filed = append(filed, one)
		}
	}

	if _, err := f.Mailbox.Send(ctx, suiteMail(f.Host, report, filed, failures)); err != nil {
		failures = append(failures, fmt.Sprintf("the mail to %s could not be sent: %v", MayorMailbox, err))
	}
	if len(failures) > 0 {
		return filed, fmt.Errorf("mw suite --file did not finish:\n  %s", strings.Join(failures, "\n  "))
	}
	return filed, nil
}

// fileOne puts one failing file on a bead: the open one that already names it,
// or a new held bug.
func (f SuiteFiler) fileOne(ctx context.Context, epic EpicDetail, report SuiteReport, file SuiteFile) (SuiteFiled, error) {
	if story, open := openSuiteBug(epic, file.File); open {
		if err := f.Tracker.CommentOnStory(ctx, story.Story.ID, suiteComment(report, file)); err != nil {
			return SuiteFiled{}, fmt.Errorf("commenting on %s: %w", story.Story.ID, err)
		}
		return SuiteFiled{ID: story.Story.ID, File: file.File}, nil
	}

	// The bug takes its path from the epic as a hand-filed story does: the
	// epic's seven keys copied on, and the file it is about as its own.
	path := epic.Defaults
	path.Files = file.File
	id, err := f.Tracker.CreateStory(ctx, NewStory{
		EpicID:      epic.ID,
		Title:       suiteBugTitle(report.Result.Rig, file),
		Description: suiteBugDescription(report, file),
		Acceptance:  fmt.Sprintf("1. %s passes on main 5 runs in a row (a shell loop, uptime beside each). 2. The rig's gate passes.", file.File),
		Overrides:   path,
		Labels:      []string{LabelBug, LabelSuite},
	})
	if err != nil {
		return SuiteFiled{}, fmt.Errorf("filing the bug: %w", err)
	}
	return SuiteFiled{ID: id, File: file.File, New: true}, nil
}

// openSuiteBug is the open bead under the suite epic, labelled suite, whose
// title names file: the one a run's failure of the file is written on, and the
// one mw status names beside the failing count.
func openSuiteBug(epic EpicDetail, file string) (StoryDetail, bool) {
	for _, story := range epic.Stories {
		if story.Status != StatusClosed && carriesAny(story.Labels, []string{LabelSuite}) && namesFile(story.Story.Title, file) {
			return story, true
		}
	}
	return StoryDetail{}, false
}

// namesFile reports whether a title names file as a word of its own, so that
// src/a.test.ts is not found in src/data.test.ts's bug.
func namesFile(title, file string) bool {
	for _, word := range strings.Fields(title) {
		if strings.Trim(word, ":,;.()") == file {
			return true
		}
	}
	return false
}

func suiteBugTitle(rig string, file SuiteFile) string {
	if file.Flaky {
		return fmt.Sprintf("[bug] %s main: %s%s (failed, then passed alone)", rig, file.File, flakyOnMain)
	}
	return fmt.Sprintf("[bug] %s main: %s fails on main", rig, file.File)
}

// suiteHow says in a phrase how a file went.
func suiteHow(file SuiteFile) string {
	if file.Flaky {
		return "flaky (failed in the whole run, then passed alone)"
	}
	return "failed twice (in the whole run and again alone)"
}

func suiteDay(report SuiteReport) string {
	if len(report.Result.Started) >= len("2006-01-02") {
		return report.Result.Started[:len("2006-01-02")]
	}
	return report.Result.Started
}

func suiteBugDescription(report SuiteReport, file SuiteFile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s on %s main at %s: %s.\n\n", file.File, report.Result.Rig, report.Result.Sha, suiteHow(file))
	if file.Test != "" {
		fmt.Fprintf(&b, "First failing test: %s\n\n", file.Test)
	}
	if file.Error != "" {
		fmt.Fprintf(&b, "First error lines:\n%s\n\n", file.Error)
	}
	fmt.Fprintf(&b, "Main's sha: %s\n", report.Result.Sha)
	fmt.Fprintf(&b, "The day's log, in the vault: %s\n\n", report.LogPath)
	b.WriteString(SuiteFoundBy)
	return b.String()
}

func suiteComment(report SuiteReport, file SuiteFile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Suite %s: %s again on main at %s, %s.", suiteDay(report), file.File, report.Result.Sha, suiteHow(file))
	if file.Error != "" {
		fmt.Fprintf(&b, "\nFirst error lines:\n%s", file.Error)
	}
	fmt.Fprintf(&b, "\nThe day's log, in the vault: %s", report.LogPath)
	return b.String()
}

// suiteMail is the one mail to the Mayor: the subject says how many files
// failed, the body names each bead with its file and whether it is new.
func suiteMail(host string, report SuiteReport, filed []SuiteFiled, failures []string) NewMessage {
	result := report.Result
	failing := len(result.Failing) + len(result.Unnamed)
	subject := fmt.Sprintf("Suite: %s main %s: all pass", result.Rig, short(result.Sha))
	if failing > 0 {
		subject = fmt.Sprintf("Suite: %s main %s: %d failing", result.Rig, short(result.Sha), failing)
	}

	var b strings.Builder
	if failing == 0 {
		fmt.Fprintf(&b, "The whole suite of %s passed on main at %s.\n", result.Rig, result.Sha)
	}
	for _, one := range filed {
		how := "commented (an open bug already names the file)"
		if one.New {
			how = "new, held"
		}
		fmt.Fprintf(&b, "%s  %s  %s\n", one.ID, one.File, how)
	}
	for _, said := range result.Unnamed {
		fmt.Fprintf(&b, "no file  %s\n", said)
	}
	for _, failure := range failures {
		fmt.Fprintf(&b, "not filed  %s\n", failure)
	}
	if report.LogPath != "" {
		fmt.Fprintf(&b, "\nThe day's log, in the vault: %s\n", report.LogPath)
	}
	return NewMessage{From: SeatIdentity(MwSeat, host), To: MayorMailbox, Subject: subject, Body: b.String()}
}

// oneTestFile is a files key that is a single test file and nothing else.
var oneTestFile = regexp.MustCompile("^" + testFileName.String() + "$")

// suiteSpec is the one spec a nightly-suite bug names: the story carries
// LabelSuite and its files key (files, the story's path's) is exactly one test
// file, no glob and no list. Any other story is never re-run first, nor is a
// flaky file's bug, which one pass on main does not fix: its acceptance is five
// in a row.
func suiteSpec(detail StoryDetail, files string) (string, bool) {
	files = strings.TrimSpace(files)
	if !carriesAny(detail.Labels, []string{LabelSuite}) || !oneTestFile.MatchString(files) ||
		strings.Contains(detail.Story.Title, files+flakyOnMain) {
		return "", false
	}
	return files, true
}
