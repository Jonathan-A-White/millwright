package application

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The tags a Tester's finding is filed under: the same two FINDINGS uses.
const (
	TesterKindBug   = "bug"
	TesterKindTaste = "taste"
)

// TesterProvisionalFix is what a Tester's suggested fix is marked with in the
// story it files: the Mayor confirms it or writes the real one.
const TesterProvisionalFix = "PROVISIONAL (Tester's suggestion; Mayor to confirm)"

// TesterFileTracker is the tracker narrowed to what mw tester file reads and
// writes: the stories of the landed story's epic and rig, and a new held story.
type TesterFileTracker interface {
	WorkTracker
	TesterStories
}

// TesterFinding is one finding a Tester files as a story of its own.
type TesterFinding struct {
	// Kind is TesterKindBug or TesterKindTaste, and tags the title.
	Kind string
	// App is the app's name for the title; the landed story's rig when empty.
	App string
	// Title is the finding in one line.
	Title string
	// Finding is the finding as the FINDINGS comment says it, verbatim.
	Finding string
	// Shot is the shot's path, when there is one.
	Shot string
	// Fix is the Tester's suggested fix in one line, when it has one.
	Fix string
	// HowToCheck is what the Governor would do to see it fixed.
	HowToCheck string
	// Acceptance is checkable by running something.
	Acceptance string
	// Files is the story's files key: the repo paths it expects to change.
	Files string
	// Host is the host the fix is to run on; empty files it for any (auto),
	// whatever host the landed story ran on.
	Host string
	// Class is the slug of the common-errors class the finding is an instance
	// of; empty when none fits. The story is labelled class:<slug>, and a finding
	// of a class an open or held story of the same app already carries joins that
	// story rather than becoming another one (mw-gq6.587).
	Class string
}

// ClassLabelPrefix starts the label a story filed for a common-errors class
// carries: class:<slug>.
const ClassLabelPrefix = "class:"

// TesterFiled is what filing one finding did.
type TesterFiled struct {
	// ID is the held story filed; empty when the finding was a duplicate.
	ID string
	// Duplicate is the open bead the finding matches, when it does, in which
	// case nothing was filed.
	Duplicate      string
	DuplicateTitle string
	// Joined is the open or held story of the same class and app the finding was
	// added to as a comment, when there is one; nothing was filed.
	Joined string
	// Waits are the open or running stories of the rig that name a file this
	// one does, which it waits on.
	Waits []string
	// Chained are the findings still held, filed for the same landed story, that
	// name a file this one does, which it waits on in filing order.
	Chained []string
	// Notes are what could not be done, one line each; they never undo the filing.
	Notes []string
}

// TesterFiler files a Tester's finding: a story held under the landed story's
// epic, so that the Mayor only reads it and releases it, or drops it. It does
// the mechanical part of triage: the path copied from the landed story but for
// the host (auto, unless the Tester names one), a bug at P2 and a taste finding
// at P3, the files key, a wait on every open story of the rig that names the same
// file and on every earlier held finding of the same landing that does, a wait
// of the epic's open demo on the new story, and no story at all for a finding an
// open bead already holds. Releasing stays the Mayor's.
type TesterFiler struct {
	Tracker TesterFileTracker
	// Rules says which label marks the epic's last story; nil leaves the demo be.
	Rules EpicRules
	// Register, when set, is the common-errors register a --class slug must name
	// a class of, in the seat Seat; nil takes any slug.
	Register ErrorFiles
	Seat     string
	// Out is where it says what it did; nil prints nothing.
	Out io.Writer
}

// File files finding for the Tester story testerID, which names the landed
// story on its Landed story line.
func (f TesterFiler) File(ctx context.Context, testerID string, finding TesterFinding) (TesterFiled, error) {
	if f.Tracker == nil {
		return TesterFiled{}, fmt.Errorf("filing a Tester finding: there is no work tracker to file it in")
	}
	finding.Kind = strings.ToLower(strings.TrimSpace(finding.Kind))
	finding.Title = strings.Join(strings.Fields(finding.Title), " ")
	switch {
	case finding.Kind != TesterKindBug && finding.Kind != TesterKindTaste:
		return TesterFiled{}, fmt.Errorf("filing a Tester finding: its kind is %q, and must be %s or %s", finding.Kind, TesterKindBug, TesterKindTaste)
	case finding.Title == "":
		return TesterFiled{}, fmt.Errorf("filing a Tester finding: it needs a one-line title")
	case strings.TrimSpace(finding.Finding) == "":
		return TesterFiled{}, fmt.Errorf("filing a Tester finding: it needs the finding itself, verbatim")
	case strings.TrimSpace(finding.Acceptance) == "":
		return TesterFiled{}, fmt.Errorf("filing a Tester finding: it needs acceptance a run can check")
	case strings.TrimSpace(finding.HowToCheck) == "":
		return TesterFiled{}, fmt.Errorf("filing a Tester finding: it needs a HOW TO CHECK IT")
	}
	finding.Class = strings.TrimSpace(finding.Class)
	if finding.Class != "" {
		if err := checkSlug(finding.Class); err != nil {
			return TesterFiled{}, fmt.Errorf("filing a Tester finding: the class %q may hold only a-z, 0-9 and hyphens: name a slug from mw errors list", finding.Class)
		}
		if f.Register != nil {
			classes, _, err := f.Register.ReadErrorRegister(ctx, f.Seat)
			if err != nil {
				return TesterFiled{}, fmt.Errorf("filing a Tester finding: reading the common-errors register: %w", err)
			}
			if _, known := classes[finding.Class]; !known {
				return TesterFiled{}, fmt.Errorf("filing a Tester finding: no class %q in the common-errors register (mw errors list names them): "+
					"name one, or leave --class off", finding.Class)
			}
		}
	}
	// A bug finding owes a sweep as a bug the Mayor files does (mw-gq6.447).
	if verdict := domain.SweepOfStory(finding.Acceptance, finding.Finding); finding.Kind == TesterKindBug && !verdict.Passes() {
		if verdict == domain.SweepInDescription {
			return TesterFiled{}, fmt.Errorf("filing a Tester finding: its Sweep line is in the finding, which becomes the description, not in --accept: "+
				"move it to --accept, or mw dispatch would refuse it as %s", domain.SweepRuleName)
		}
		return TesterFiled{}, fmt.Errorf("filing a Tester finding: %s: add that line to --accept", verdict.FilingRefusal())
	}

	tester, err := f.Tracker.ShowStory(ctx, testerID)
	if err != nil {
		return TesterFiled{}, fmt.Errorf("reading the Tester story %s: %w", testerID, err)
	}
	if !IsTesterStory(tester) {
		return TesterFiled{}, fmt.Errorf("%s is not a Tester story, so it files no findings", testerID)
	}
	landedID := TesterLanded(tester)
	if landedID == "" {
		return TesterFiled{}, fmt.Errorf("the Tester story %s names no landed story: its description has no %q line", testerID, TesterLandedLine+"<id>")
	}
	landed, err := f.Tracker.ShowStory(ctx, landedID)
	if err != nil {
		return TesterFiled{}, fmt.Errorf("reading the landed story %s: %w", landedID, err)
	}
	path := landed.Merged()
	if path.Rig == "" {
		return TesterFiled{}, fmt.Errorf("the landed story %s has no rig, so there is no path to copy from it: nothing was filed", landedID)
	}
	app := strings.TrimSpace(finding.App)
	if app == "" {
		app = path.Rig
	}
	title := fmt.Sprintf("[%s] %s: %s", finding.Kind, app, finding.Title)

	stories, err := f.Tracker.StoriesSince(ctx, time.Time{})
	if err != nil {
		return TesterFiled{}, fmt.Errorf("reading the open stories of %s: %w", path.Rig, err)
	}
	var filed TesterFiled
	if twin, ok := openTwin(stories, path.Rig, finding.Title); ok {
		filed.Duplicate, filed.DuplicateTitle = twin.Story.ID, twin.Story.Title
		f.say("not filed: %q matches the open bead %s, %q: name it in FINDINGS as 'duplicate of: %s'\n",
			finding.Title, twin.Story.ID, twin.Story.Title, twin.Story.ID)
		return filed, nil
	}

	if finding.Class != "" {
		if mate, ok := classMate(stories, path.Rig, app, finding.Class); ok {
			if err := f.Tracker.CommentOnStory(ctx, mate.Story.ID, testerJoinComment(finding)); err != nil {
				return filed, fmt.Errorf("adding the finding to %s: %w", mate.Story.ID, err)
			}
			filed.Joined = mate.Story.ID
			f.say("joined: %s\n", mate.Story.ID)
			f.say("  nothing filed: %s is open or held with the class %s in %s; the finding is its comment 'Also on: %s'\n",
				mate.Story.ID, finding.Class, app, finding.Title)
			f.say("  put 'joined: %s' under the finding in FINDINGS\n", mate.Story.ID)
			return filed, nil
		}
	}

	path.Files = ""
	path.Host = domain.HostAuto
	if host := strings.TrimSpace(finding.Host); host != "" {
		path.Host = host
	}
	if files := domain.FileList(finding.Files); len(files) > 0 {
		path.Files = strings.Join(files, ",")
	}
	shared := sameFileStories(stories, path)
	filed.Waits = shared
	filed.Chained = heldFindingsNamingSameFile(stories, path, landedID)

	story := NewStory{
		EpicID:      landed.EpicID,
		Standalone:  landed.EpicID == "",
		Title:       title,
		Description: testerFindingDescription(landedID, testerID, finding),
		Acceptance:  strings.TrimSpace(finding.Acceptance),
		Priority:    testerFindingPriority(finding.Kind),
		Overrides:   path,
		Needs:       append(slices.Clone(shared), filed.Chained...),
	}
	if finding.Kind == TesterKindBug {
		story.Labels = []string{LabelBug}
	}
	if finding.Class != "" {
		story.Labels = append(story.Labels, ClassLabelPrefix+finding.Class)
	}
	id, err := f.Tracker.CreateStory(ctx, story)
	if err != nil {
		return filed, fmt.Errorf("filing the finding: %w", err)
	}
	filed.ID = id
	f.say("filed %s, held, under %s: %s\n", id, orStandalone(landed.EpicID), title)
	if len(shared) > 0 {
		f.say("  waits on %s: they name the same file as it\n", strings.Join(shared, ", "))
	}
	if len(filed.Chained) > 0 {
		f.say("  waits on %s: held findings of the same landing that name the same file\n", strings.Join(filed.Chained, ", "))
	}
	if path.Files == "" {
		filed.Notes = append(filed.Notes, fmt.Sprintf("%s has no files key: no wait on stories that name the same file was made", id))
	}
	filed.Notes = append(filed.Notes, HoldDemosForTester(ctx, f.Tracker, f.Rules, path.Rig, landed.EpicID, id)...)
	for _, note := range filed.Notes {
		f.say("  note: %s\n", note)
	}
	f.say("  put 'held: %s' under the finding in FINDINGS\n", id)
	return filed, nil
}

func (f TesterFiler) say(format string, args ...any) {
	if f.Out != nil {
		fmt.Fprintf(f.Out, format, args...)
	}
}

func orStandalone(epic string) string {
	if epic == "" {
		return "no epic"
	}
	return epic
}

// testerFindingDescription is the body of a filed finding: the finding as the
// Tester wrote it, where it came from, the shot, a suggested fix marked as
// provisional, and how the Governor would see it fixed.
func testerFindingDescription(landed, tester string, finding TesterFinding) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(finding.Finding))
	fmt.Fprintf(&b, "\n\n%s. Source: Tester on %s (%s).\n", TesterFindingMarker, landed, tester)
	if shot := strings.TrimSpace(finding.Shot); shot != "" {
		fmt.Fprintf(&b, "Shot: %s\n", shot)
	}
	if fix := strings.Join(strings.Fields(finding.Fix), " "); fix != "" {
		fmt.Fprintf(&b, "Suggested fix: %s: %s\n", TesterProvisionalFix, fix)
	}
	fmt.Fprintf(&b, "\nHOW TO CHECK IT, for the Governor:\n%s\n", strings.TrimSpace(finding.HowToCheck))
	return b.String()
}

// titleWords is a title as the duplicate check compares it: lower case, its
// [tag] left off, only letters and digits, one space between words.
func titleWords(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	for _, tag := range []string{"[" + TesterKindBug + "]", "[" + TesterKindTaste + "]"} {
		title = strings.TrimPrefix(title, tag)
	}
	return strings.Join(nonWord.Split(title, -1), " ")
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// minContained is how long a finding must be, in characters, to count as the
// same as a bead whose title merely holds it: a short one has to match whole.
const minContained = 12

// openTwin is the open bead of rig whose title says what the finding's does:
// the same words, or the finding's inside the bead's title (a bead's own tag
// and the app it names go before them).
func openTwin(stories []StoryDetail, rig, finding string) (StoryDetail, bool) {
	want := strings.TrimSpace(strings.Join(nonWord.Split(strings.ToLower(finding), -1), " "))
	if want == "" {
		return StoryDetail{}, false
	}
	for _, d := range stories {
		if d.IsEpic || d.Closed() || d.Merged().Rig != rig {
			continue
		}
		have := strings.TrimSpace(titleWords(d.Story.Title))
		if have == want || (len(want) >= minContained && strings.Contains(" "+have+" ", " "+want+" ")) ||
			(len(have) >= minContained && strings.Contains(" "+want+" ", " "+have+" ")) {
			return d, true
		}
	}
	return StoryDetail{}, false
}

// sameFileStories are the ids of the stories on path's rig that are running or
// open (a held story is neither) and name a file path's files key does.
func sameFileStories(stories []StoryDetail, path domain.Path) []string {
	ours := domain.FileList(path.Files)
	if len(ours) == 0 {
		return nil
	}
	var ids []string
	for _, d := range stories {
		theirs := d.Merged()
		status := strings.ToLower(strings.TrimSpace(d.Status))
		if d.IsEpic || theirs.Rig != path.Rig || IsTesterStory(d) || (status != StatusOpen && status != StatusInProgress) {
			continue
		}
		if _, shared := domain.SharedFile(ours, domain.FileList(theirs.Files)); shared {
			ids = append(ids, d.Story.ID)
		}
	}
	return ids
}

// testerFindingPriority is the priority a finding is filed at: a bug is P2,
// as any story is by default, and a taste finding P3 (mw-gq6.542).
func testerFindingPriority(kind string) int {
	if kind == TesterKindTaste {
		return 3
	}
	return DefaultPriority
}

// heldFindingsNamingSameFile are the ids of the findings still held that a
// Tester filed for the landed story, in filing order, and that name a file
// path's files key does.
func heldFindingsNamingSameFile(stories []StoryDetail, path domain.Path, landedID string) []string {
	ours := domain.FileList(path.Files)
	if len(ours) == 0 {
		return nil
	}
	source := "Tester on " + landedID + " ("
	var ids []string
	for _, d := range stories {
		theirs := d.Merged()
		if d.IsEpic || theirs.Rig != path.Rig || strings.ToLower(strings.TrimSpace(d.Status)) != StatusHeld ||
			!strings.Contains(d.Description, TesterFindingMarker) || !strings.Contains(d.Description, source) {
			continue
		}
		if _, shared := domain.SharedFile(ours, domain.FileList(theirs.Files)); shared {
			ids = append(ids, d.Story.ID)
		}
	}
	return ids
}

// classMate is the story of rig that is not closed, is no epic or Tester story,
// carries the label class:<class> and names app in its title: the story a
// finding of that class in that app joins. The oldest wins, which is the first
// the tracker lists.
func classMate(stories []StoryDetail, rig, app, class string) (StoryDetail, bool) {
	for _, d := range stories {
		if d.IsEpic || d.Closed() || IsTesterStory(d) || d.Merged().Rig != rig || !hasLabel(d.Labels, ClassLabelPrefix+class) {
			continue
		}
		if titleApp(d.Story.Title) == strings.ToLower(app) {
			return d, true
		}
	}
	return StoryDetail{}, false
}

// titleApp is the app a filed finding's title names: '[bug] <app>: <title>'.
func titleApp(title string) string {
	title = strings.TrimSpace(title)
	for _, tag := range []string{"[" + TesterKindBug + "]", "[" + TesterKindTaste + "]"} {
		title = strings.TrimSpace(strings.TrimPrefix(title, tag))
	}
	app, _, found := strings.Cut(title, ":")
	if !found {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(app))
}

// testerJoinComment is what a finding that joins a story of its class adds to
// it: the finding, its shot, how to check it and its acceptance, under the
// screen it is also on (the finding's one-line title).
func testerJoinComment(finding TesterFinding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Also on: %s\n\n%s\n", finding.Title, strings.TrimSpace(finding.Finding))
	if shot := strings.TrimSpace(finding.Shot); shot != "" {
		fmt.Fprintf(&b, "\nShot: %s\n", shot)
	}
	fmt.Fprintf(&b, "\nHOW TO CHECK IT, for the Governor:\n%s\n", strings.TrimSpace(finding.HowToCheck))
	fmt.Fprintf(&b, "\nAccept:\n%s\n", strings.TrimSpace(finding.Acceptance))
	return b.String()
}
