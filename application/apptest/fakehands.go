package apptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeHandsSig is the signature FakeHandsVerifier takes as key's approval of
// the step hashing to sha at at: no cryptography, only a string a test can
// make and a mistake cannot.
func FakeHandsSig(key, sha string, at int64) string {
	return fmt.Sprintf("sig:%s:%s:%d", key, sha, at)
}

// FakeHandsVerifier is an in-memory application.HandsVerifier: an approval
// verifies when its signature is FakeHandsSig of the very key, hash and
// time it is checked against.
type FakeHandsVerifier struct{}

var _ application.HandsVerifier = FakeHandsVerifier{}

// VerifyApproval implements application.HandsVerifier.
func (FakeHandsVerifier) VerifyApproval(key, sha string, at int64, sig string) error {
	if sig != FakeHandsSig(key, sha, at) {
		return fmt.Errorf("the approval's signature is not the Governor's over this step at this time")
	}
	return nil
}

// FakeHandsRunner is an in-memory application.HandsRunner: it runs nothing,
// keeps every job it was given, and answers each with Outcome, or Err.
type FakeHandsRunner struct {
	mu sync.Mutex

	Outcome application.HandsOutcome
	Err     error

	jobs []application.HandsJob
}

var _ application.HandsRunner = (*FakeHandsRunner)(nil)

// Run implements application.HandsRunner.
func (f *FakeHandsRunner) Run(_ context.Context, job application.HandsJob) (application.HandsOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs = append(f.jobs, job)
	if f.Err != nil {
		return application.HandsOutcome{}, f.Err
	}
	return f.Outcome, nil
}

// Jobs reports every job Run was given, in order.
func (f *FakeHandsRunner) Jobs() []application.HandsJob {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]application.HandsJob(nil), f.jobs...)
}
