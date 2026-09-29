package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakePosternSender is an application.PosternSender that sends nothing: it
// keeps each request it is asked to send, and answers with Err when set.
type FakePosternSender struct {
	mu   sync.Mutex
	sent []application.PosternSendRequest

	// Err, when set, is what every Run returns, the request not kept.
	Err error
}

// FakePosternSender satisfies the port.
var _ application.PosternSender = (*FakePosternSender)(nil)

// Run keeps req, or fails with Err.
func (f *FakePosternSender) Run(_ context.Context, req application.PosternSendRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	f.sent = append(f.sent, req)
	return "fake-push-txid", nil
}

// Sent reports every request Run kept, oldest first.
func (f *FakePosternSender) Sent() []application.PosternSendRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]application.PosternSendRequest(nil), f.sent...)
}
