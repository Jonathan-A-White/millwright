package application

import (
	"context"
	"fmt"
	"io"
	"time"
)

// DefaultParentNoteHour is the local hour a member's parent's note goes out
// when the [report] table says nothing.
const DefaultParentNoteHour = 7

// ParentNoteDayPrefix heads the day a parent's note is remembered under in the
// log of days sent, so that the factory report's own day, which shares the log,
// never stops it.
const ParentNoteDayPrefix = "parent-"

// ParentNoteSend is mw report send --class parent: a member's factory posts
// one note a day, at a local hour, to the parent's key on the member's own
// backend, built from the same report as mw report send with the story titles,
// ids and fuel left out (FactoryReportResult.ParentNote). It posts at the hour
// and at no other time unless Force is set, and at most once a local day.
type ParentNoteSend struct {
	// Report is the report the note is made from; its Out is not used. It writes
	// nothing: no mail, no snapshot.
	Report FactoryReport
	// Name is the member, as the parent calls them.
	Name string
	// Hour is the local hour, 0 to 23, the note goes out in.
	Hour int
	Sent ReportSentLog
	Post ReportPoster
	// Force posts at any hour, though today's note has gone; DryRun prints the
	// note and posts and remembers nothing.
	Force, DryRun bool
	// Now is the clock, in the member's own zone. The zero value reads the real
	// local one.
	Now func() time.Time
	Out io.Writer
}

// Run posts the note when it is due, and reports the txid; empty when nothing
// was posted.
func (s ParentNoteSend) Run(ctx context.Context) (string, error) {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if s.Hour < 0 || s.Hour > 23 {
		return "", fmt.Errorf("mw report send: the parent's note hour is %d: it must be 0 to 23", s.Hour)
	}
	today := ParentNoteDayPrefix + now.Format(LedgerDate)
	if !s.Force && !s.DryRun && now.Hour() != s.Hour {
		s.printf("not the hour: the parent's note goes out at %02d:00 local; nothing sent\n", s.Hour)
		return "", nil
	}

	report := s.Report
	// The window is since yesterday, by the UTC days the ledger keeps.
	report.Since = reportDay(now.UTC()).AddDate(0, 0, -1)
	report.Now = func() time.Time { return now.UTC() }
	report.Out, report.JSON = nil, false
	report.Notes, report.Mailbox, report.Snapshots = nil, nil, nil
	result, err := report.Run(ctx)
	if err != nil {
		return "", err
	}
	note := result.ParentNote(s.Name)
	if s.DryRun {
		s.printf("%s", note)
		return "", nil
	}

	if !s.Force {
		sent, err := s.Sent.Sent(ctx)
		if err != nil {
			return "", fmt.Errorf("mw report send: the days sent: %w", err)
		}
		for _, was := range sent {
			if was.Day == today {
				s.printf("already sent today (%s) at %s, %s; --force sends again\n", today, was.At.Format("15:04:05Z07:00"), was.Txid)
				return "", nil
			}
		}
	}

	txid, err := s.Post.Run(ctx, PosternSendRequest{Text: note})
	if err != nil {
		return "", err
	}
	s.printf("%s\n", txid)
	if err := s.Sent.Record(ctx, ReportSent{Day: today, At: now, Txid: txid}); err != nil {
		return txid, fmt.Errorf("mw report send: the note went out (%s) but the day could not be remembered, so the next run in the hour will send again: %w", txid, err)
	}
	return txid, nil
}

func (s ParentNoteSend) printf(format string, args ...any) {
	if s.Out != nil {
		fmt.Fprintf(s.Out, format, args...)
	}
}
