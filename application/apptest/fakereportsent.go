package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.ReportSentLog = (*FakeReportSentLog)(nil)

// FakeReportSentLog is the list of days mw report send has gone out, in memory.
type FakeReportSentLog struct{ sent []application.ReportSent }

// Sent implements application.ReportSentLog.
func (f *FakeReportSentLog) Sent(context.Context) ([]application.ReportSent, error) {
	return append([]application.ReportSent(nil), f.sent...), nil
}

// Record implements application.ReportSentLog.
func (f *FakeReportSentLog) Record(_ context.Context, sent application.ReportSent) error {
	f.sent = append(f.sent, sent)
	return nil
}

// All is what was recorded, oldest first.
func (f *FakeReportSentLog) All() []application.ReportSent {
	return append([]application.ReportSent(nil), f.sent...)
}
