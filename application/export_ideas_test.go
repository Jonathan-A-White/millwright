package application

import "context"

// AboutAToDo lets the tests of package application_test ask the one rule every
// Ideas-only hold path decides through.
func (i PosternInbox) AboutAToDo(ctx context.Context, m PosternInboxMessage) bool {
	return i.aboutAToDo(ctx, m)
}

// TalkRecordClass and CallRecordClass are the classes mw talk wait's turns and
// calls are held under.
const (
	TalkRecordClass = talkRecordClass
	CallRecordClass = callRecordClass
)
