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
	// FixedOnMain is how the finding's acceptance passes on main already, when it
	// does; nothing was filed.
	FixedOnMain string
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
	// Unwritten are the waits (Waits and Chained) the tracker would not write
	// after the story was filed, with why, one line each (mw-gq6.1467): the story
	// is filed and held all the same, and the filing fails naming them.
	Unwritten []string
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
	// Retired, CloudBook and CloudPlan say which hosts cannot take a story: a
	// finding that names a retired host, or a cloud box the book no longer
	// holds, is filed with host=auto and the note says so (mw-gq6.1203). Nil
	// and empty judge nothing.
	Retired   []string
	CloudBook CloudBook
	CloudPlan CloudPlan
	// Main, when it is set, reads a bug finding's acceptance on the rig's origin
	// as merged before the finding is filed: a command it names that is cheap and
	// deterministic and passes there is a finding fixed already, and nothing is
	// filed (mw-gq6.1245). GateLock is the lock that run is made under, and Host
	// the host it is made on. Nil Main files every finding.
	Main     FindingRechecker
	GateLock GateLock
	Host     string
	// Out is where it says what it did; nil prints nothing.
	Out io.Writer
	// Pause waits between the tries of a wait the tracker could not write; nil
	// waits for real.
	Pause func(context.Context, time.Duration) error
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
	// The landed story's size, bump and callers_ok are its own, not a finding's
	// (mw-gq6.1227), and so are its model and effort (mw-gq6.1551).
	path := FiledPath(landed.Merged(), "", finding.Kind)
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

	how, failedAt := f.fixedOnMain(ctx, testerID, path.Rig, TesterLandedCommit(tester), finding)
	if how != "" {
		filed.FixedOnMain = how
		f.say("not filed: already fixed on main: %s (%s)\n", finding.Title, how)
		f.say("  put 'already fixed on main' under the finding in FINDINGS\n")
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
		if why := HostRefusedWhy(ctx, host, f.Retired, f.CloudBook, f.CloudPlan); why != "" {
			f.say("host=%s was not written: %s; host=auto was written instead\n", host, why)
			path.Host = domain.HostAuto
		}
	}
	// An epic that says why its host stays keeps it for the finding too: the
	// host written above is replaced by the epic's own, and a host the Tester
	// named is not (mw-gq6.1288).
	if host, why := EpicKeepsHost(landed.Defaults); host != "" {
		if named := strings.TrimSpace(finding.Host); named != "" && named != host {
			f.say("host=%s was not written: %s must stay on host=%s (%s); the finding is filed there\n", named, landed.EpicID, host, why)
		}
		path.Host = ""
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
		Description: testerFindingDescription(landedID, testerID, finding, failedAt),
		Acceptance:  strings.TrimSpace(finding.Acceptance),
		Priority:    testerFindingPriority(finding.Kind),
		Overrides:   path,
	}
	if finding.Kind == TesterKindBug {
		story.Labels = []string{LabelBug}
	}
	if finding.Class != "" {
		story.Labels = append(story.Labels, ClassLabelPrefix+finding.Class)
	}
	// The story is created with no waits and each wait is added after: bd create
	// --deps checks every dependency for a cycle inside the create, and when that
	// read times out on a busy server the create is undone and nothing is filed
	// (mw-gq6.1467). A wait that still fails is named, not fatal to the filing.
	if why := UnderOpenParent(ctx, f.Tracker, &story); why != "" {
		f.say("filed %s\n", why)
	}
	id, err := f.Tracker.CreateStory(ctx, story)
	if err != nil {
		return filed, fmt.Errorf("filing the finding: %w", err)
	}
	filed.ID = id
	f.say("filed %s, held, under %s: %s\n", id, orStandalone(landed.EpicID), title)
	waits := append(slices.Clone(shared), filed.Chained...)
	written, unwritten := f.writeWaits(ctx, id, waits)
	filed.Unwritten = unwritten
	shared = slices.DeleteFunc(slices.Clone(shared), func(need string) bool { return !slices.Contains(written, need) })
	filed.Chained = slices.DeleteFunc(slices.Clone(filed.Chained), func(need string) bool { return !slices.Contains(written, need) })
	filed.Waits = shared
	// A criterion no Builder can run before landing is warned of as mw file and
	// mw path set warn of it (PathSet.notRunnableWarning, mw-gq6.1411).
	if line := (PathSet{Rules: f.Rules}).notRunnableWarning(ctx, id, path.Rig, story.Acceptance); line != "" {
		f.say("%s\n", line)
	}
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
	if len(unwritten) > 0 {
		return filed, fmt.Errorf("filed %s, held, but %d of its waits could not be written, so it does not yet wait on them: %s; "+
			"make each with bd dep add %s <id> when the server is calmer", id, len(unwritten), strings.Join(unwritten, "; "), id)
	}
	return filed, nil
}

// WaitTries is how many times a wait the tracker could not write for a passing
// fault (a cycle check that timed out) is tried, and waitFirstPause the first
// pause between tries, doubled for each one after it.
const (
	WaitTries      = 4
	waitFirstPause = 3 * time.Second
)

// writeWaits makes story id wait on each need, one at a time, trying a need
// again after a growing pause when the tracker failed on a passing fault. It
// returns the needs written and, for each that was not, a line 'need: why'.
func (f TesterFiler) writeWaits(ctx context.Context, id string, needs []string) (written, unwritten []string) {
	return writeWaits(ctx, f.Tracker, f.Pause, f.say, id, needs)
}

// writeWaits is what TesterFiler.writeWaits and File do: a nil pause waits for
// real, and say tells each try again.
func writeWaits(ctx context.Context, tracker interface {
	AddBlocker(ctx context.Context, blocked, blocker string) error
}, pause func(context.Context, time.Duration) error, say func(string, ...any), id string, needs []string) (written, unwritten []string) {
	if pause == nil {
		pause = waitFor
	}
	for _, need := range needs {
		var err error
		delay := waitFirstPause
		for try := 1; try <= WaitTries; try++ {
			if err = tracker.AddBlocker(ctx, id, need); err == nil || !passingDepFault(err) || try == WaitTries {
				break
			}
			say("  note: %s could not be made to wait on %s (%s), trying again in %s\n", id, need, firstLine(err.Error()), delay)
			if waitErr := pause(ctx, delay); waitErr != nil {
				err = fmt.Errorf("%w (and waiting to try again: %v)", err, waitErr)
				break
			}
			delay *= 2
		}
		if err != nil {
			unwritten = append(unwritten, fmt.Sprintf("%s: %s", need, firstLine(err.Error())))
			continue
		}
		written = append(written, need)
	}
	return written, unwritten
}

// passingDepFault says err is a dependency write that failed on the server and
// not on a refusal: a cycle check whose read timed out. A cycle bd found is a
// refusal and is not tried again.
func passingDepFault(err error) bool {
	said := strings.ToLower(err.Error())
	if strings.Contains(said, "would create a cycle") {
		return false
	}
	for _, fault := range append([]string{"invalid connection", "failed to check for dependency cycle", "timeout"}, bdPassingFaults...) {
		if strings.Contains(said, fault) {
			return true
		}
	}
	return false
}

// fixedOnMain is how the acceptance of a bug finding passes on main as merged
// now, or empty: for a taste finding, an acceptance that names no cheap command,
// or a read of main that could not be made, which is said and files the finding.
// failedAt is the commit of main the command was seen to fail at, which the
// story filed names as its main's sha (mw-gq6.1742); it is empty when the
// command was not seen to fail.
func (f TesterFiler) fixedOnMain(ctx context.Context, testerID, rig, seenAt string, finding TesterFinding) (how, failedAt string) {
	if f.Main == nil || finding.Kind != TesterKindBug {
		return "", ""
	}
	command := ""
	for _, line := range strings.Split(finding.Acceptance, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "sweep:") {
			continue
		}
		if command = foundCheckCommand(line); command != "" {
			break
		}
	}
	if command == "" {
		return "", ""
	}
	ask := &foundSpecAsk{checks: f.Main, command: command}
	askCtx, cancel := context.WithTimeout(ctx, foundCheckBound)
	defer cancel()
	_, err := rerunUnderGate(askCtx, ask, f.GateLock, DispatchGateWait, f.Host, testerID, rig, "", func(note string) { f.say("  note: %s\n", note) })
	if err != nil {
		f.say("  note: the finding was not read against main, so it is filed: %s\n", firstLine(err.Error()))
		return "", ""
	}
	if ask.answer.Ran && !ask.answer.Passed {
		return "", ask.answer.Sha
	}
	fixed, how := foundFixedOnMain(command, nil, ask.answer)
	if !fixed {
		return "", ""
	}
	// A command that passes on main is a fix only if it failed where the Tester
	// saw the fault: the commit its story names (mw-gq6.1736).
	if !f.reproducedWhereSeen(ctx, testerID, rig, seenAt, command) {
		return "", ""
	}
	return how, ""
}

// reproducedWhereSeen says whether command shows the fault at seenAt, the commit
// the Tester story names: it failed there, or it named a test that commit did
// not have. When it passed there, or the commit could not be read, a note says
// why and the finding is filed.
func (f TesterFiler) reproducedWhereSeen(ctx context.Context, testerID, rig, seenAt, command string) bool {
	at, ok := f.Main.(FindingRecheckerAt)
	if !ok || seenAt == "" {
		f.say("  note: the command `%s` was not run where the Tester saw the fault, so it is not known to reproduce the finding and it is filed\n", command)
		return false
	}
	askCtx, cancel := context.WithTimeout(ctx, foundCheckBound)
	defer cancel()
	var answer FindingAnswer
	_, err := rerunUnderGate(askCtx, foundAtAsk{func(ctx context.Context, rig string) (FindingAnswer, error) {
		return at.RecheckFindingAt(ctx, rig, seenAt, command)
	}, &answer}, f.GateLock, DispatchGateWait, f.Host, testerID, rig, "", func(note string) { f.say("  note: %s\n", note) })
	if err != nil {
		f.say("  note: the command `%s` was not run at %s, where the Tester saw the fault, so the finding is filed: %s\n", command, short(seenAt), firstLine(err.Error()))
		return false
	}
	if !answer.Ran {
		f.say("  note: the command `%s` did not run at %s, where the Tester saw the fault, so the finding is filed\n", command, short(seenAt))
		return false
	}
	ran := !foundNoTestsToRun.MatchString(answer.Tail) || !strings.HasPrefix(command, "go test")
	if answer.Passed && ran {
		f.say("  note: the command `%s` passes at %s, where the Tester saw the fault, so it does not reproduce the finding; it is filed\n", command, short(seenAt))
		return false
	}
	return true
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
// provisional, and how the Governor would see it fixed. failedAt is the commit
// the finding's command was seen to fail at, when it was: the story then ends
// with the line 'Main's sha: <commit>' that mw suite and mw next write, so a
// dispatch that finds the check passing on a main that has moved closes it as
// fixed, and holds it when main is that very commit. A story with no such line
// is one nothing showed to have failed (its command passed where the Tester saw
// the fault, did not run, or named none), and a dispatch holds it when its check
// passes (mw-gq6.1742).
func testerFindingDescription(landed, tester string, finding TesterFinding, failedAt string) string {
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
	if failedAt = strings.TrimSpace(failedAt); failedAt != "" {
		fmt.Fprintf(&b, "\nMain's sha: %s\n", failedAt)
	}
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
