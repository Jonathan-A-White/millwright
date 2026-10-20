package application_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// toDoTextInbox is an inbox over a tracker that holds one open to-do, mw-lst.1
// 'Write the lesson plan', on the list 'Discipleship Institute', and one story
// that is no to-do.
func toDoTextInbox(t *testing.T) application.PosternInbox {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-lst", domain.Path{})
	tracker.DescribeEpic("mw-lst", "Discipleship Institute", apptest.StatusOpen, application.DefaultPriority)
	tracker.AddStory("mw-lst", domain.Story{ID: "mw-lst.1", Title: "Write the lesson plan"})
	if err := tracker.SetLabels("mw-lst.1", application.LabelHitl, application.LabelHitlTodo); err != nil {
		t.Fatal(err)
	}
	tracker.AddStory("mw-lst", domain.Story{ID: "mw-lst.2", Title: "Lampas hosting"})
	return application.PosternInbox{Tracker: tracker, Out: &bytes.Buffer{}}
}

// Ideas only holds what is not about a to-do, whichever way it came in: the
// one rule, aboutAToDo, decides for mw postern inbox, mw talk wait's turns,
// calls and messages, the general channel and a bead's thread alike
// (mw-8ifimx.13).
func TestEveryIdeasOnlyHoldPathDecidesThroughAboutAToDo(t *testing.T) {
	inbox := toDoTextInbox(t)
	talk := func(text string) application.PosternInboxMessage {
		return application.PosternInboxMessage{Txid: "direct:1", Class: application.TalkRecordClass, Thread: application.TalkRecordClass, Text: text}
	}
	call := func(text string) application.PosternInboxMessage {
		return application.PosternInboxMessage{Txid: "direct:1", Class: application.CallRecordClass, Thread: application.CallRecordClass, Text: text}
	}
	general := func(text string) application.PosternInboxMessage {
		return application.PosternInboxMessage{Txid: "direct:1", Class: "message", Thread: application.PosternGeneralThread, Text: text}
	}
	topic := func(text string) application.PosternInboxMessage {
		return application.PosternInboxMessage{Txid: "direct:1", Class: "message", Thread: "ideas", Text: text}
	}
	inBead := func(bead, text string) application.PosternInboxMessage {
		return application.PosternInboxMessage{Txid: "direct:1", Class: "message", Thread: bead, ThreadIsBead: true, Text: text}
	}
	cases := []struct {
		name string
		m    application.PosternInboxMessage
		want bool
	}{
		{"talk turn about his to do and the list's title", talk("can you help me with the instructions in my to do for my discipleship Institute"), true},
		{"talk turn, the list's title word alone", talk("how do I start the Discipleship thing"), true},
		{"talk turn, a card's title word", talk("what should the weather look like"), false},
		{"talk turn, a card's distinctive title word", talk("remind me about the lesson plan"), true},
		{"talk turn, a new idea", talk("a new idea for Lampas"), false},
		{"talk turn, the id of a to-do", talk("what is mw-lst.1 about"), true},
		{"talk turn, the id of a longer bead", talk("what is mw-lst.10 about"), false},
		{"talk turn, the id of a child of a to-do", talk("what is mw-lst.1.1 about"), false},
		{"call request about a to-do", call("call me about my to-do list"), true},
		{"call request about the roof", call("Call me about the roof"), false},
		{"general message, todo", general("helping me because I'm asking about to do"), true},
		{"general message, todo in one word", general("add it to my todo"), true},
		{"general message, to-do", general("my to-do for Friday"), true},
		{"general message, a new idea", general("a new idea for Lampas"), false},
		{"general message, a story that is no to-do", general("what about Lampas hosting"), false},
		{"named topic, about a to-do", topic("my discipleship notes"), true},
		{"named topic, an idea", topic("an app for the roof"), false},
		{"message in a to-do's thread", inBead("mw-lst.1", "done, thanks"), true},
		{"message in another bead's thread", inBead("mw-lst.2", "a new idea for Lampas"), false},
		{"empty text", general(""), false},
		{"an action on a to-do", general(`{"action":"close","bead":"mw-lst.1"}`), true},
		{"an action on another bead, whatever the words", general(`{"action":"todo_move","bead":"mw-lst.2"}`), false},
		{"a reply on a to-do", general(`{"bead":"mw-lst.1","answer":"ok"}`), true},
	}
	for _, c := range cases {
		if got := inbox.AboutAToDo(context.Background(), c.m); got != c.want {
			t.Errorf("%s: aboutAToDo = %v, want %v", c.name, got, c.want)
		}
	}
}

// Ideas only holds a message when the tracker cannot say which to-dos there
// are, unless the words say 'to do' themselves.
func TestIdeasOnlyHoldsWhatItCannotTieToAToDo(t *testing.T) {
	inbox := toDoTextInbox(t)
	inbox.Tracker.(*apptest.FakeTracker).Err = context.DeadlineExceeded
	if inbox.AboutAToDo(context.Background(), application.PosternInboxMessage{Txid: "direct:1", Text: "my discipleship notes"}) {
		t.Fatalf("expected a message held when the to-dos cannot be read")
	}
	if !inbox.AboutAToDo(context.Background(), application.PosternInboxMessage{Txid: "direct:2", Text: "my to do for Friday"}) {
		t.Fatalf("expected a message that says 'to do' to pass whatever the tracker says")
	}
}
