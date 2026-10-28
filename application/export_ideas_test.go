package application

import "context"

// AboutAToDo lets the tests of package application_test ask the one rule every
// Ideas-only hold path decides through.
func (i PosternInbox) AboutAToDo(ctx context.Context, m PosternInboxMessage) (bool, error) {
	return i.aboutAToDo(ctx, m)
}

// TalkRecordClass and CallRecordClass are the classes mw talk wait's turns and
// calls are held under.
const (
	TalkRecordClass = talkRecordClass
	CallRecordClass = callRecordClass
)

// HoldIdea lets the tests of package application_test ask the one place Ideas
// only keeps a message, with a pass of its own.
func (i PosternInbox) HoldIdea(ctx context.Context, m PosternInboxMessage) (line string, kept, fresh bool, err error) {
	var pass ideasPass
	return i.holdIdea(ctx, m, &pass)
}
