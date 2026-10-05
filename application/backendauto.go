package application

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// stagedSwap is what a staged swap is kept as until the home's tick has run it:
// the bead and the hands step that hold it.
type stagedSwap struct {
	Rig    string `json:"rig"`
	Story  string `json:"story"`
	Bead   string `json:"bead"`
	Step   string `json:"step"`
	Commit string `json:"commit"`
}

func swapKey(rig, commit string) string { return BackendSwapPrefix + rig + "." + commit }

func (b BackendStage) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// swapStaged is the home's tick running the swaps it staged (mw-gq6.270): each
// staged swap of a rig whose swap is automatic is run by the same step the
// Governor's tap would run, with no tap, once no Talk is open — a restart in the
// middle of a Talk would drop his line, so a swap waits for the first tick after
// it — and never while another swap or a postern inbox pass holds the inbox lock.
// A swap is begun at most once: the step's ran note is written before it runs and
// nothing here runs a step that has one, so a failed swap stays failed for a
// person. A tick that cannot read the Talk, or finds the lock held, leaves the
// swap for the next.
func (b BackendStage) swapStaged(ctx context.Context) []string {
	if b.Runner == nil || b.Lock == nil {
		return nil
	}
	kept, err := b.Notes.NotesWithPrefix(ctx, BackendSwapPrefix)
	if err != nil {
		return []string{"backend: the staged swaps could not be read: " + firstLine(err.Error())}
	}
	keys := make([]string, 0, len(kept))
	for key := range kept {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var notes []string
	for _, key := range keys {
		var swap stagedSwap
		if err := json.Unmarshal([]byte(kept[key]), &swap); err != nil {
			continue
		}
		cfg, ok := b.Settings[swap.Rig]
		if !ok || !cfg.Automatic() {
			// The rig has kept the tap since: the step stays a hands step.
			b.Notes.ClearNote(ctx, key)
			continue
		}
		notes = append(notes, b.swapOne(ctx, key, swap)...)
	}
	return notes
}

// swapOne runs one staged swap if it is time to, and says what became of it.
func (b BackendStage) swapOne(ctx context.Context, key string, swap stagedSwap) []string {
	short := updatedRevision(swap.Commit)
	words := fmt.Sprintf("backend: the swap of %s to %s", swap.Rig, short)

	open, err := talkOpen(ctx, b.Notes, b.now())
	if err != nil {
		return []string{words + " waits: the Talk could not be read: " + firstLine(err.Error())}
	}
	if open {
		return nil
	}
	release, taken, err := b.Lock.TryTake(ctx)
	if err != nil {
		return []string{words + " waits: the inbox lock could not be taken: " + firstLine(err.Error())}
	}
	if !taken {
		return nil
	}
	defer release()

	// Every look at the step is made under the lock, so that two ticks that raced
	// to it do not both find it unrun.
	step, why, err := b.swapStep(ctx, swap)
	if err != nil {
		return []string{words + " waits: " + firstLine(err.Error())}
	}
	if why != "" {
		if err := b.Notes.ClearNote(ctx, key); err != nil {
			return []string{words + " was not run (" + why + ") and its note could not be cleared: " + firstLine(err.Error())}
		}
		return []string{words + " was not run: " + why}
	}

	started, err := json.Marshal(HandsRan{At: b.now().UTC().Format(time.RFC3339), Exit: -1, Host: step.Host, Why: HandsStartedWhy})
	if err != nil {
		return []string{words + " waits: " + err.Error()}
	}
	if err := b.Notes.SetNote(ctx, HandsRanKey(swap.Bead, step.ID), string(started)); err != nil {
		return []string{words + " waits: recording that it started: " + firstLine(err.Error())}
	}
	// It is begun now, so the note that asked for it goes: whatever comes of the run, no tick begins it again.
	if err := b.Notes.ClearNote(ctx, key); err != nil {
		return []string{words + " was not run: its note could not be cleared: " + firstLine(err.Error())}
	}

	outcome, err := b.Runner.Run(ctx, HandsJob{Request: domain.HandsRequest{
		Bead: swap.Bead, ID: step.ID, Host: step.Host, As: step.As, Run: step.Run, WayBack: step.WayBack,
	}})
	if err != nil {
		outcome = HandsOutcome{Exit: -1, Output: fmt.Sprintf("the step could not be started: %v", err)}
	}
	return b.swapResult(ctx, swap, step, outcome)
}

// swapStep is the step of swap as the bead holds it, or why it is not to be run:
// it is gone, superseded by a newer swap, or has been begun before.
func (b BackendStage) swapStep(ctx context.Context, swap stagedSwap) (domain.HandsStep, string, error) {
	raw, err := b.Notes.Note(ctx, HandsStepsKey(swap.Bead))
	if err != nil {
		return domain.HandsStep{}, "", err
	}
	steps, err := parseHandsSteps(raw)
	if err != nil {
		return domain.HandsStep{}, fmt.Sprintf("%s cannot be read: %v", HandsStepsKey(swap.Bead), err), nil
	}
	var step domain.HandsStep
	found := false
	for _, s := range steps {
		if s.ID == swap.Step {
			step, found = s.HandsStep, true
		}
	}
	if !found {
		return domain.HandsStep{}, fmt.Sprintf("there is no step %s on %s", swap.Step, swap.Bead), nil
	}
	by, err := b.Notes.Note(ctx, HandsSupersededKey(swap.Bead, step.ID))
	if err != nil {
		return domain.HandsStep{}, "", err
	}
	if by = strings.TrimSpace(by); by != "" {
		return domain.HandsStep{}, "it is superseded by " + by + ", a newer swap", nil
	}
	ran, err := b.Notes.Note(ctx, HandsRanKey(swap.Bead, step.ID))
	if err != nil {
		return domain.HandsStep{}, "", err
	}
	if strings.TrimSpace(ran) != "" {
		return domain.HandsStep{}, "it was begun before", nil
	}
	return step, "", nil
}

// swapResult records how the step ran, comments its output on the bead, says the
// result once on the bead's channel, and closes the bead if the swap went right.
// A swap that failed leaves its bead open and hitl, as filed, for a person.
func (b BackendStage) swapResult(ctx context.Context, swap stagedSwap, step domain.HandsStep, outcome HandsOutcome) []string {
	short := updatedRevision(swap.Commit)
	var lines []string
	problem := func(what string, err error) {
		lines = append(lines, fmt.Sprintf("backend: %s of the swap of %s to %s: %s", what, swap.Rig, short, firstLine(err.Error())))
	}

	ran, err := json.Marshal(HandsRan{At: b.now().UTC().Format(time.RFC3339), Exit: outcome.Exit, Host: step.Host, Why: handsWhy(outcome.Exit, outcome.Output)})
	if err == nil {
		err = b.Notes.SetNote(ctx, HandsRanKey(swap.Bead, step.ID), string(ran))
	}
	if err != nil {
		problem("recording how it ran", err)
	}

	said := swapSaid(short, outcome)
	comment := fmt.Sprintf("RAN step %s on %s as %s by mw itself, with no tap, exit %d\n\n%s", step.ID, step.Host, step.As, outcome.Exit, handsOutputBlock(outcome.Output))
	if err := b.Tracker.CommentOnStory(ctx, swap.Bead, comment); err != nil {
		problem("writing the output on "+swap.Bead, err)
	}
	if b.Say != nil {
		if _, err := b.Say.Run(ctx, PosternSendRequest{Class: "message", Text: said, Thread: swap.Bead, Recorded: true}); err != nil {
			problem("telling the Governor", err)
		}
	}
	if outcome.Exit == 0 {
		if err := b.Tracker.CloseStory(ctx, swap.Bead, said); err != nil {
			problem("closing "+swap.Bead, err)
		}
	}
	return append([]string{"backend: " + said}, lines...)
}

// swapSaid is the result of a swap as the Governor is told it, from what the step
// printed: a swap that went through says the step's own last word, "backend <short>
// is live and answering"; one that failed says so, and that the old backend was put
// back only when the step says it did that.
func swapSaid(short string, outcome HandsOutcome) string {
	last := handsLastLine(outcome.Output)
	if outcome.Exit == 0 {
		if strings.Contains(outcome.Output, "backend "+short+" is live and answering") {
			return "backend " + short + " is live and answering"
		}
		if last == "" {
			return "backend " + short + ": the swap ran and printed nothing"
		}
		return last
	}
	if strings.Contains(outcome.Output, "putting the old backend back") {
		return fmt.Sprintf("backend %s did not answer: the swap failed and the old backend was put back (%s)", short, last)
	}
	return fmt.Sprintf("backend %s: the swap failed (exit %d): %s", short, outcome.Exit, last)
}

// handsLastLine is the last line of output that has any words, clipped as a failed
// step's why is.
func handsLastLine(output string) string {
	return handsWhy(1, output)
}
