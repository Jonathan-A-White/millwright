package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// CloseRetry is the retry a close of a bead gets wherever it is made, lifted
// out of Next so that a caller without a Next closes the way mw next does
// (mw-gq6.1131):
//
//   - a close that met a passing fault of the network (TransientFault) waits
//     on RetryWait, a growing part of PushWait, and is tried again, ReadRetries
//     times in all;
//   - a close bd said was indeterminate, which may have landed, is read first:
//     a bead that now reads closed is closed and is not written again, and one
//     that reads open is a passing fault, tried again;
//   - a close lost every try is mailed to the Mayor once, with how many times it
//     was tried, so that it is not only a line nobody reads.
//
// Next's own closes (closeStory, closeFormulaBead) share the rules below
// (settleClose, retryWrite and mailLost) and add what only a landing has.
type CloseRetry struct {
	// Close makes one close of a bead and Show reads it, as the tracker's
	// CloseStory and ShowStory do.
	Close func(ctx context.Context, id, reason string) error
	Show  func(ctx context.Context, id string) (StoryDetail, error)

	// Host is named in the mail of a close lost. PushWait is the base of the wait
	// between tries and RetryWait the wait itself: nil is a real wait. Mailbox
	// takes the mail; nil sends none. Err is where a mail that could not be sent
	// is said; nil says nothing.
	Host      string
	PushWait  time.Duration
	RetryWait func(ctx context.Context, d time.Duration) error
	Mailbox   Mailbox
	Err       io.Writer
}

// CloseStory closes the bead id for reason, as the type says. command and doing
// name who is closing and what they were doing ("mw triage", "sweeping
// orphans") in the mail of a close that fails every try. An error is the last
// try's, in the tracker's own words.
func (c CloseRetry) CloseStory(ctx context.Context, command, doing, id, reason string) error {
	tries := 0
	_, err := retryWrite(ctx, c.PushWait, c.wait, nil, "write", "the close of "+id, func() (struct{}, error) {
		tries++
		return struct{}{}, c.closeOnce(ctx, id, reason)
	})
	if err == nil {
		return nil
	}
	what := "the close of " + id
	c.mailLost(ctx, command, doing, id, nil, what, fmt.Sprintf("%s could not be closed: %v", id, err), tries)
	return err
}

// closeOnce makes one close and reads the bead if bd was unsure of it.
func (c CloseRetry) closeOnce(ctx context.Context, id, reason string) error {
	return settleClose(c.Close(ctx, id, reason), func() bool {
		detail, err := c.Show(ctx, id)
		return err == nil && detail.Closed()
	})
}

func (c CloseRetry) wait(ctx context.Context, d time.Duration) error {
	if c.RetryWait != nil {
		return c.RetryWait(ctx, d)
	}
	return waitFor(ctx, d)
}

// settleClose is the rule for a close that failed with err: nil if it did not
// fail, or if bd was unsure of it ("write commit result indeterminate", which
// may follow a close that landed and which bd will not retry for fear of
// applying it twice) or it met a passing fault and closed says the bead now
// reads closed. A bead that still reads open after an indeterminate close is a
// passing fault for retryWrite to try again; any other error comes back as it
// was (mw-gq6.990).
func settleClose(err error, closed func() bool) error {
	if err == nil {
		return nil
	}
	_, passing := Faulted(err)
	unsure := strings.Contains(strings.ToLower(err.Error()), "indeterminate")
	if !passing && !unsure {
		return err
	}
	if closed() {
		return nil
	}
	if !passing {
		return &TransientFault{Why: "write commit result indeterminate", Err: err}
	}
	return err
}

// retryWrite runs one write of the tracker or the mailbox, and when it failed on
// a passing fault tries it again ReadRetries times, waiting a growing part of
// pushWait between the tries, one note per retry when report is not nil. A write
// that fails every time comes back in the tracker's own words. It is the loop
// under Next's readRetrying and writeRetrying and under CloseRetry.
func retryWrite[T any](ctx context.Context, pushWait time.Duration, wait func(context.Context, time.Duration) error, report *NextReport, verb, what string, try func() (T, error)) (T, error) {
	for retry := 0; ; retry++ {
		got, err := try()
		if err == nil {
			return got, nil
		}
		fault, passing := Faulted(err)
		if !passing || retry == ReadRetries {
			return got, err
		}
		delay := pushWait / 4 * time.Duration(retry+1)
		if report != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s of %s failed (%s), retried in %s", verb, what, fault.Why, delay))
		}
		if waitErr := wait(ctx, delay); waitErr != nil {
			return got, fmt.Errorf("%w (and waiting %s to %s %s again: %v)", err, delay, verb, what, waitErr)
		}
	}
}

// mailLost tells the Mayor of a write that was lost for good: command, on
// c.Host, lost what while doing storyID, and said is the tracker's words. tries
// is how often it was tried (0 when the caller does not know); the mail says
// "tried again" only of a write that was (mw-gq6.990). A mail that cannot be
// sent is said on Err and changes nothing else.
func (c CloseRetry) mailLost(ctx context.Context, command, doing, storyID string, report *NextReport, what, said string, tries int) {
	if c.Mailbox == nil {
		return
	}
	c.mailLostVia(ctx, c.Mailbox.Send, command, doing, storyID, report, what, said, tries)
}

// mailLostVia is mailLost through send, the way to a mailbox: mw tidy has one
// of its own (mw-gq6.1132). A nil send mails nothing.
func (c CloseRetry) mailLostVia(ctx context.Context, send func(context.Context, NewMessage) (string, error), command, doing, storyID string, report *NextReport, what, said string, tries int) {
	if send == nil || ctx.Err() != nil {
		return
	}
	tried := "It failed"
	switch {
	case tries == 1:
		tried = "It was tried once; the fault was not a passing one, so it was not tried again, and it failed"
	case tries > 1:
		tried = fmt.Sprintf("It was tried %d times, waiting between the tries, and failed every time", tries)
	}
	mail := NewMessage{
		From:    SeatIdentity(MwSeat, c.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("lost write: %s on %s (%s)", what, storyID, c.Host),
		Body: fmt.Sprintf("%s on %s lost a write while %s %s: %s.\n\n%s\n\n%s; "+
			"nothing else was changed for it. Make the write by hand, with the command named above.\n", command, c.Host, doing, storyID, what, said, tried),
	}
	_, err := retryWrite(ctx, c.PushWait, c.wait, report, "write", "the mail about a lost write", func() (struct{}, error) {
		_, err := send(ctx, mail)
		return struct{}{}, err
	})
	if err != nil && c.Err != nil {
		fmt.Fprintf(c.Err, "%s: the mail to %s about a lost write of %s could not be sent: %v\n", command, MayorMailbox, storyID, err)
	}
}
