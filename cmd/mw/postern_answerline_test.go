package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// The answer line mw talk wait and mw postern inbox print under a message
// from the Governor is a command: run as written, mw postern send must send
// it, in the right channel and thread (mw-gq6.435).

const (
	answerMayorKey    = "mayor-pubkey-hex"
	answerGovernorKey = "governor-pubkey-hex"
)

// answerKeys is the Mayor's key, as the wait and the inbox read it.
type answerKeys struct{}

func (answerKeys) Path() string                                           { return "" }
func (answerKeys) Exists() (bool, error)                                  { return true, nil }
func (answerKeys) Generate() error                                        { return nil }
func (answerKeys) PublicKey() (string, string, error)                     { return answerMayorKey, "", nil }
func (answerKeys) PrivateKeyWIF() (string, error)                         { return "priv", nil }
func (answerKeys) Sign([]application.PosternUtxo, []byte) (string, error) { return "", nil }

func (answerKeys) MarkSpent([]application.PosternUtxo) error { return nil }
func (answerKeys) MarkSent(string) error                     { return nil }

// answerStream says hello and then announces the record at seq.
type answerStream struct{ seq int64 }

func (s answerStream) Events(_ context.Context, onEvent func(application.PosternEvent) error) error {
	if err := onEvent(application.PosternEvent{Kind: application.PosternEventHello}); err != nil {
		return err
	}
	return onEvent(application.PosternEvent{Kind: application.PosternEventMessage, Seq: s.seq})
}

// governorPost is one message from the Governor to the Mayor: its txid, the
// channel it is in (a zero thread is General) and the post it answers.
type governorPost struct {
	txid   string
	thread application.PosternThread
	re     string
}

// add indexes p on backend, encrypted as the Governor's, returning its seq.
func (p governorPost) add(t *testing.T, backend *apptest.FakePostern) int64 {
	t.Helper()
	plain := "hello Mayor"
	if p.thread != (application.PosternThread{}) || p.re != "" {
		raw, err := json.Marshal(application.PosternThreadedMessage{Thread: p.thread, Text: plain, Re: p.re})
		if err != nil {
			t.Fatal(err)
		}
		plain = string(raw)
	}
	cipher := apptest.NewFakeCipher()
	cipher.From = answerGovernorKey
	ct, err := cipher.Encrypt(answerMayorKey, plain)
	if err != nil {
		t.Fatal(err)
	}
	return backend.AddRecord(application.PosternRecord{
		Txid: p.txid, Class: "message", From: answerGovernorKey, To: answerMayorKey, Signer: answerGovernorKey, Ciphertext: ct,
	}).Seq
}

// answerLineOf is the command on the "answer in its thread:" line of out,
// split into mw's arguments as a shell would split it, without the leading
// "mw".
func answerLineOf(t *testing.T, out string) []string {
	t.Helper()
	const lead = "answer in its thread: "
	for _, line := range strings.Split(out, "\n") {
		if _, command, ok := strings.Cut(line, lead); ok {
			args := shellWords(t, command)
			if len(args) == 0 || args[0] != "mw" {
				t.Fatalf("expected the answer line to be an mw command, got %q", command)
			}
			return args[1:]
		}
	}
	t.Fatalf("expected an answer line, got:\n%s", out)
	return nil
}

// shellWords splits command into its words as sh itself does, quoting and
// all, expanding nothing it would not.
func shellWords(t *testing.T, command string) []string {
	t.Helper()
	out, err := exec.Command("sh", "-c", `printf '%s\0' `+command).Output()
	if err != nil {
		t.Fatalf("sh could not read %q: %v", command, err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
}

// runAnswerLine runs args (an answer line, mw postern send ...) as mw does,
// and reports the one plaintext it sent.
func runAnswerLine(t *testing.T, args []string, plaintexts *[]string) application.PosternThreadedMessage {
	t.Helper()
	if len(args) < 2 || args[0] != "postern" || args[1] != "send" {
		t.Fatalf("expected mw postern send, got %q", args)
	}
	out, err := runPostern(t, args[1:]...)
	if err != nil {
		t.Fatalf("the answer line %q failed as written: %v\n%s", strings.Join(args, " "), err, out)
	}
	if len(*plaintexts) != 1 {
		t.Fatalf("expected one message sent, got %v", *plaintexts)
	}
	var sent application.PosternThreadedMessage
	if err := json.Unmarshal([]byte((*plaintexts)[0]), &sent); err != nil {
		t.Fatalf("the message sent is not threaded: %v: %s", err, (*plaintexts)[0])
	}
	return sent
}

// answerCases are the posts the Governor sends that an answer line is printed
// under, and where the answer must go: the root's thread, in the root's channel.
var answerCases = map[string]struct {
	post        governorPost
	wantRe      string
	wantChannel application.PosternThread
}{
	"a General post":                         {governorPost{txid: "direct:post1"}, "direct:post1", application.PosternThread{}},
	"a General reply to a root mw never saw": {governorPost{txid: "direct:reply1", re: "direct:root1"}, "direct:root1", application.PosternThread{}},
	"a post in a named channel":              {governorPost{txid: "direct:topic1", thread: application.PosternThread{Topic: "road map"}}, "direct:topic1", application.PosternThread{Topic: "road map"}},
	"a reply in a named channel":             {governorPost{txid: "direct:topic2", thread: application.PosternThread{Topic: "roadmap"}, re: "direct:root2"}, "direct:root2", application.PosternThread{Topic: "roadmap"}},
	"a named channel sh would expand":        {governorPost{txid: "direct:topic3", thread: application.PosternThread{Topic: `it's $HOME's "plan" \ !`}}, "direct:topic3", application.PosternThread{Topic: `it's $HOME's "plan" \ !`}},
	"a reply in a bead's channel":            {governorPost{txid: "direct:bead1", thread: application.PosternThread{Bead: "mw-a.1"}, re: "direct:root3"}, "direct:root3", application.PosternThread{Bead: "mw-a.1"}},
}

func checkAnswer(t *testing.T, sent application.PosternThreadedMessage, wantRe string, wantChannel application.PosternThread) {
	t.Helper()
	if sent.Re != wantRe {
		t.Errorf("expected the answer to hang from %s, it named re %q", wantRe, sent.Re)
	}
	if sent.Thread != wantChannel {
		t.Errorf("expected the answer in channel %+v, it went to %+v", wantChannel, sent.Thread)
	}
}

// mw talk wait's answer line, run as written, sends.
func TestTalkWaitsAnswerLineRunsAsWritten(t *testing.T) {
	for name, c := range answerCases {
		t.Run(name, func(t *testing.T) {
			plaintexts, _ := channelHome(t)
			threads, err := posternThreads()
			if err != nil {
				t.Fatal(err)
			}
			backend := apptest.NewFakePostern()
			seq := c.post.add(t, backend)
			var out strings.Builder
			_, err = application.TalkWait{
				Stream: answerStream{seq: seq}, Postern: backend, Cipher: apptest.NewFakeCipher(), Keys: answerKeys{},
				Memory: apptest.NewFakeTracker(), GovernorKey: answerGovernorKey, Threads: threads,
				Limit: 200 * time.Millisecond, MinBackoff: 5 * time.Millisecond, MaxBackoff: 5 * time.Millisecond,
				Out: &out, Err: &out,
			}.Run(context.Background())
			if err != nil {
				t.Fatalf("the wait: %v\n%s", err, out.String())
			}

			sent := runAnswerLine(t, answerLineOf(t, out.String()), plaintexts)

			checkAnswer(t, sent, c.wantRe, c.wantChannel)
		})
	}
}

// mw postern inbox's answer line, run as written, sends.
func TestPosternInboxsAnswerLineRunsAsWritten(t *testing.T) {
	for name, c := range answerCases {
		t.Run(name, func(t *testing.T) {
			plaintexts, _ := channelHome(t)
			threads, err := posternThreads()
			if err != nil {
				t.Fatal(err)
			}
			backend := apptest.NewFakePostern()
			c.post.add(t, backend)
			var out strings.Builder
			_, err = application.PosternInbox{
				Postern: backend, Cipher: apptest.NewFakeCipher(), Keys: answerKeys{},
				Memory: apptest.NewFakeTracker(), GovernorKey: answerGovernorKey, Threads: threads, Out: &out,
			}.Run(context.Background())
			if err != nil {
				t.Fatalf("the inbox: %v\n%s", err, out.String())
			}

			sent := runAnswerLine(t, answerLineOf(t, out.String()), plaintexts)

			checkAnswer(t, sent, c.wantRe, c.wantChannel)
		})
	}
}
