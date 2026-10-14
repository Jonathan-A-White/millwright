package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.MetricSnapshots = (*FakeMetricSnapshots)(nil)

// FakeMetricSnapshots is the factory's dated key-metric snapshots, in memory.
type FakeMetricSnapshots struct{ kept []application.MetricSnapshot }

// Snapshots implements application.MetricSnapshots.
func (f *FakeMetricSnapshots) Snapshots(context.Context) ([]application.MetricSnapshot, error) {
	return f.All(), nil
}

// Append implements application.MetricSnapshots.
func (f *FakeMetricSnapshots) Append(_ context.Context, snapshot application.MetricSnapshot) error {
	f.kept = append(f.kept, snapshot)
	return nil
}

// All is what was kept, oldest first.
func (f *FakeMetricSnapshots) All() []application.MetricSnapshot {
	return append([]application.MetricSnapshot(nil), f.kept...)
}
