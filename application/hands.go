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

// HandsSupersededKey is the note that marks a step as superseded, holding the
// bead whose newer step took its place: a superseded step cannot be approved to
// run (mw-gq6.190).
func HandsSupersededKey(bead, id string) string { return "hands.superseded." + bead + "." + id }

// HandsApprovalKey is the note an approval is marked run under once mw has
// started its step, holding the txid that carried it: an approval runs once.
func HandsApprovalKey(approvalID string) string { return "hands.approval." + approvalID }

// HandsStepRecord is one step as a bead's hands note keeps it: the step, and
// when the Mayor added it.
type HandsStepRecord struct {
	domain.HandsStep
	AddedAt string `json:"added_at"`
}

// HandsWhyLimit is the most characters of a failed step's why.
const HandsWhyLimit = 200

// HandsRan is a step's last run: when, with what exit status, on which host,
// and — when the exit was not 0 — why: the last non-empty line the step
// printed, or the error that kept it from starting.
type HandsRan struct {
	At   string `json:"at"`
	Exit int    `json:"exit"`
	Host string `json:"host"`
	Why  string `json:"why,omitempty"`
}

// HandsStartedWhy is the why of a ran record written when a step starts, which
// the finished record replaces: one left standing says the pass that ran the
// step never reported how it ended.
const HandsStartedWhy = "started; the pass that ran it has not reported back"

// handsWhy is why a step that exited with exit failed, out of its output:
// the last non-empty line, clipped to HandsWhyLimit characters. A step that
// exited 0, or printed nothing, has none.
func handsWhy(exit int, output string) string {
	if exit == 0 {
		return ""
	}
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if runes := []rune(line); len(runes) > HandsWhyLimit {
			line = string(runes[:HandsWhyLimit])
		}
		return line
	}
	return ""
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
	// After are beads that must finish before the step is worth his hands:
	// each is made to block Bead (AddBlocker) before the step is kept, so the
	// view marks the step waiting and the push says what it waits on.
	After []string
}

// PosternSender sends one message to the Governor: PosternSend, narrowed to
// the one call a use case that pushes needs.
type PosternSender interface {
	Run(ctx context.Context, req PosternSendRequest) (string, error)
}

// HandsPushClass is the class of the push a new hands step sends. Postern's
// src/push/classOptions.ts CLASS_URLS opens decision-needed, landing and
// alarm at /?v=needs — Needs you — and message at /?v=talk. The step waits on
// his decision, so decision-needed; alarm would sound and stay until
// dismissed.
const HandsPushClass = "decision-needed"

// PosternViewPublisher builds, seals and writes the live view: PosternView,
// narrowed to the one call a use case that refreshes it needs.
type PosternViewPublisher interface {
	Run(ctx context.Context) (PosternViewDoc, error)
}

// ViewLock is the lock the mail notifier holds for a whole tick, taken
// without waiting. hostlock.Try is the real adapter.
type ViewLock interface {
	TryTake(ctx context.Context) (release func(), taken bool, err error)
}

// HandsAdd writes down a step only the Governor's hands could take (postern's
// docs/protocol.md §17) — what the Mayor used to write as a `!` line for him
// to type — on a bead: kept in the bead's hands note, commented on the bead
// exactly as it will run, and the bead labelled hitl so the view shows it as
// his. It runs nothing: the step runs only once he approves it. Once it is
// kept the live view is published, so his phone shows the step, or the new
// hash of a replaced one, without waiting for the notifier's next tick.
type HandsAdd struct {
	Tracker WorkTracker
	Notes   PosternNotes
	// Now stamps the step. The zero value reads the real clock.
	Now func() time.Time
	// Out is where the step's hash is printed. A nil Out prints nothing.
	Out io.Writer

	// Push sends the Governor a message that a step is waiting, on the bead's
	// thread, once the step is written. A nil Push, or NoPush, sends none. A
	// push that fails is said on Err and on the bead, and undoes nothing.
	Push   PosternSender
	NoPush bool

	// View publishes the live view once the step is kept, under ViewLock so
	// that it never runs beside the notifier's own. A nil View, or NoView,
	// publishes none; a nil ViewLock takes no lock. A view that is busy or
	// fails is said on Err, and undoes nothing.
	View     PosternViewPublisher
	ViewLock ViewLock
	NoView   bool
	// Err is where a failed push or view is reported. A nil Err reports a push on the
	// bead only.
	Err io.Writer
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
	found, err := h.Tracker.ShowBeads(ctx, append([]string{req.Bead}, req.After...))
	if err != nil {
		return HandsStepRecord{}, fmt.Errorf("mw hands add: reading %s: %w", req.Bead, err)
	}
	if len(found) == 0 || found[0].Story.ID != req.Bead {
		return HandsStepRecord{}, fmt.Errorf("mw hands add: there is no bead %s", req.Bead)
	}
	waits, err := handsBlockers(req, found[1:])
	if err != nil {
		return HandsStepRecord{}, err
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
	approved := false
	for i, step := range steps {
		if step.ID != req.Step.ID {
			continue
		}
		if !req.Replace {
			return HandsStepRecord{}, fmt.Errorf("mw hands add: %s already has a step %s: --replace to change it, which voids any approval of the old one", req.Bead, req.Step.ID)
		}
		ran, err := h.Notes.Note(ctx, HandsRanKey(req.Bead, req.Step.ID))
		if err != nil {
			return HandsStepRecord{}, err
		}
		approved = strings.TrimSpace(ran) != ""
		steps[i] = record
		replaced = true
	}
	if !replaced {
		steps = append(steps, record)
	}
	for _, blocker := range req.After {
		if err := h.Tracker.AddBlocker(ctx, req.Bead, blocker); err != nil {
			return HandsStepRecord{}, fmt.Errorf("mw hands add: making %s block %s: %w", blocker, req.Bead, err)
		}
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
		if err := h.Notes.ClearNote(ctx, HandsSupersededKey(req.Bead, req.Step.ID)); err != nil {
			return HandsStepRecord{}, fmt.Errorf("mw hands add: forgetting that the old step was superseded: %w", err)
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
	h.publish(ctx)
	if !replaced || approved {
		h.push(ctx, req.Bead, found[0].Story.Title, waits, replaced)
	}
	return record, nil
}

// handsBlockers checks the beads req.After names against what the tracker
// found of them — every one there, none the bead itself — and reports the
// titles of those still open, what the push says the step waits on (an id
// stands for a title that is empty).
func handsBlockers(req HandsAddRequest, found []StoryDetail) ([]string, error) {
	byID := map[string]StoryDetail{}
	for _, d := range found {
		byID[d.Story.ID] = d
	}
	var waits []string
	for _, id := range req.After {
		d, ok := byID[id]
		switch {
		case id == req.Bead:
			return nil, fmt.Errorf("mw hands add: %s cannot wait on itself", id)
		case !ok:
			return nil, fmt.Errorf("mw hands add: --after %s: there is no such bead", id)
		case d.Closed():
			continue
		}
		title := strings.TrimSpace(d.Story.Title)
		if title == "" {
			title = id
		}
		waits = append(waits, title)
	}
	return waits, nil
}

// publish refreshes the live view once a step is kept: the very view mw
// postern view writes, under the notifier's lock so that the two never write
// it together. A busy lock skips it — the notifier's own tick is publishing
// it — and a failure is said; the step is kept either way, so neither is
// returned.
func (h HandsAdd) publish(ctx context.Context) {
	if h.View == nil || h.NoView {
		return
	}
	if h.ViewLock != nil {
		release, taken, err := h.ViewLock.TryTake(ctx)
		if err != nil {
			h.report("the view was not published: taking the notifier's lock: %v (the step is kept)", err)
			return
		}
		if !taken {
			h.report("the view was skipped: the notifier is publishing it, and its next tick shows the step")
			return
		}
		defer release()
	}
	if _, err := h.View.Run(ctx); err != nil {
		h.report("the view was not published: %v (the step is kept; mw postern view publishes it)", err)
	}
}

// push tells the Governor a step is waiting: one message of HandsPushClass on
// bead's thread. The step is already kept, so a failure is only reported, on
// Err and on the bead, never returned. The message is Recorded: the step's
// own comment is already on the bead. Its summary, "Step ready: <bead> on
// <title>", reads as a question's does; title is the bead's, empty when it has none.
// changed is a replaced step he had approved: the push says his approval no
// longer counts. A replaced step he had not approved is not pushed at all (Run):
// the first push already told him, and the step's comment and the view carry
// the change. The step's ran note is the record of an approval, for the approval
// itself is kept only once its step starts.
func (h HandsAdd) push(ctx context.Context, bead, title string, waits []string, changed bool) {
	if h.Push == nil || h.NoPush {
		return
	}
	text := fmt.Sprintf("New hands step on %s: %s", bead, title)
	if changed {
		text = fmt.Sprintf("Changed hands step on %s: %s (your approval of the old step no longer counts; approve again)", bead, title)
	}
	if len(waits) > 0 {
		text += " (waits on " + strings.Join(waits, ", ") + ")"
	}
	summary := "Step ready: " + bead
	if title = titleOrID(title, ""); title != "" {
		summary += " on " + title
	}
	_, err := h.Push.Run(ctx, PosternSendRequest{
		Class:    HandsPushClass,
		Text:     text,
		Summary:  summary,
		Thread:   bead,
		Recorded: true,
	})
	if err == nil {
		return
	}
	h.report("the push to the Governor failed: %v (the step is kept)", err)
	comment := fmt.Sprintf("PUSH FAILED: the Governor was not told of the new hands step: %v", err)
	if err := h.Tracker.CommentOnStory(ctx, bead, comment); err != nil {
		h.report("commenting the failed push on %s failed: %v", bead, err)
	}
}

func (h HandsAdd) report(format string, args ...any) {
	if h.Err != nil {
		fmt.Fprintf(h.Err, "mw hands add: "+format+"\n", args...)
	}
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
	superseded, err := l.Notes.NotesWithPrefix(ctx, "hands.superseded."+bead+".")
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
			if r.Why == HandsStartedWhy {
				state = fmt.Sprintf("started %s on %s, %s", r.At, r.Host, strings.TrimPrefix(r.Why, "started; "))
			}
		} else if by := strings.TrimSpace(superseded[HandsSupersededKey(bead, step.ID)]); by != "" {
			state = "superseded by " + by + " (cannot be approved)"
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

// HandsVerifier checks the Governor's approval of a step: that sigDERHex is
// governorKeyHex's signature over domain.HandsApprovalDigest(sha256Hex,
// approvedAt). The real adapter is infrastructure/hands's, the same check
// mw-hands-root makes (infrastructure/handsroot).
type HandsVerifier interface {
	VerifyApproval(governorKeyHex, sha256Hex string, approvedAt int64, sigDERHex string) error
}

// HandsJob is one approved step to run: the request mw-hands-root is handed
// for a root step — the step, its bead and the Governor's approval — and how
// its host is reached.
type HandsJob struct {
	Request domain.HandsRequest
	// Remote is the ssh prefix, split on whitespace, that reaches the step's
	// host — config [hands_hosts]; empty runs the step on this host.
	Remote []string
}

// HandsOutcome is how a step ran: its exit status and what it printed, its
// standard output and error together.
type HandsOutcome struct {
	Exit   int
	Output string
}

// HandsRunner runs an approved hands step where it belongs: as the host's own
// user, sh -c, or as root, by handing the request to mw-hands-root through
// sudo -n, which checks the approval again itself. A step that runs and
// fails is an outcome, not an error; an error is a step that could not be
// started at all. The real adapter is infrastructure/hands's Runner.
type HandsRunner interface {
	Run(ctx context.Context, job HandsJob) (HandsOutcome, error)
}
