package application

import (
	"context"
	"fmt"
	"io"
	"time"
)

// ReportSent is one mw report send that went out: the UTC day it covers, the
// moment, and the txid of the message.
type ReportSent struct {
	Day  string    `json:"day"`
	At   time.Time `json:"at"`
	Txid string    `json:"txid"`
}

// ReportSentLog is where mw remembers the days it has sent the report: Sent
// reads them oldest first, none and no error when nothing was sent; Record adds
// one. The real adapter is infrastructure/reportlog; the fake is
// apptest.FakeReportSentLog.
type ReportSentLog interface {
	Sent(ctx context.Context) ([]ReportSent, error)
	Record(ctx context.Context, sent ReportSent) error
}

// ReportPoster sends a message to the Governor and reports its txid; it is
// what PosternSend is to this use case.
type ReportPoster interface {
	Run(ctx context.Context, req PosternSendRequest) (string, error)
}

// FactoryReportSend is mw report send: it builds the phone-sized summary of the
// week from FactoryReport and posts it, as a plain message to the Factory
// channel, from the home only and at most once a UTC day.
type FactoryReportSend struct {
	// Report is the report the summary is made from; its Out is not used.
	Report FactoryReport
	// Home is the vault's home file, and Host this host's name: the report goes
	// out from the home alone.
	Home HomeFile
	Host string
	Sent ReportSentLog
	Post ReportPoster
	// Force sends again though today's report has gone; DryRun prints the
	// summary and sends and remembers nothing.
	Force, DryRun bool
	Now           func() time.Time
	Out           io.Writer
}

// Run sends the summary, and reports the txid; empty when nothing was sent.
func (s FactoryReportSend) Run(ctx context.Context) (string, error) {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	now = now.UTC()
	today := now.Format(LedgerDate)

	record, err := WhereIsHome(ctx, s.Home)
	if err != nil {
		return "", fmt.Errorf("mw report send: %w", err)
	}
	if record.Host != s.Host {
		s.printf("%s is home, not %s: the report goes out from the home only; nothing sent\n", record.Host, s.Host)
		return "", nil
	}

	report := s.Report
	report.Now = func() time.Time { return now }
	report.Out, report.JSON = nil, false
	if s.DryRun {
		// A dry run writes nothing: no mail to the Mayor, no snapshot.
		report.Notes, report.Mailbox, report.Snapshots = nil, nil, nil
	}
	result, err := report.Run(ctx)
	if err != nil {
		return "", err
	}
	summary := result.Summary()
	if s.DryRun {
		s.printf("%s", summary)
		return "", nil
	}

	if !s.Force {
		sent, err := s.Sent.Sent(ctx)
		if err != nil {
			return "", fmt.Errorf("mw report send: the days sent: %w", err)
		}
		for _, was := range sent {
			if was.Day == today {
				s.printf("already sent today (%s) at %s, %s; --force sends again\n", today, was.At.UTC().Format("15:04:05Z"), was.Txid)
				return "", nil
			}
		}
	}

	txid, err := s.Post.Run(ctx, PosternSendRequest{Text: summary})
	if err != nil {
		return "", err
	}
	s.printf("%s\n", txid)
	if err := s.Sent.Record(ctx, ReportSent{Day: today, At: now, Txid: txid}); err != nil {
		return txid, fmt.Errorf("mw report send: the report went out (%s) but the day could not be remembered, so the next run will send again: %w", txid, err)
	}
	return txid, nil
}

func (s FactoryReportSend) printf(format string, args ...any) {
	if s.Out != nil {
		fmt.Fprintf(s.Out, format, args...)
	}
}
