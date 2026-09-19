package apptest

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FirstSent is when the first message a FakeMailbox takes is stamped as sent;
// each message after it is a minute later, so that "oldest first" is testable.
var FirstSent = time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)

// FakeMailbox is an in-memory application.Mailbox. Messages are given ids
// mail-1, mail-2, and so on.
type FakeMailbox struct {
	mu sync.Mutex

	messages map[string]*fakeMessage
	sent     int

	// writes counts the calls that changed something, so that a test can say a
	// refusal wrote nothing.
	writes int

	// Err, when set, is returned by every method instead of doing the work.
	Err error
}

type fakeMessage struct {
	application.Message
	read bool
}

// FakeMailbox satisfies the port.
var _ application.Mailbox = (*FakeMailbox)(nil)

// NewFakeMailbox returns an empty mailbox.
func NewFakeMailbox() *FakeMailbox {
	return &FakeMailbox{messages: map[string]*fakeMessage{}}
}

// Writes reports how many calls changed something: a Send, or a Read that
// marked an unread message read.
func (f *FakeMailbox) Writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

// Send implements application.Mailbox.
func (f *FakeMailbox) Send(_ context.Context, message application.NewMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	f.sent++
	id := fmt.Sprintf("mail-%d", f.sent)
	f.messages[id] = &fakeMessage{Message: application.Message{
		ID:      id,
		From:    message.From,
		To:      message.To,
		Subject: message.Subject,
		Body:    message.Body,
		Sent:    FirstSent.Add(time.Duration(f.sent-1) * time.Minute),
	}}
	f.writes++
	return id, nil
}

// Inbox implements application.Mailbox.
func (f *FakeMailbox) Inbox(_ context.Context, mailbox string) ([]application.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	var unread []application.Message
	for _, message := range f.messages {
		if message.To == mailbox && !message.read {
			unread = append(unread, message.Message)
		}
	}
	sort.Slice(unread, func(i, j int) bool { return unread[i].Sent.Before(unread[j].Sent) })
	return unread, nil
}

// Read implements application.Mailbox.
func (f *FakeMailbox) Read(_ context.Context, id, _ string) (application.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return application.Message{}, f.Err
	}
	message, ok := f.messages[id]
	if !ok {
		return application.Message{}, fmt.Errorf("no mail %s", id)
	}
	if !message.read {
		message.read = true
		f.writes++
	}
	return message.Message, nil
}
