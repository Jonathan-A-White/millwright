package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// DefaultFoldMin is the size a group of bugs must reach to be listed.
const DefaultFoldMin = 3

// FoldRule is the rule the Mayor folds by, said in the command's help: bugs
// that share a cause are fixed once, each bug's own check is copied into the
// fold's acceptance criteria, and mw fold supersede closes the bug as superseded.
const FoldRule = "Fold rule: a folded bug's check is copied into the fold's acceptance criteria and the bug is closed as superseded by the fold."

// Fold is `mw fold <rig>`: a rig's open and held bugs grouped by the error class
// they carry (the class:<slug> label) or, with none, by the screen their title
// names, the groups of Min or more first. A bug that already waits on a fold
// story is folded and left out. It runs no model and writes nothing;
// it is the list the Mayor's weekly fold pass starts from (mw-gq6.588).
type Fold struct {
	Tracker TesterStories
	Rig     string
	// Min is the smallest group listed; zero takes DefaultFoldMin.
	Min int
	Out io.Writer
}

// FoldBug is one bug in a group.
type FoldBug struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// InProgress is true while a session works the bug: it is shown, not folded.
	InProgress bool `json:"in_progress,omitempty"`
}

// FoldGroup is the bugs that share one key.
type FoldGroup struct {
	// Key is class:<slug> or screen:<name>.
	Key  string    `json:"key"`
	Bugs []FoldBug `json:"bugs"`
}

// FoldReport is what mw fold found.
type FoldReport struct {
	Rig    string      `json:"rig"`
	Min    int         `json:"min"`
	Bugs   int         `json:"bugs"`
	Groups []FoldGroup `json:"groups"`
}

const foldScreenPrefix = "screen:"

// Run reads the rig's stories and prints the groups.
func (f Fold) Run(ctx context.Context) (FoldReport, error) {
	minSize := f.Min
	if minSize <= 0 {
		minSize = DefaultFoldMin
	}
	report := FoldReport{Rig: f.Rig, Min: minSize, Groups: []FoldGroup{}}
	if f.Tracker == nil {
		return report, fmt.Errorf("folding the bugs of %s: there is no work tracker to read", f.Rig)
	}
	stories, err := f.Tracker.StoriesSince(ctx, time.Time{})
	if err != nil {
		return report, fmt.Errorf("reading the bugs of %s: %w", f.Rig, err)
	}
	openFolds := map[string]bool{}
	for _, d := range stories {
		if isFoldTitle(d.Story.Title, f.Rig) && strings.ToLower(strings.TrimSpace(d.Status)) != StatusClosed {
			openFolds[d.Story.ID] = true
		}
	}
	byKey := map[string]*FoldGroup{}
	var order []string
	for _, d := range stories {
		status := strings.ToLower(strings.TrimSpace(d.Status))
		if d.IsEpic || IsTesterStory(d) || d.Iced() || d.Merged().Rig != f.Rig || (status != StatusOpen && status != StatusInProgress && status != StatusHeld) {
			continue
		}
		if !hasLabel(d.Labels, LabelBug) && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(d.Story.Title)), "[bug]") {
			continue
		}
		if isFoldTitle(d.Story.Title, f.Rig) || waitsOnFold(d, openFolds) {
			continue
		}
		report.Bugs++
		key := foldKey(d, f.Rig)
		if key == "" {
			continue
		}
		slot := strings.ToLower(key)
		if byKey[slot] == nil {
			byKey[slot] = &FoldGroup{Key: key}
			order = append(order, slot)
		}
		byKey[slot].Bugs = append(byKey[slot].Bugs, FoldBug{ID: d.Story.ID, Title: d.Story.Title, InProgress: status == StatusInProgress})
	}
	for _, slot := range order {
		if group := byKey[slot]; len(group.Bugs) >= minSize {
			report.Groups = append(report.Groups, *group)
		}
	}
	sort.SliceStable(report.Groups, func(i, j int) bool {
		a, b := report.Groups[i], report.Groups[j]
		if len(a.Bugs) != len(b.Bugs) {
			return len(a.Bugs) > len(b.Bugs)
		}
		aClass, bClass := !strings.HasPrefix(a.Key, foldScreenPrefix), !strings.HasPrefix(b.Key, foldScreenPrefix)
		if aClass != bClass {
			return aClass
		}
		return a.Key < b.Key
	})
	f.print(report)
	return report, nil
}

func (f Fold) print(report FoldReport) {
	if f.Out == nil {
		return
	}
	if len(report.Groups) == 0 {
		fmt.Fprintf(f.Out, "%s: no group of %d or more among its %d open and held bugs\n", report.Rig, report.Min, report.Bugs)
		return
	}
	fmt.Fprintf(f.Out, "%s: %d groups of %d or more among its %d open and held bugs\n", report.Rig, len(report.Groups), report.Min, report.Bugs)
	for _, group := range report.Groups {
		fmt.Fprintf(f.Out, "\n%s (%d)\n", group.Key, len(group.Bugs))
		for _, bug := range group.Bugs {
			mark := ""
			if bug.InProgress {
				mark = "  [in progress: do not fold]"
			}
			fmt.Fprintf(f.Out, "  %s  %s%s\n", bug.ID, bug.Title, mark)
		}
	}
	fmt.Fprintf(f.Out, "\n%s\n", FoldRule)
}

// waitsOnFold is true for a bug with a blocker that is a fold still open: the
// fold rule makes each folded bug wait on its fold story, so it is already
// folded and is not listed as a bug to fold (mw-gq6.591).
func waitsOnFold(d StoryDetail, openFolds map[string]bool) bool {
	for _, need := range d.Needs {
		if openFolds[need] {
			return true
		}
	}
	return false
}

// isFoldTitle is true for a story that is itself a fold: the words after the
// app name end in 'fold' before the colon ('[bug] lampas: Lampas fold: ...'). A
// fold carries its folded bugs as blockers, so it is left out of the groups.
func isFoldTitle(title, rig string) bool {
	screen := strings.ToLower(titleScreen(title, rig))
	return screen == "fold" || strings.HasSuffix(screen, " fold")
}

// foldKey is the key a bug is grouped by: its class:<slug> label, else the
// screen its title names, else empty.
func foldKey(d StoryDetail, rig string) string {
	for _, label := range d.Labels {
		if strings.HasPrefix(label, ClassLabelPrefix) && strings.TrimSpace(strings.TrimPrefix(label, ClassLabelPrefix)) != "" {
			return label
		}
	}
	if screen := titleScreen(d.Story.Title, rig); screen != "" {
		return foldScreenPrefix + screen
	}
	return ""
}

// titleScreen is the screen a bug's title names: the words before the first
// ':' that follows the app name ('[bug] lampas: Lampas Review: card overflows'
// and 'Lampas Review: card overflows' both name Lampas Review). The app name is
// the first part when it is the rig, or when a [bug] tag precedes two colons;
// a title with no colon after it names no screen.
func titleScreen(title, rig string) string {
	title = strings.TrimSpace(title)
	tagged := false
	for _, tag := range []string{"[" + TesterKindBug + "]", "[" + TesterKindTaste + "]"} {
		if rest := strings.TrimSpace(strings.TrimPrefix(title, tag)); rest != title {
			title, tagged = rest, true
		}
	}
	head, rest, found := strings.Cut(title, ":")
	if !found {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(head), rig) || (tagged && strings.Contains(rest, ":")) {
		head, _, found = strings.Cut(rest, ":")
		if !found {
			return ""
		}
	}
	return strings.TrimSpace(head)
}
