package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// NamedAsk is one ask of a host to run tests by name.
type NamedAsk struct {
	Host, Rig, Ref string
	Tests          []string
}

// FakeNamedElsewhere is an in-memory application.NamedElsewhere: a test says
// what each host's run of the named tests comes to, and reads back what was
// asked. A host it was told nothing of cannot be reached.
type FakeNamedElsewhere struct {
	mu   sync.Mutex
	runs map[string]application.NamedRun
	errs map[string]error
	asks []NamedAsk
}

// NewFakeNamedElsewhere returns a port that reaches no host.
func NewFakeNamedElsewhere() *FakeNamedElsewhere {
	return &FakeNamedElsewhere{runs: map[string]application.NamedRun{}, errs: map[string]error{}}
}

var _ application.NamedElsewhere = (*FakeNamedElsewhere)(nil)

// Runs makes host answer with run.
func (f *FakeNamedElsewhere) Runs(host string, run application.NamedRun) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs[host] = run
	delete(f.errs, host)
}

// Unreachable makes asking host fail with err.
func (f *FakeNamedElsewhere) Unreachable(host string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[host] = err
}

// Asked is what was asked, oldest first.
func (f *FakeNamedElsewhere) Asked() []NamedAsk {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]NamedAsk(nil), f.asks...)
}

// RunNamedOn implements application.NamedElsewhere.
func (f *FakeNamedElsewhere) RunNamedOn(_ context.Context, host, rig, ref string, tests []string) (application.NamedRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asks = append(f.asks, NamedAsk{Host: host, Rig: rig, Ref: ref, Tests: append([]string(nil), tests...)})
	if err := f.errs[host]; err != nil {
		return application.NamedRun{}, err
	}
	if run, ok := f.runs[host]; ok {
		return run, nil
	}
	return application.NamedRun{}, &unreachable{host}
}

type unreachable struct{ host string }

func (u *unreachable) Error() string { return "no way to reach " + u.host }
