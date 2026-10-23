package application

import (
	"context"
	"time"
)

// BatchJobsRunning lets the tests of package application_test ask which rigs a
// running batch-land job is landing, and since when.
func (s Status) BatchJobsRunning(ctx context.Context) map[string]time.Time {
	return s.batchJobsRunning(ctx)
}
