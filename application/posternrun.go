package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// PosternActionRun runs a hands step the Governor approved: postern's
// docs/protocol.md §13's run row and §17.
const PosternActionRun = "run"

// HandsOutputLimit is how many characters of a step's output its outcome
// carries — the last of them, where a failure says why: §17's 4000.
const HandsOutputLimit = 4000

// runsHands reports whether this inbox can run a hands step at all: with no
// runner or no verifier configured, a run action is left for the Mayor.
func (i PosternInbox) runsHands() bool {
	return i.HandsRunner != nil && i.HandsVerifier != nil
}

// applyRun runs the hands step the Governor approved (§17), once every check
// holds: the step is on the bead and still hashes to what he approved, the
// approval is his key's signature over that hash and time, the bead waits on
// no open bead, the approval is under domain.HandsApprovalMaxAge old (and not
// over two minutes ahead) and not signed before the step was added, it has
// not run before, the step has not already run OK (a stale screen's Approve;
// --replace forgets a run), and the step's host is this one or one
// [hands_hosts] reaches. The approval is
// marked spent before the step starts, so a crash cannot run it twice. How
// it ran — or why it did not — is commented on the bead, sent back to him in
// the bead's thread, re the approval's txid, and mailed to the Mayor.
func (i PosternInbox) applyRun(ctx context.Context, m PosternInboxMessage, action PosternAction) (posternApplied, error) {
	result := posternApplied{Kind: PosternActionRun, Bead: action.Bead, Txid: m.Txid}
	refuse := func(why string) (posternApplied, error) {
		result.Refused, result.Detail = true, why
		text := fmt.Sprintf("NOT RUN step %s (approved by the Governor via postern, txid %s): %s", action.Step, m.Txid, why)
		return result, i.reportRun(ctx, action.Bead, m.Txid, text, fmt.Sprintf("Not run: %s %s", action.Bead, action.Step))
	}

	raw, err := i.Memory.Note(ctx, HandsStepsKey(action.Bead))
	if err != nil {
		return posternApplied{}, err
	}
	steps, err := parseHandsSteps(raw)
	if err != nil {
		return refuse(fmt.Sprintf("%s cannot be read: %v", HandsStepsKey(action.Bead), err))
	}
	var step domain.HandsStep
	var addedAt string
	found := false
	for _, s := range steps {
		if s.ID == action.Step {
			step, addedAt, found = s.HandsStep, s.AddedAt, true
		}
	}
	if !found {
		return refuse(fmt.Sprintf("there is no step %s on %s", action.Step, action.Bead))
	}
	if domain.HandsSHA256(action.Bead, step) != action.SHA256 {
		return refuse("the step changed since you approved it: approve it again as it stands now")
	}
	if err := i.HandsVerifier.VerifyApproval(i.GovernorKey, action.SHA256, action.ApprovedAt, action.Sig); err != nil {
		return refuse(err.Error())
	}
	waits, err := i.handsWaits(ctx, action.Bead)
	if err != nil {
		return posternApplied{}, err
	}
	if len(waits) > 0 {
		done := "that is"
		if len(waits) > 1 {
			done = "they are"
		}
		return refuse(fmt.Sprintf("it waits on %s. Approve it again once %s done.", strings.Join(waits, ", "), done))
	}
	if err := domain.CheckHandsApprovalAge(action.ApprovedAt, i.now()); err != nil {
		return refuse(err.Error())
	}
	added, err := time.Parse(time.RFC3339, addedAt)
	if err != nil {
		return refuse(fmt.Sprintf("when step %s was added cannot be read (%q): the Mayor re-adds it with mw hands add --replace", step.ID, addedAt))
	}
	if action.ApprovedAt < added.Unix() {
		return refuse(fmt.Sprintf("you approved it at %s, before the step was added at %s: approve it again as it stands now",
			time.Unix(action.ApprovedAt, 0).UTC().Format(time.RFC3339), added.UTC().Format(time.RFC3339)))
	}
	approval := HandsApprovalKey(domain.HandsApprovalID(action.SHA256, action.ApprovedAt))
	spent, err := i.Memory.Note(ctx, approval)
	if err != nil {
		return posternApplied{}, err
	}
	if strings.TrimSpace(spent) != "" {
		return refuse(fmt.Sprintf("this approval has already run (txid %s): approve the step again to run it again", strings.TrimSpace(spent)))
	}
	rawRan, err := i.Memory.Note(ctx, HandsRanKey(action.Bead, step.ID))
	if err != nil {
		return posternApplied{}, err
	}
	if ran, ok := parseHandsRan(rawRan); ok && ran.Exit == 0 {
		return refuse(fmt.Sprintf("the step already ran OK at %s on %s: the Mayor re-adds it with mw hands add --replace if it must run again", ran.At, ran.Host))
	}
	var remote []string
	if step.Host != i.Host {
		remote = strings.Fields(i.HandsHosts[step.Host])
		if len(remote) == 0 {
			return refuse(fmt.Sprintf("there is no [hands_hosts] entry for %s in this host's config: add %s = \"ssh <how to reach it>\" to its [hands_hosts] table", step.Host, step.Host))
		}
	}

	if err := i.Memory.SetNote(ctx, approval, m.Txid); err != nil {
		return posternApplied{}, fmt.Errorf("marking the approval of %s on %s spent: %w", step.ID, action.Bead, err)
	}
	outcome, err := i.HandsRunner.Run(ctx, HandsJob{
		Request: domain.HandsRequest{
			Bead: action.Bead, ID: step.ID, Host: step.Host, As: step.As, Run: step.Run, WayBack: step.WayBack,
			SHA256: action.SHA256, ApprovedAt: action.ApprovedAt, Sig: action.Sig,
		},
		Remote: remote,
	})
	if err != nil {
		outcome = HandsOutcome{Exit: -1, Output: fmt.Sprintf("the step could not be started: %v", err)}
	}
	ran, err := json.Marshal(HandsRan{At: i.now().UTC().Format(time.RFC3339), Exit: outcome.Exit, Host: step.Host, Why: handsWhy(outcome.Exit, outcome.Output)})
	if err != nil {
		return posternApplied{}, err
	}
	if err := i.Memory.SetNote(ctx, HandsRanKey(action.Bead, step.ID), string(ran)); err != nil {
		return posternApplied{}, fmt.Errorf("recording how %s on %s ran: %w", step.ID, action.Bead, err)
	}
	result.Detail = fmt.Sprintf("exit %d", outcome.Exit)
	text := fmt.Sprintf("RAN step %s on %s as %s, exit %d (approved by the Governor via postern, txid %s)\n\n%s",
		step.ID, step.Host, step.As, outcome.Exit, m.Txid, handsOutputBlock(outcome.Output))
	return result, i.reportRun(ctx, action.Bead, m.Txid, text, fmt.Sprintf("Ran: %s %s, exit %d", action.Bead, step.ID, outcome.Exit))
}

// handsWaits names the open beads bead waits on, each as "<title> (<id>)":
// a step on a bead that waits is not run (§17), just as the view offers it
// no Approve. A blocker the tracker has no record of is not a wait, as in
// the view.
func (i PosternInbox) handsWaits(ctx context.Context, bead string) ([]string, error) {
	details, err := i.Tracker.ShowBeads(ctx, []string{bead})
	if err != nil {
		return nil, fmt.Errorf("reading what %s waits on: %w", bead, err)
	}
	if len(details) == 0 || len(details[0].Needs) == 0 {
		return nil, nil
	}
	blockers, err := i.Tracker.ShowBeads(ctx, details[0].Needs)
	if err != nil {
		return nil, fmt.Errorf("reading what %s waits on: %w", bead, err)
	}
	var waits []string
	for _, b := range blockers {
		if b.Closed() {
			continue
		}
		title := strings.TrimSpace(b.Story.Title)
		if title == "" {
			title = b.Story.ID
		}
		waits = append(waits, fmt.Sprintf("%s (%s)", title, b.Story.ID))
	}
	return waits, nil
}

// handsOutputBlock is a step's output as its outcome carries it: the last
// HandsOutputLimit characters, fenced.
func handsOutputBlock(output string) string {
	runes := []rune(strings.TrimRight(output, "\n"))
	if len(runes) == 0 {
		return "(no output)"
	}
	if len(runes) > HandsOutputLimit {
		runes = runes[len(runes)-HandsOutputLimit:]
	}
	return fencedAs("", string(runes))
}

// reportRun says text — how a step ran, or why it did not — by all three
// channels §17 names: a comment on the bead, a message back to the Governor
// in the bead's thread re the approval's txid, and a mail to the Mayor,
// which also says whichever of the other two could not be made.
func (i PosternInbox) reportRun(ctx context.Context, bead, txid, text, subject string) error {
	var problems []string
	if err := i.Tracker.CommentOnStory(ctx, bead, text); err != nil {
		problems = append(problems, fmt.Sprintf("it could not be written on %s: %v", bead, err))
	}
	if i.Sender == nil {
		problems = append(problems, "no sender is configured to tell the Governor")
	} else if _, err := i.Sender.Run(ctx, PosternSendRequest{Class: "message", Text: text, Thread: bead, Re: txid, Recorded: true}); err != nil {
		problems = append(problems, fmt.Sprintf("it could not be sent back to the Governor: %v", err))
	}
	body := text
	for _, problem := range problems {
		body += "\n\nBut " + problem + "."
	}
	return i.mail(ctx, subject, body)
}

// now is the clock an approval's age and a run's time are read by.
func (i PosternInbox) now() time.Time {
	if i.Now == nil {
		return time.Now()
	}
	return i.Now()
}
