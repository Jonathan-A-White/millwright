package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ClosingOutField is the metadata a close-out (mw next) leaves on its story in
// the tracker while it runs (mw-gq6.1498): who runs it, since when, and until
// when the mark holds if nobody renews it. CloseOutMarks says the same on the
// close-out's own host only; this one is read by every host's dispatch, so that
// a story given back open and landed by hand (mw next --no-dispatch
// --met-by-mayor, --rescoped-by-mayor, contrib/mw-closeout) is not taken again
// as a fresh attempt while the close-out waits for the gate lock.
const ClosingOutField = "closing_out"

// ClosingOutLease is how long a mark holds without being renewed: the close-out
// renews it every HeartbeatInterval, so a close-out that died frees the story
// within this.
const ClosingOutLease = 5 * time.Minute

// ClosingOut is the mark a running close-out leaves on its story.
type ClosingOut struct {
	By    string
	Since time.Time
	Until time.Time
}

// String is the mark as ClosingOutField keeps it: "<host> <since> <until>".
func (c ClosingOut) String() string {
	return fmt.Sprintf("%s %s %s", c.By, c.Since.UTC().Format(time.RFC3339), c.Until.UTC().Format(time.RFC3339))
}

// ParseClosingOut reads what ClosingOutField holds. Nothing, or words that are
// not a host and two times, is no mark.
func ParseClosingOut(text string) (ClosingOut, bool) {
	words := strings.Fields(text)
	if len(words) != 3 {
		return ClosingOut{}, false
	}
	since, err := time.Parse(time.RFC3339, words[1])
	if err != nil {
		return ClosingOut{}, false
	}
	until, err := time.Parse(time.RFC3339, words[2])
	if err != nil {
		return ClosingOut{}, false
	}
	return ClosingOut{By: words[0], Since: since, Until: until}, true
}

// Live says the mark holds at now: there is one and its lease has not lapsed.
func (c ClosingOut) Live(now time.Time) bool {
	return !c.Until.IsZero() && now.Before(c.Until)
}

// closeOutOfKey is the context key of the story a close-out runs for.
type closeOutOfKey struct{}

// WithCloseOutOf is ctx marked as that of the close-out of story: the dispatch
// the close-out itself runs afterwards is not held by the close-out's own mark.
func WithCloseOutOf(ctx context.Context, story string) context.Context {
	return context.WithValue(ctx, closeOutOfKey{}, story)
}

// closingOutNow is the mark that holds detail at now, if there is one that is
// not the dispatch's own close-out's.
func (d Dispatch) closingOutNow(ctx context.Context, detail StoryDetail) (ClosingOut, bool) {
	if story, _ := ctx.Value(closeOutOfKey{}).(string); story == detail.Story.ID {
		return ClosingOut{}, false
	}
	return detail.ClosingOut, detail.ClosingOut.Live(d.now())
}

// closingOutWhy is the words of the closing-out gate for detail, "" when no
// close-out holds it.
func (d Dispatch) closingOutWhy(ctx context.Context, detail StoryDetail) string {
	mark, ok := d.closingOutNow(ctx, detail)
	if !ok {
		return ""
	}
	return fmt.Sprintf("it is being landed by the Mayor: a close-out on %s has run %dm and its lease holds until %s, so it is not claimed and no attempt is spent",
		mark.By, int(d.now().Sub(mark.Since)/time.Minute), mark.Until.UTC().Format("15:04Z"))
}

// claimLost is what start returns when another holder's claim got there first,
// so that run passes the story over instead of failing the tick.
type claimLost struct {
	holder     string
	closingOut string // the closing-out gate's words, when a close-out's mark was seen at the claim (claimStory)
}

func (e *claimLost) Error() string {
	if e.closingOut != "" {
		return e.closingOut
	}
	return "claimed by " + e.holder + " first"
}

// passed is what run reports of the story: the closing-out gate's, when the
// claim was not made for a close-out's mark, and the claim lost's otherwise.
func (e *claimLost) passed(id string) Passed {
	if e.closingOut != "" {
		return Passed{StoryID: id, Gate: "closing-out", Why: e.closingOut, Waiting: true}
	}
	return Passed{StoryID: id, Gate: ClaimLostGate, Why: e.Error()}
}

// claimStory takes the claim on story id for start. Just before it, the story
// is read again and the closing-out gate asked once more (mw-gq6.1656): the
// ready list start was handed was read before the claim, and a close-out that
// marked the story since (one forwarded to this host over ssh, say) was not in
// it. The story is then passed over (a *claimLost that says so) and no attempt
// is spent. A story that cannot be read is claimed as it was before this look.
func (d Dispatch) claimStory(ctx context.Context, id string) error {
	if fresh, err := d.Tracker.ShowStory(ctx, id); err == nil {
		if why := d.closingOutWhy(ctx, fresh); why != "" {
			return &claimLost{closingOut: why}
		}
	}
	if err := d.Tracker.ClaimStory(ctx, id); err != nil {
		if holder, held := ClaimHeldBy(err); held {
			return &claimLost{holder: holder}
		}
		return fmt.Errorf("claiming %s: %w", id, err)
	}
	return nil
}

// reclaimDeadPane is reclaimDeadPaneNow, but for a story a close-out holds: it
// is left alone, a refused one reported held as ever, and no attempt is spent.
func (d Dispatch) reclaimDeadPane(ctx context.Context, detail StoryDetail, reads *candidateReads, report *DispatchReport) (reclaimed, held bool, err error) {
	if _, ok := d.closingOutNow(ctx, detail); !ok {
		return d.reclaimDeadPaneNow(ctx, detail, reads, report)
	}
	if d.refusedWithoutSession(ctx, detail, reads) {
		report.HeldRefused = append(report.HeldRefused, HeldRefused{StoryID: detail.Story.ID, Session: SessionName(detail.Story.ID)})
		return false, true, nil
	}
	return false, false, nil
}

// closingMark is the close-out mark of detail for mw status: this host's own
// file's, else the tracker's, worded the same.
func closingMark(local map[string]CloseOutMark, detail StoryDetail, now time.Time) (CloseOutMark, bool) {
	if mark, ok := local[detail.Story.ID]; ok {
		return mark, true
	}
	if detail.ClosingOut.Live(now) {
		return CloseOutMark{Story: detail.Story.ID, Since: detail.ClosingOut.Since}, true
	}
	return CloseOutMark{}, false
}

// markClosingOut writes the close-out's mark on the story in the tracker and
// renews it every HeartbeatInterval; the release it returns stops the renewal
// and clears the mark. A mark that cannot be written changes nothing about the
// close-out (the host's own file still says it runs). It returns ctx marked as
// the close-out's own.
func (n Next) markClosingOut(ctx context.Context, storyID string) (context.Context, func()) {
	if n.Tracker == nil || strings.TrimSpace(storyID) == "" {
		return ctx, func() {}
	}
	began := n.now()
	write := func(c context.Context, at time.Time) {
		mark := ClosingOut{By: n.Host, Since: began, Until: at.Add(ClosingOutLease)}
		_ = n.Tracker.SetStoryMetadata(c, storyID, map[string]string{ClosingOutField: mark.String()})
	}
	write(ctx, began)
	renew, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// waitFor, not n.wait: HeartbeatWait stands in for the claim's own clock,
		// and a second caller of it would run that clock twice as fast.
		for waitFor(renew, HeartbeatInterval) == nil {
			if renew.Err() != nil {
				return
			}
			write(renew, n.now())
		}
	}()
	return WithCloseOutOf(ctx, storyID), func() {
		cancel()
		<-done
		// Its own context: the close-out may have ended because its did.
		_ = n.Tracker.SetStoryMetadata(context.Background(), storyID, map[string]string{ClosingOutField: ""})
	}
}

// holdLeaseMarked is holdLease and the tracker's mark together: for as long as
// the close-out of storyID runs, its claim's lease is renewed and the story
// carries its closing-out mark. It returns the context the close-out goes on
// with, and the release that ends both and gives back what the lease renewal
// could not do.
func (n Next) holdLeaseMarked(ctx context.Context, storyID string) (context.Context, func() []string) {
	release := n.holdLease(ctx, storyID)
	ctx, unmark := n.markClosingOut(ctx, storyID)
	return ctx, func() []string {
		unmark()
		return release()
	}
}

// closingOutWords is, for each of the ready stories a close-out holds, the
// words mw status gives it: the same "closing out 21m" a story whose close-out
// runs on this host says, and that mw dispatch passes it over for it.
func (s Status) closingOutWords(ready []StoryDetail) map[string]string {
	words := map[string]string{}
	for _, detail := range ready {
		if mark, ok := closingMark(nil, detail, s.now()); ok {
			words[detail.Story.ID] = fmt.Sprintf("%s, by the Mayor's close-out on %s: dispatch leaves it alone", mark.since(s.now()), detail.ClosingOut.By)
		}
	}
	return words
}

// ReasonAttemptMoved is a close-out that found its story started again, as a
// later attempt, since it began (mw-gq6.1656): the formula and the session now
// carrying the story's name are that attempt's, not the ones it is closing out.
const ReasonAttemptMoved Reason = "attempt-moved"

// attemptSeen is what a close-out read of its story's attempt when it began:
// the count, and when the latest claim was made.
type attemptSeen struct {
	read     bool
	attempts int
	claimed  time.Time
}

func seenAttempt(detail StoryDetail) attemptSeen {
	return attemptSeen{read: true, attempts: detail.Attempts, claimed: detail.ClaimedAt}
}

// attemptMoved is nil while the story is still at the attempt the close-out
// began on, and otherwise the error that says a later one was started, which
// is also written on report as the reason it stopped. A dispatch stamps the
// time of its claim first and counts the attempt once its session is up, so
// either one moving is a later attempt. The count is not looked at when
// byCount is false: a send-back this close-out made counts an attempt of its
// own. Nothing else is written: the formula, the session and the claim are the
// later attempt's. A story that cannot be read is not taken to have moved.
func (n Next) attemptMoved(ctx context.Context, report *NextReport, byCount bool) error {
	if n.Tracker == nil || !report.seen.read {
		return nil
	}
	fresh, err := n.Tracker.ShowStory(ctx, report.StoryID)
	if err != nil || fresh.Closed() {
		return nil
	}
	moved := fresh.ClaimedAt.After(report.seen.claimed) || byCount && fresh.Attempts > report.seen.attempts
	if !moved {
		return nil
	}
	was := max(report.seen.attempts, 1)
	now := max(fresh.Attempts, was+1)
	report.Reason = ReasonAttemptMoved
	report.Why = fmt.Sprintf("attempt %d of %s was started while this close-out of attempt %d ran: its formula and its session are that attempt's, so nothing was judged, closed or changed",
		now, report.StoryID, was)
	return fmt.Errorf("closing out %s: %s (%s)", report.StoryID, report.Why, ReasonAttemptMoved)
}
