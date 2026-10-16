package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TriageSizeField is the metadata a story's size is kept in, once the size story
// lands; a story with none dumps an empty size.
const TriageSizeField = "size"

// TriageStaleDays is how many days without an update make a bead stale in a
// diff: newly stale is a bead that crossed it between two dumps.
const TriageStaleDays = 14

// TriageDescriptionRunes is how much of a description a dump line carries.
const TriageDescriptionRunes = 500

// TriageBead is one bead as the triage pass reads it: every bead the tracker
// holds except mail and events, closed ones included, because a dump has to
// know whether what a bead waits on is finished and the orphan sweep has to
// know whether a story is.
type TriageBead struct {
	ID          string
	Title       string
	Type        string
	Status      string
	Priority    int
	Labels      []string
	Rig         string
	Size        string
	Parent      string
	Description string
	Created     time.Time
	Updated     time.Time
	ClosedAt    time.Time
	// Needs are the beads this one waits on, the link to its parent left out.
	Needs []string
	// Molecule is the root of the formula poured for this story, "" for a bead
	// no formula was poured for.
	Molecule string
}

// TriageSource reads every bead for the triage pass. It reads and writes
// nothing.
type TriageSource interface {
	TriageBeads(ctx context.Context) ([]TriageBead, error)
}

// TriageCloser is the one write the orphan sweep makes.
type TriageCloser interface {
	CloseStory(ctx context.Context, id, reason string) error
}

// isFormulaBead is whether the id is one of a poured formula's beads (a root or
// a step), which is never work of its own.
func isFormulaBead(b TriageBead) bool {
	return strings.Contains(b.ID, "-mol-") || b.Type == "molecule"
}

// isWork is whether a bead is a live work bead: not closed, not mail, event or
// formula.
func isWork(b TriageBead) bool {
	if strings.EqualFold(b.Status, StatusClosed) || isFormulaBead(b) {
		return false
	}
	return b.Type != "mail" && b.Type != "event"
}

// TriageLine is one live work bead as a dump line says it, and the unit a diff
// compares.
type TriageLine struct {
	ID              string   `json:"id"`
	Type            string   `json:"type"`
	Status          string   `json:"status"`
	Priority        int      `json:"priority"`
	Labels          []string `json:"labels"`
	Rig             string   `json:"rig"`
	Size            string   `json:"size"`
	Parent          string   `json:"parent"`
	Created         string   `json:"created"`
	Updated         string   `json:"updated"`
	DaysSinceUpdate int      `json:"days_since_update"`
	Ready           bool     `json:"ready"`
	DaysReady       int      `json:"days_ready"`
	Deps            []string `json:"deps"`
	Dependents      []string `json:"dependents"`
	Description     string   `json:"description"`
	HisPriority     bool     `json:"his_priority"`
}

// Stale is whether the bead has gone TriageStaleDays without an update.
func (l TriageLine) Stale() bool { return l.DaysSinceUpdate >= TriageStaleDays }

// TriageDump writes one JSON line per live work bead: the one shape the nightly
// triage pass reads its beads in. Only the writer is written to.
type TriageDump struct {
	Source TriageSource
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Run reads the beads and returns the dump lines in id order, writing each as a
// line of JSON to w when w is not nil.
func (d TriageDump) Run(ctx context.Context, w io.Writer) ([]TriageLine, error) {
	if d.Source == nil {
		return nil, fmt.Errorf("triage dump: there is no work tracker to read from")
	}
	beads, err := d.Source.TriageBeads(ctx)
	if err != nil {
		return nil, fmt.Errorf("triage dump: %w", err)
	}
	now := time.Now()
	if d.Now != nil {
		now = d.Now()
	}
	lines := triageLines(beads, now)
	if w != nil {
		enc := json.NewEncoder(w)
		for _, line := range lines {
			if err := enc.Encode(line); err != nil {
				return nil, fmt.Errorf("triage dump: %w", err)
			}
		}
	}
	return lines, nil
}

func triageLines(beads []TriageBead, now time.Time) []TriageLine {
	byID := make(map[string]TriageBead, len(beads))
	for _, b := range beads {
		byID[b.ID] = b
	}
	var lines []TriageLine
	dependents := map[string][]string{}
	for _, b := range beads {
		if !isWork(b) {
			continue
		}
		line := TriageLine{
			ID: b.ID, Type: b.Type, Status: b.Status, Priority: b.Priority,
			Labels: sortedCopy(b.Labels), Rig: b.Rig, Size: b.Size, Parent: b.Parent,
			Created: formatTriageTime(b.Created), Updated: formatTriageTime(b.Updated),
			Description: clipRunes(b.Description, TriageDescriptionRunes),
			Deps:        []string{}, Dependents: []string{},
		}
		line.HisPriority = HisPriority(b.Description)
		updated := b.Updated
		if updated.IsZero() {
			updated = b.Created
		}
		line.DaysSinceUpdate = wholeDays(now, updated)

		readyFrom := b.Created
		for _, need := range b.Needs {
			if blocker, known := byID[need]; known && strings.EqualFold(blocker.Status, StatusClosed) {
				if blocker.ClosedAt.After(readyFrom) {
					readyFrom = blocker.ClosedAt
				}
				continue
			}
			line.Deps = append(line.Deps, need)
			dependents[need] = append(dependents[need], b.ID)
		}
		sort.Strings(line.Deps)
		line.Ready = strings.EqualFold(b.Status, StatusOpen) && len(line.Deps) == 0
		if line.Ready {
			line.DaysReady = wholeDays(now, readyFrom)
		}
		lines = append(lines, line)
	}
	for i := range lines {
		if list := dependents[lines[i].ID]; len(list) > 0 {
			sort.Strings(list)
			lines[i].Dependents = list
		}
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].ID < lines[j].ID })
	return lines
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func formatTriageTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func wholeDays(now, then time.Time) int {
	if then.IsZero() || now.Before(then) {
		return 0
	}
	return int(now.Sub(then) / (24 * time.Hour))
}

func clipRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n])
}

// The three shapes in which a description quotes the Governor setting a
// priority: the word prioritize, "to P0", and the line a Postern priority
// action leaves ("PRIORITY 1 set by the Governor via postern"). A plain
// 'priority' is not one.
var hisPriorityShapes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bprioriti[sz](e|es|ed|ing)\b`),
	regexp.MustCompile(`(?i)\bto p[0-4]\b`),
	regexp.MustCompile(`(?i)\bpriority [0-4] set by the governor via postern`),
}

// HisPriority is whether a description quotes the Governor setting a priority.
func HisPriority(description string) bool {
	for _, shape := range hisPriorityShapes {
		if shape.MatchString(description) {
			return true
		}
	}
	return false
}

// ReadTriageLines reads the lines of a dump back.
func ReadTriageLines(r io.Reader) ([]TriageLine, error) {
	var lines []TriageLine
	dec := json.NewDecoder(r)
	for dec.More() {
		var line TriageLine
		if err := dec.Decode(&line); err != nil {
			return nil, fmt.Errorf("reading a triage dump: %w", err)
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// TriageChange is one field of a bead that differs between two dumps.
type TriageChange struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// TriageMove is a bead in both dumps whose priority, status or labels changed.
type TriageMove struct {
	ID      string         `json:"id"`
	Changes []TriageChange `json:"changes"`
}

// TriageDiff is what moved between two dumps. Each list is in id order.
type TriageDiff struct {
	New        []string     `json:"new"`
	Closed     []string     `json:"closed"`
	Moved      []TriageMove `json:"moved"`
	NewlyReady []string     `json:"newly_ready"`
	NewlyStale []string     `json:"newly_stale"`
}

// DiffTriage compares an older dump with a newer one. Closed is a bead the
// newer dump no longer holds: it was closed, or it left the live work.
func DiffTriage(old, fresh []TriageLine) TriageDiff {
	was := make(map[string]TriageLine, len(old))
	for _, l := range old {
		was[l.ID] = l
	}
	now := make(map[string]TriageLine, len(fresh))
	for _, l := range fresh {
		now[l.ID] = l
	}
	diff := TriageDiff{New: []string{}, Closed: []string{}, Moved: []TriageMove{}, NewlyReady: []string{}, NewlyStale: []string{}}
	for id, l := range now {
		before, existed := was[id]
		if !existed {
			diff.New = append(diff.New, id)
			continue
		}
		var changes []TriageChange
		if before.Priority != l.Priority {
			changes = append(changes, TriageChange{"priority", fmt.Sprintf("P%d", before.Priority), fmt.Sprintf("P%d", l.Priority)})
		}
		if before.Status != l.Status {
			changes = append(changes, TriageChange{"status", before.Status, l.Status})
		}
		if from, to := strings.Join(sortedCopy(before.Labels), ","), strings.Join(sortedCopy(l.Labels), ","); from != to {
			changes = append(changes, TriageChange{"labels", from, to})
		}
		if len(changes) > 0 {
			diff.Moved = append(diff.Moved, TriageMove{ID: id, Changes: changes})
		}
		if l.Ready && !before.Ready {
			diff.NewlyReady = append(diff.NewlyReady, id)
		}
		if l.Stale() && !before.Stale() {
			diff.NewlyStale = append(diff.NewlyStale, id)
		}
	}
	for id := range was {
		if _, still := now[id]; !still {
			diff.Closed = append(diff.Closed, id)
		}
	}
	sort.Strings(diff.New)
	sort.Strings(diff.Closed)
	sort.Strings(diff.NewlyReady)
	sort.Strings(diff.NewlyStale)
	sort.Slice(diff.Moved, func(i, j int) bool { return diff.Moved[i].ID < diff.Moved[j].ID })
	return diff
}

// WriteJSON writes the diff as one indented JSON object.
func (d TriageDiff) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

// String is the diff as a person reads it: a heading and a line per bead, and
// nothing for a list that is empty.
func (d TriageDiff) String() string {
	var b strings.Builder
	section := func(heading string, ids []string) {
		if len(ids) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s (%d)\n", heading, len(ids))
		for _, id := range ids {
			b.WriteString("  " + id + "\n")
		}
	}
	section("New", d.New)
	section("Closed", d.Closed)
	if len(d.Moved) > 0 {
		fmt.Fprintf(&b, "Moved (%d)\n", len(d.Moved))
		for _, m := range d.Moved {
			var parts []string
			for _, c := range m.Changes {
				parts = append(parts, fmt.Sprintf("%s %s -> %s", c.Field, c.From, c.To))
			}
			fmt.Fprintf(&b, "  %s: %s\n", m.ID, strings.Join(parts, "; "))
		}
	}
	section("Newly ready", d.NewlyReady)
	section("Newly stale", d.NewlyStale)
	if b.Len() == 0 {
		return "Nothing moved.\n"
	}
	return b.String()
}

// TriageOrphans finds the formula beads of finished stories: a story that is
// closed names the root of the formula poured for it, and that root and its
// steps are of no further use. It lists them, and closes them only with Apply.
type TriageOrphans struct {
	Source TriageSource
	Closer TriageCloser
	Apply  bool
	Out    io.Writer
}

// Run returns the ids of the orphans still open, steps before roots, each in id
// order. With Apply it closes them in that order, each with a comment saying
// why; without, it writes nothing to the tracker.
func (o TriageOrphans) Run(ctx context.Context) ([]string, error) {
	if o.Source == nil {
		return nil, fmt.Errorf("triage orphans: there is no work tracker to read from")
	}
	if o.Apply && o.Closer == nil {
		return nil, fmt.Errorf("triage orphans: there is nothing to close them with")
	}
	beads, err := o.Source.TriageBeads(ctx)
	if err != nil {
		return nil, fmt.Errorf("triage orphans: %w", err)
	}
	finishedStory := map[string]string{} // formula root -> its closed story
	for _, b := range beads {
		if b.Molecule != "" && strings.EqualFold(b.Status, StatusClosed) {
			finishedStory[b.Molecule] = b.ID
		}
	}
	var steps, roots []string
	story := map[string]string{}
	for _, b := range beads {
		if !isFormulaBead(b) || strings.EqualFold(b.Status, StatusClosed) {
			continue
		}
		if owner, ok := finishedStory[b.ID]; ok {
			roots = append(roots, b.ID)
			story[b.ID] = owner
		} else if owner, ok := finishedStory[b.Parent]; ok {
			steps = append(steps, b.ID)
			story[b.ID] = owner
		}
	}
	sort.Strings(steps)
	sort.Strings(roots)
	found := append(steps, roots...)
	for _, id := range found {
		reason := fmt.Sprintf("Orphaned formula bead of closed story %s, closed by mw triage dump --sweep-orphans", story[id])
		if o.Apply {
			if err := o.Closer.CloseStory(ctx, id, reason); err != nil {
				return found, fmt.Errorf("triage orphans: closing %s: %w", id, err)
			}
		}
		if o.Out != nil {
			verb := "would close"
			if o.Apply {
				verb = "closed"
			}
			fmt.Fprintf(o.Out, "%s %s (story %s is closed)\n", verb, id, story[id])
		}
	}
	return found, nil
}
