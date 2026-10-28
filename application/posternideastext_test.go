package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
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
		if got, err := inbox.AboutAToDo(context.Background(), c.m); err != nil || got != c.want {
			t.Errorf("%s: aboutAToDo = %v (%v), want %v", c.name, got, err, c.want)
		}
	}
}

// Ideas only does not decide a message when the tracker cannot say which
// to-dos there are, unless the words say 'to do' themselves: the read's error
// is returned and the message is weighed again on the next pass (mw-gq6.827).
func TestIdeasOnlyDoesNotDecideWhatItCannotTieToAToDo(t *testing.T) {
	inbox := toDoTextInbox(t)
	inbox.Tracker.(*apptest.FakeTracker).Err = context.DeadlineExceeded
	if _, err := inbox.AboutAToDo(context.Background(), application.PosternInboxMessage{Txid: "direct:1", Text: "my discipleship notes"}); err == nil {
		t.Fatalf("expected the error of a to-do read that failed")
	}
	if got, err := inbox.AboutAToDo(context.Background(), application.PosternInboxMessage{Txid: "direct:2", Text: "my to do for Friday"}); err != nil || !got {
		t.Fatalf("expected a message that says 'to do' to pass whatever the tracker says, got %v, %v", got, err)
	}
}

// A message that begins 'TODO:' is the Governor asking for it to be looked at
// at once: Ideas only lets it through whatever its channel and whatever the
// tracker says, in any case and with or without a space after the colon
// (mw-8ifimx.18). Further in, the word still passes by the ordinary rule
// (it says 'todo'); the prefix is what holds in a bead's thread that is no
// to-do.
func TestIdeasOnlyLetsAMessageBeginningTodoColonThrough(t *testing.T) {
	inbox := toDoTextInbox(t)
	general := func(text string) application.PosternInboxMessage {
		return application.PosternInboxMessage{Txid: "direct:1", Class: "message", Thread: application.PosternGeneralThread, Text: text}
	}
	cases := []struct {
		name string
		m    application.PosternInboxMessage
		want bool
	}{
		{"upper case, a space", general("TODO: an app for the roof"), true},
		{"upper case, no space", general("TODO:an app for the roof"), true},
		{"lower case", general("todo: an app for the roof"), true},
		{"mixed case", general("ToDo: an app for the roof"), true},
		{"leading blanks", general("  \nTODO: an app for the roof"), true},
		{"no words after it", general("TODO:"), true},
		{"in a thread that is no to-do", application.PosternInboxMessage{Txid: "direct:1", Class: "message", Thread: "mw-lst.2", ThreadIsBead: true, Text: "TODO: an app for the roof"}, true},
		{"on a named topic", application.PosternInboxMessage{Txid: "direct:1", Class: "message", Thread: "ideas", Text: "TODO: an app for the roof"}, true},
		{"as a Talk turn", application.PosternInboxMessage{Txid: "direct:1", Class: application.TalkRecordClass, Thread: application.TalkRecordClass, Text: "TODO: an app for the roof"}, true},
	}
	for _, c := range cases {
		if got, err := inbox.AboutAToDo(context.Background(), c.m); err != nil || got != c.want {
			t.Errorf("%s: aboutAToDo = %v (%v), want %v", c.name, got, err, c.want)
		}
	}
	// With a tracker that cannot be read, the prefix still gets through.
	inbox.Tracker.(*apptest.FakeTracker).Err = context.DeadlineExceeded
	if got, err := inbox.AboutAToDo(context.Background(), general("TODO: an app for the roof")); err != nil || !got {
		t.Fatalf("expected TODO: to pass whatever the tracker says, got %v, %v", got, err)
	}
}

// A message about an open to-do that waits on an unfinished story is about a
// to-do all the same (mw-gq6.663).
func TestTextAboutAToDoThatWaitsOnAnUnfinishedStory(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-lst", domain.Path{})
	tracker.AddStory("mw-lst", domain.Story{ID: "mw-lst.1", Title: "Choose the Max plan or an API key"})
	if err := tracker.SetLabels("mw-lst.1", application.LabelHitl, application.LabelHitlTodo); err != nil {
		t.Fatal(err)
	}
	tracker.AddStory("mw-other", domain.Story{ID: "mw-story", Title: "The story it waits on"})
	if err := tracker.AddBlocker(context.Background(), "mw-lst.1", "mw-story"); err != nil {
		t.Fatal(err)
	}
	inbox := application.PosternInbox{Tracker: tracker, Graph: tracker, Out: &bytes.Buffer{}}
	for _, text := range []string{"what is mw-lst.1 about", "remind me to choose soon"} {
		m := application.PosternInboxMessage{Txid: "direct:1", Class: application.TalkRecordClass, Thread: application.TalkRecordClass, Text: text}
		if got, err := inbox.AboutAToDo(context.Background(), m); err != nil || !got {
			t.Errorf("%q is about the waiting to-do mw-lst.1 but was not taken to be", text)
		}
	}
}

// A filler word of five letters or more in a to-do's title ties no message to
// it: his 'bare claude workers' idea shared 'something' with the title of a
// to-do and got through Ideas only, while a message naming the volume still
// passes as about that to-do (mw-gq6.828).
func TestIdeasOnlyHoldsAnIdeaThatSharesOnlyAFillerWordWithAToDoTitle(t *testing.T) {
	f, inbox, files := ideasTodoFixture(t)
	f.tracker.AddEpic("mw-hitl", domain.Path{})
	f.tracker.AddStory("mw-hitl", domain.Story{ID: "mw-hitl.3", Title: "Governor: check the old TrueCrypt volume still unlocks, then move it to something modern"})
	mustDo(t, f.tracker.SetLabels("mw-hitl.3", application.LabelHitl, application.LabelHitlTodo))
	inbox.Sender = &application.PosternSend{
		Postern: f.backend, Cipher: f.cipher, Keys: stubPosternKeys{pubKey: applyInboxKey},
		GovernorKey: releaseTapGovernorKey,
	}
	idea := "At work, we've been using bare claude workers to save tokens.  Is something like that useful here (something like 1.3K context on start up). They are only given the exact context they are expected to need."
	f.message(t, releaseTapGovernorKey, "tx-bare-workers", idea)

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if len(files.held) != 1 || files.held[0].Txid != "tx-bare-workers" {
		t.Fatalf("expected the idea held, got %+v", files.held)
	}
	delivered := f.backend.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("expected one answer, got %d", len(delivered))
	}
	var payload application.PosternPayload
	mustDo(t, json.Unmarshal(delivered[0], &payload))
	plain, _, err := f.cipher.Decrypt("governor", payload.Ct)
	mustDo(t, err)
	if !strings.Contains(plain, "Recorded for after") {
		t.Fatalf("expected the answer 'Recorded for after ...', got %q", plain)
	}

	about := application.PosternInboxMessage{Txid: "direct:2", Class: "message", Thread: application.PosternGeneralThread, Text: "did the TrueCrypt volume unlock?"}
	if got, err := inbox.AboutAToDo(context.Background(), about); err != nil || !got {
		t.Fatalf("expected a message naming the TrueCrypt volume to pass as about its to-do, got %v, %v", got, err)
	}
}
