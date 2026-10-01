package apptest

import (
	"strings"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakePosternThreadIndex is an application.PosternThreadIndex held in memory.
type FakePosternThreadIndex struct {
	mu      sync.Mutex
	threads map[string]application.PosternThread

	// Err, when set, is what every Remember and Lookup returns.
	Err error
}

// FakePosternThreadIndex satisfies the port.
var _ application.PosternThreadIndex = (*FakePosternThreadIndex)(nil)

// NewFakePosternThreadIndex is an index that has seen no post.
func NewFakePosternThreadIndex() *FakePosternThreadIndex {
	return &FakePosternThreadIndex{threads: map[string]application.PosternThread{}}
}

// Remember keeps thread under txid, bare or "direct:"-prefixed alike.
func (f *FakePosternThreadIndex) Remember(txid string, thread application.PosternThread) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.threads[strings.TrimPrefix(strings.TrimSpace(txid), "direct:")] = thread
	return nil
}

// Lookup reports what Remember kept under txid.
func (f *FakePosternThreadIndex) Lookup(txid string) (application.PosternThread, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return application.PosternThread{}, false, f.Err
	}
	thread, ok := f.threads[strings.TrimPrefix(strings.TrimSpace(txid), "direct:")]
	return thread, ok, nil
}
