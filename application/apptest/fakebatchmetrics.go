package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.BatchMetrics = (*FakeBatchMetrics)(nil)

// FakeBatchMetrics is the batch lines of metrics/factory.jsonl, in memory.
type FakeBatchMetrics struct{ kept []application.BatchMetric }

// Batches implements application.BatchHistory.
func (f *FakeBatchMetrics) Batches(context.Context) ([]application.BatchMetric, error) {
	return f.All(), nil
}

// AppendBatch implements application.BatchMetrics.
func (f *FakeBatchMetrics) AppendBatch(_ context.Context, metric application.BatchMetric) error {
	f.kept = append(f.kept, metric)
	return nil
}

// All is what was kept, oldest first.
func (f *FakeBatchMetrics) All() []application.BatchMetric {
	return append([]application.BatchMetric(nil), f.kept...)
}
