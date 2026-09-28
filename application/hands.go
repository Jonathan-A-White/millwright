package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// HandsStepsKey is the note a bead's hands steps live in: postern's
// docs/protocol.md §17, a JSON array of HandsStepRecord.
func HandsStepsKey(bead string) string { return "hands." + bead }

// HandsRanKey is the note a step's last run is recorded under, HandsRan's
// JSON.
func HandsRanKey(bead, id string) string { return "hands.ran." + bead + "." + id }

// HandsApprovalKey is the note an approval is marked run under once mw has
// started its step, holding the txid that carried it: an approval runs once.
func HandsApprovalKey(approvalID string) string { return "hands.approval." + approvalID }

// HandsStepRecord is one step as a bead's hands note keeps it: the step, and
// when the Mayor added it.
type HandsStepRecord struct {
	domain.HandsStep
	AddedAt string `json:"added_at"`
}

// HandsRan is a step's last run: when, with what exit status, on which host.
type HandsRan struct {
	At   string `json:"at"`
	Exit int    `json:"exit"`
	Host string `json:"host"`
}

// parseHandsSteps reads a bead's hands note. An empty note is no steps; one
// that does not read as steps is an error, so nothing is ever written over
// it on a guess.
func parseHandsSteps(raw string) ([]HandsStepRecord, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var steps []HandsStepRecord
	if err := json.Unmarshal([]byte(raw), &steps); err != nil {
		return nil, fmt.Errorf("the hands note does not read as steps: %w", err)
	}
	return steps, nil
}

// parseHandsRan reads a step's ran note, reporting false for none.
func parseHandsRan(raw string) (HandsRan, bool) {
	var ran HandsRan
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &ran) != nil {
		return HandsRan{}, false
	}
	return ran, true
}

// HandsAddRequest is a step the Mayor adds to a bead, for the Governor to
// approve and the factory to run.
type HandsAddRequest struct {
	Bead string
	Step domain.HandsStep
	// Replace lets the step take the place of one already on the bead with
	// its id: its hash changes, so any approval of the old one no longer
	// matches it.
	Replace bool
}

// HandsAdd writes down a step only the Governor's hands could take (postern's
// docs/protocol.md §17) — what the Mayor used to write as a `!` line for him
// to type — on a bead: kept in the bead's hands note, commented on the bead
// exactly as it will run, and the bead labelled hitl so the view shows it as
// his. It runs nothing: the step runs only once he approves it.
type HandsAdd struct {
	Tracker WorkTracker
	Notes   PosternNotes
	// Now stamps the step. The zero value reads the real clock.
	Now func() time.Time
	// Out is where the step's hash is printed. A nil Out prints nothing.
	Out io.Writer
}

// Run adds req's step, refusing an invalid one, a bead the tracker does not
// hold, and — unless req.Replace — an id the bead already has a step of.
func (h HandsAdd) Run(ctx context.Context, req HandsAddRequest) (HandsStepRecord, error) {
	if h.Tracker == nil || h.Notes == nil {
		return HandsStepRecord{}, fmt.Errorf("mw hands add: no work tracker is configured")
	}
	if err := domain.ValidateHandsStep(req.Bead, req.Step); err != nil {
		return HandsStepRecord{}, fmt.Errorf("mw hands add: %w", err)
	}
	found, err := h.Tracker.ShowBeads(ctx, []string{req.Bead})
	if err != nil {
		return HandsStepRecord{}, fmt.Errorf("mw hands add: reading %s: %w", req.Bead, err)
	}
	if len(found) == 0 {
		return HandsStepRecord{}, fmt.Errorf("mw hands add: there is no bead %s", req.Bead)
	}
	raw, err := h.Notes.Note(ctx, HandsStepsKey(req.Bead))
	if err != nil {
		return HandsStepRecord{}, err
	}
	steps, err := parseHandsSteps(raw)
	if err != nil {
		return HandsStepRecord{}, fmt.Errorf("mw hands add: %s: %w", HandsStepsKey(req.Bead), err)
	}

	record := HandsStepRecord{HandsStep: req.Step, AddedAt: h.now().UTC().Format(time.RFC3339)}
	replaced := false
	for i, step := range steps {
		if step.ID != req.Step.ID {
			continue
		}
		if !req.Replace {
			return HandsStepRecord{}, fmt.Errorf("mw hands add: %s already has a step %s: --replace to change it, which voids any approval of the old one", req.Bead, req.Step.ID)
		}
		steps[i] = record
		replaced = true
	}
	if !replaced {
		steps = append(steps, record)
	}
	encoded, err := json.Marshal(steps)
	if err != nil {
		return HandsStepRecord{}, err
	}
	if err := h.Notes.SetNote(ctx, HandsStepsKey(req.Bead), string(encoded)); err != nil {
		return HandsStepRecord{}, fmt.Errorf("mw hands add: keeping the step: %w", err)
	}
	if replaced {
		if err := h.Notes.ClearNote(ctx, HandsRanKey(req.Bead, req.Step.ID)); err != nil {
			return HandsStepRecord{}, fmt.Errorf("mw hands add: forgetting the old step's run: %w", err)
		}
	}
	if err := h.Tracker.CommentOnStory(ctx, req.Bead, handsStepComment(req.Step, replaced)); err != nil {
		return HandsStepRecord{}, fmt.Errorf("mw hands add: commenting the step on %s: %w", req.Bead, err)
	}
	if !found[0].Hitl() {
		if err := h.Tracker.AddLabel(ctx, req.Bead, LabelHitl); err != nil {
			return HandsStepRecord{}, fmt.Errorf("mw hands add: labelling %s %s: %w", req.Bead, LabelHitl, err)
		}
	}
	if h.Out != nil {
		fmt.Fprintf(h.Out, "added step %s to %s, on %s as %s: sha256 %s\n", req.Step.ID, req.Bead, req.Step.Host, req.Step.As, domain.HandsSHA256(req.Bead, req.Step))
	}
	return record, nil
}

// handsStepComment is what a step is recorded on its bead as: who runs it
// where, then the text and the way back, each fenced exactly as it runs.
func handsStepComment(step domain.HandsStep, replacing bool) string {
	head := fmt.Sprintf("HANDS STEP %s on %s as %s", step.ID, step.Host, step.As)
	if replacing {
		head += " (replacing the step of that id)"
	}
	comment := head + ":\n" + fencedAs("sh", step.Run) + "\nway back:"
	if strings.TrimSpace(step.WayBack) == "" {
		return comment + " none"
	}
	return comment + "\n" + fencedAs("sh", step.WayBack)
}

// fencedAs is fenced (next.go), its opening fence naming lang.
func fencedAs(lang, text string) string {
	block := fenced(strings.TrimRight(text, "\n"))
	open := strings.Index(block, "\n")
	return block[:open] + lang + block[open:]
}

func (h HandsAdd) now() time.Time {
	if h.Now == nil {
		return time.Now()
	}
	return h.Now()
}

// HandsList prints every hands step of a bead: where and as whom it runs,
// its §17 sha256, whether it has run, its text and its way back. It reads
// two notes' worth and writes nothing.
type HandsList struct {
	Notes PosternNotes
	Out   io.Writer
}

// Run lists bead's steps.
func (l HandsList) Run(ctx context.Context, bead string) ([]HandsStepRecord, error) {
	if l.Notes == nil {
		return nil, fmt.Errorf("mw hands list: no work tracker is configured")
	}
	raw, err := l.Notes.Note(ctx, HandsStepsKey(bead))
	if err != nil {
		return nil, err
	}
	steps, err := parseHandsSteps(raw)
	if err != nil {
		return nil, fmt.Errorf("mw hands list: %s: %w", HandsStepsKey(bead), err)
	}
	ran, err := l.Notes.NotesWithPrefix(ctx, "hands.ran."+bead+".")
	if err != nil {
		return nil, err
	}
	if l.Out == nil {
		return steps, nil
	}
	if len(steps) == 0 {
		fmt.Fprintf(l.Out, "%s has no hands steps\n", bead)
	}
	for _, step := range steps {
		state := "not run"
		if r, ok := parseHandsRan(ran[HandsRanKey(bead, step.ID)]); ok {
			state = fmt.Sprintf("ran %s on %s, exit %d", r.At, r.Host, r.Exit)
		}
		fmt.Fprintf(l.Out, "%s on %s as %s  sha256 %s  %s\n", step.ID, step.Host, step.As, domain.HandsSHA256(bead, step.HandsStep), state)
		for _, line := range strings.Split(strings.TrimRight(step.Run, "\n"), "\n") {
			fmt.Fprintf(l.Out, "    %s\n", line)
		}
		if strings.TrimSpace(step.WayBack) != "" {
			fmt.Fprintf(l.Out, "    way back: %s\n", strings.Join(strings.Fields(step.WayBack), " "))
		}
	}
	return steps, nil
}
