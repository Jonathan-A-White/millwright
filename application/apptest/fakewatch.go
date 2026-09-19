package apptest

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// errSSHFailed is what a FakeWatch whose ssh fails says.
var errSSHFailed = errors.New("ssh: connect to host: connection timed out")

// FakeWatch satisfies the probes mw watch reaches out through, with no network,
// no ssh and no disk. What answers is set with the fields; what was asked, and
// what was kept, is read back with the methods.
var _ application.WatchProbes = (*FakeWatch)(nil)

// FakeWatch is a world for mw watch: the URLs that answer, what ssh prints or
// that it fails, and the memory and log kept on the host.
type FakeWatch struct {
	mu sync.Mutex

	// Answering are the URLs that answer. Any other URL does not.
	Answering map[string]bool
	// Health is what `cat ~/.mw-health` prints over ssh, and SSHFails makes the
	// ssh itself fail instead.
	Health   string
	SSHFails bool
	// SaveErr, when set, is what saving the memory fails with, and LoadErr what
	// loading it fails with.
	SaveErr error
	LoadErr error

	memory  application.WatchMemory
	saves   int
	log     []string
	reached []string
	reads   []string
}

// NewFakeWatch is a world where nothing answers and nothing has been kept.
func NewFakeWatch() *FakeWatch {
	return &FakeWatch{Answering: map[string]bool{}}
}

// Reach implements application.WatchProbes.
func (f *FakeWatch) Reach(_ context.Context, url string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reached = append(f.reached, url)
	return f.Answering[url]
}

// ReadHealth implements application.WatchProbes.
func (f *FakeWatch) ReadHealth(_ context.Context, ssh string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, ssh)
	if f.SSHFails {
		return "", errSSHFailed
	}
	return f.Health, nil
}

// LoadMemory implements application.WatchProbes.
func (f *FakeWatch) LoadMemory(context.Context) (application.WatchMemory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.LoadErr != nil {
		return application.WatchMemory{}, f.LoadErr
	}
	return f.memory, nil
}

// SaveMemory implements application.WatchProbes.
func (f *FakeWatch) SaveMemory(_ context.Context, memory application.WatchMemory) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SaveErr != nil {
		return f.SaveErr
	}
	f.saves++
	f.memory = memory
	return nil
}

// AppendLog implements application.WatchProbes.
func (f *FakeWatch) AppendLog(_ context.Context, line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, line)
	return nil
}

// Memory is what was last saved.
func (f *FakeWatch) Memory() application.WatchMemory {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.memory
}

// FirstFailure is when the remembered run of failed checks began, or zero.
func (f *FakeWatch) FirstFailure() time.Time { return f.Memory().FirstFailure }

// Saves is how many times the memory was written.
func (f *FakeWatch) Saves() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saves
}

// Log is every line appended to the log, in order.
func (f *FakeWatch) Log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

// Reached is every URL asked about, in order, blog included.
func (f *FakeWatch) Reached() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reached...)
}

// Reads is the ssh host name of every health read, in order.
func (f *FakeWatch) Reads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reads...)
}

// SetMemory seeds what the last watch remembered, without counting a save.
func (f *FakeWatch) SetMemory(memory application.WatchMemory) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.memory = memory
}
