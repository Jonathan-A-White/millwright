package application

import (
	"context"
	"time"
)

// Ideas only in mw talk wait (postern's docs/protocol.md section 25). While the
// Governor's key is in the mode, the wait keeps what he sends instead of
// ending on it: it appends each turn, call and message of his to the held file,
// answers it from the Mayor's key at zero tokens, and waits on. It keeps its
// note of the mode level from the stream's ideas-only event.

// ideasStart reads the mode before the wait begins, and says so when it is on.
func (r *talkWaitRun) ideasStart(ctx context.Context) {
	if r.Ideas == nil {
		return
	}
	r.refreshIdeas(ctx)
	if r.ideasOn() {
		r.printf(r.Out, "%s\n", r.ideasBanner(ctx))
	}
}

// refreshIdeas asks the backend, and goes on with the note when it will not say.
func (r *talkWaitRun) refreshIdeas(ctx context.Context) {
	state, err := r.Ideas.Refresh(ctx)
	if err != nil {
		r.printf(r.Err, "mw talk wait: %v; going on with the last note of Ideas only\n", err)
	}
	r.setIdeaState(state)
}

func (r *talkWaitRun) setIdeaState(state IdeasOnlyState) {
	r.ideaMu.Lock()
	defer r.ideaMu.Unlock()
	r.ideaState = state
}

func (r *talkWaitRun) ideaStateNow() IdeasOnlyState {
	r.ideaMu.Lock()
	defer r.ideaMu.Unlock()
	return r.ideaState
}

// ideasOn reports whether the Governor is in Ideas only now.
func (r *talkWaitRun) ideasOn() bool {
	return r.Ideas != nil && r.ideaStateNow().OnAt(r.Ideas.now())
}

// ideasBanner is 'Ideas only until 21:00 UTC (2 ideas held)'.
func (r *talkWaitRun) ideasBanner(ctx context.Context) string {
	held, _ := r.Ideas.Files.Held(ctx)
	return IdeasOnlyBanner(r.ideaStateNow(), len(held), r.Ideas.now())
}

// ideasEvent keeps the note level with an ideas-only event of the stream.
func (r *talkWaitRun) ideasEvent(event PosternEvent) {
	if r.Ideas == nil {
		return
	}
	state, err := r.Ideas.Apply(r.ctx, event)
	if err != nil {
		r.printf(r.Err, "mw talk wait: keeping the note of Ideas only: %v\n", err)
		return
	}
	r.setIdeaState(state)
}

// inboxForIdeas is the inbox the wait keeps a record with: the same keeping
// mw postern inbox --apply does, answered from the Mayor's key.
func (r *talkWaitRun) inboxForIdeas() PosternInbox {
	return PosternInbox{
		Postern: r.Postern, Cipher: r.Cipher, Keys: r.Keys, Memory: r.Memory,
		GovernorKey: r.GovernorKey, AttachmentDir: r.AttachmentDir, Transcriber: r.Transcriber,
		Ideas: r.Ideas, Lock: r.Lock, Now: r.Now,
		Sender: &PosternSend{Postern: r.Postern, Cipher: r.Cipher, Keys: r.Keys, GovernorKey: r.GovernorKey, Now: r.Now},
	}
}

// keepIdea keeps m, a verified record of the Governor's that the wait would
// otherwise end on, when the mode is on or it is already kept; answer says
// 'Recorded for after ...' in the way the record is answered, the thread's
// when nil. It reports whether the wait is to go on past it. A record that
// could not be kept is said and left to end the wait as it would have, so that
// it is never lost.
func (r *talkWaitRun) keepIdea(m PosternInboxMessage, answer func(context.Context, string) error) bool {
	if r.Ideas == nil {
		return false
	}
	inbox := r.inboxForIdeas()
	release, err := inbox.lock(r.ctx)
	if err != nil {
		r.printf(r.Err, "mw talk wait: %v\n", err)
		return false
	}
	defer release()
	held, err := heldNotes(r.ctx, r.Memory)
	if err == nil {
		pass := ideasPass{loaded: true, state: r.ideaStateNow(), held: held}
		var kept bool
		if _, kept, _, err = inbox.holdIdeaAnswering(r.ctx, m, &pass, answer); err == nil {
			return kept
		}
	}
	r.printf(r.Err, "mw talk wait: keeping %s for after Ideas only: %v\n", m.Txid, err)
	return false
}

// keepTurn keeps the Governor's turn of a talk; the answer goes in the talk,
// as an answer to that turn. The end of a talk is kept without one.
func (r *talkWaitRun) keepTurn(record PosternRecord, turn *TalkTurn) bool {
	m := PosternInboxMessage{Seq: record.Seq, Txid: record.Txid, Class: talkRecordClass, Thread: talkRecordClass, Text: turn.Text}
	if turn.Role == TalkRoleEnd {
		m.Text = "(the end of talk " + turn.Talk.ID + ")"
	}
	answer := func(ctx context.Context, text string) error {
		if turn.Role == TalkRoleEnd || turn.Talk.Turn < 1 {
			return nil
		}
		_, err := TalkSay{Postern: r.Postern, Cipher: r.Cipher, Keys: r.Keys, GovernorKey: r.GovernorKey, Now: r.Now}.
			Run(ctx, TalkSayRequest{Text: text, TalkID: turn.Talk.ID, Turn: turn.Talk.Turn})
		return err
	}
	return r.keepIdea(m, answer)
}

// keepCall keeps the Governor's call request, or his later on a ring; the
// answer is a message from the Mayor in the general thread, re the call.
func (r *talkWaitRun) keepCall(record PosternRecord, call *CallRecord) bool {
	m := PosternInboxMessage{Seq: record.Seq, Txid: record.Txid, Class: callRecordClass, Thread: callRecordClass, Text: call.Text}
	if call.Role == CallRoleLater {
		m.Text = "later on the ring " + call.RingTxid
	}
	inbox := r.inboxForIdeas()
	general := PosternInboxMessage{Txid: record.Txid, Thread: PosternGeneralThread}
	return r.keepIdea(m, func(ctx context.Context, text string) error { return inbox.receipt(ctx, general, text) })
}

// keepsMessage reports whether message, a post for the Mayor's key that would
// end the wait, is the Governor's and kept: by this wait now, or by the hook
// before. Posts of anyone else are never kept.
func (r *talkWaitRun) keepsMessage(message PosternInboxMessage) bool {
	if r.Ideas == nil || !message.Verified || message.From != r.GovernorKey {
		return false
	}
	return r.keepIdea(message, nil)
}

// ideasEnd is what the wait says as it ends on its limit while the mode is on.
func (r *talkWaitRun) ideasEnd(ctx context.Context) {
	if r.ideasOn() {
		r.printf(r.Out, "%s\n", r.ideasBanner(context.WithoutCancel(ctx)))
	}
}

// ideaWatchEvery is how often the wait looks at the clock for the end of Ideas
// only; ideaRetryAfter how long it leaves a release that could not be done.
const (
	ideaWatchEvery = 250 * time.Millisecond
	ideaRetryAfter = 5 * time.Second
)

// ideasDue reports whether the mode is off while ideas are still held: the end
// came while no wait or hook was there to see it.
func (r *talkWaitRun) ideasDue(ctx context.Context) bool {
	if r.Release == nil || r.Ideas == nil || r.ideasOn() {
		return false
	}
	held, err := r.Ideas.Files.Held(ctx)
	return err == nil && len(held) > 0
}

// giveBackIdeas gives the held ideas back, once, and reports whether the wait
// is to end on them. Nothing held is said (unless quiet) and the wait goes on,
// for it has nothing to wake the Mayor for; a mode still on, or a release that
// failed (said on Err), also goes on.
func (r *talkWaitRun) giveBackIdeas(quiet bool) bool {
	ctx := context.WithoutCancel(r.ctx)
	report, err := r.Release.Run(ctx)
	if state, serr := r.Ideas.Files.State(ctx); serr == nil {
		r.setIdeaState(state)
	}
	switch {
	case err != nil:
		r.printf(r.Err, "mw talk wait: giving back the ideas of Ideas only: %v\n", err)
		return false
	case report.StillOn:
		return false
	case !report.Given():
		if !quiet {
			r.printf(r.Out, "%s", report.String())
		}
		return false
	}
	r.gaveBack = &report
	return true
}

// watchIdeasEnd ends the wait when the clock passes the end of the mode its note
// names, though no event came to say so: the backend's own is not to be relied
// on across a dropped stream. It looks at the note, never the backend, until the
// end is due, and then gives the ideas back; a release that cannot be done is
// tried again after ideaRetryAfter.
func (r *talkWaitRun) watchIdeasEnd(ctx context.Context, stop context.CancelCauseFunc) {
	var retryAt time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(ideaWatchEvery):
		}
		state := r.ideaStateNow()
		if !state.On || state.Until == 0 || state.OnAt(r.Ideas.now()) || time.Now().Before(retryAt) {
			continue
		}
		retryAt = time.Now().Add(ideaRetryAfter)
		if r.giveBackIdeas(true) {
			stop(errIdeasEnded)
			return
		}
	}
}

// ideasEnded is how the wait ends on the end of Ideas only: it prints the ideas
// it gave back, and the Mayor's harness wakes to them.
func (r *talkWaitRun) ideasEnded(ctx context.Context) (TalkWaitReport, error) {
	ctx = context.WithoutCancel(ctx)
	report := TalkWaitReport{Released: r.gaveBack}
	if r.gaveBack != nil {
		r.printf(r.Out, "%s", r.gaveBack.String())
	}
	return report, r.save(ctx)
}
