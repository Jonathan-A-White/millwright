package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// plainCipher keeps each plaintext it is asked to encrypt, so a test can read
// what a send carried.
type plainCipher struct{ plaintexts *[]string }

func (c plainCipher) Encrypt(_, text string) (string, error) {
	*c.plaintexts = append(*c.plaintexts, text)
	return "ct", nil
}
func (c plainCipher) EncryptBytes(string, []byte) (string, error) { return "ct", nil }
func (c plainCipher) Decrypt(string, string) (string, string, error) {
	return "", "", fmt.Errorf("plainCipher does not decrypt")
}

// channelHome sets up a home, a direct-channel backend and a stand-in bd for
// mw postern send, reporting the plaintexts every send carries and the file bd
// logs what it is asked to.
func channelHome(t *testing.T) (plaintexts *[]string, bdLog string) {
	t.Helper()
	f := loadPosternRecordFixture(t)
	backend := &directBackend{}
	posternHome(t, backend.serve(t), f.SenderWIF, f.RecipientPubKey)
	t.Setenv("MW_POSTERN_CHANNEL", "direct")
	bdLog = filepath.Join(t.TempDir(), "bd.log")
	bin := t.TempDir()
	stub := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\nexit 0\n", bdLog)
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	plaintexts = new([]string)
	realCipher, realClock := posternCipher, posternClock
	t.Cleanup(func() { posternCipher, posternClock = realCipher, realClock })
	posternCipher = func(*postern.KeyFile) application.Cipher { return plainCipher{plaintexts: plaintexts} }
	posternClock = func() time.Time { return time.Unix(f.Ts, 0) }
	return plaintexts, bdLog
}

// channelSend runs mw postern send with args on the direct channel, reporting
// the plaintexts sent, what bd was asked (one call per line) and the output.
func channelSend(t *testing.T, args ...string) (plaintexts []string, bdCalls string, out string, err error) {
	t.Helper()
	sent, log := channelHome(t)
	out, err = runPostern(t, append([]string{"send"}, args...)...)
	calls, _ := os.ReadFile(log)
	return *sent, string(calls), out, err
}

const channelRe = "direct:ab0d0cd9f1e2d3c4b5a697887766554433221100ffeeddccbbaa99887766a8b6"

func TestPosternSendBeadChannelAnswersInAPostsThreadAndCommentsTheBead(t *testing.T) {
	plaintexts, bd, out, err := channelSend(t, "--bead-channel", "mw-a.1", "--re", channelRe, "x")
	if err != nil {
		t.Fatalf("send failed: %v\n%s", err, out)
	}
	want := `{"thread":{"bead":"mw-a.1"},"text":"x","re":"` + channelRe + `"}`
	if len(plaintexts) != 1 || plaintexts[0] != want {
		t.Fatalf("expected one message %s, got %v", want, plaintexts)
	}
	if !strings.Contains(bd, "mw-a.1") || !strings.Contains(bd, "MAYOR via postern, txid direct:") || !strings.Contains(bd, ": x") {
		t.Fatalf("expected the bead commented, bd was asked:\n%s", bd)
	}
}

func TestPosternSendChannelAnswersInANamedChannelsThread(t *testing.T) {
	plaintexts, _, out, err := channelSend(t, "--channel", "roadmap", "--re", channelRe, "x")
	if err != nil {
		t.Fatalf("send failed: %v\n%s", err, out)
	}
	want := `{"thread":{"topic":"roadmap"},"text":"x","re":"` + channelRe + `"}`
	if len(plaintexts) != 1 || plaintexts[0] != want {
		t.Fatalf("expected one message %s, got %v", want, plaintexts)
	}
}

// The old names still work, and say they are deprecated.
func TestPosternSendOldFlagNamesStillSendAndSayDeprecated(t *testing.T) {
	for _, c := range []struct{ old, new, wire string }{
		{"--thread", "--bead-channel", `{"thread":{"bead":"mw-a.1"},"text":"x"}`},
		{"--topic", "--channel", `{"thread":{"topic":"mw-a.1"},"text":"x"}`},
	} {
		plaintexts, _, out, err := channelSend(t, c.old, "mw-a.1", "x")
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", c.old, err, out)
		}
		if len(plaintexts) != 1 || plaintexts[0] != c.wire {
			t.Fatalf("%s: expected %s, got %v", c.old, c.wire, plaintexts)
		}
		if !strings.Contains(out, "deprecated") || !strings.Contains(out, "use "+c.new) {
			t.Fatalf("%s: expected a deprecation note naming %s, got:\n%s", c.old, c.new, out)
		}
	}
}

func TestPosternSendRefusesWhatConflicts(t *testing.T) {
	for name, c := range map[string]struct {
		args []string
		want []string
	}{
		"two channels":     {[]string{"--channel", "roadmap", "--bead-channel", "mw-a.1", "x"}, []string{"--channel", "--bead-channel"}},
		"alias with new":   {[]string{"--thread", "mw-a.1", "--bead-channel", "mw-a.1", "x"}, []string{"--thread", "--bead-channel"}},
		"topic with new":   {[]string{"--topic", "a", "--channel", "a", "x"}, []string{"--topic", "--channel"}},
		"re with bead":     {[]string{"--class", "decision-needed", "--bead", "mw-a.1", "--re", channelRe, "x"}, []string{"--re", "--bead"}},
		"bead with class":  {[]string{"--bead", "mw-a.1", "x"}, []string{"--bead-channel <id> posts in a bead's channel"}},
		"question channel": {[]string{"--class", "decision-needed", "--bead", "mw-a.1", "--channel", "a", "x"}, []string{"decision-needed question"}},
	} {
		plaintexts, _, out, err := channelSend(t, c.args...)
		if err == nil {
			t.Errorf("%s: expected a refusal, sent %v\n%s", name, plaintexts, out)
			continue
		}
		for _, want := range c.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: expected the refusal to say %q, got %v", name, want, err)
			}
		}
		if len(plaintexts) != 0 {
			t.Errorf("%s: expected nothing sent, got %v", name, plaintexts)
		}
	}
}

func TestPosternSendHelpSpeaksOfChannelsAndThreads(t *testing.T) {
	out, err := runPostern(t, "send", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"topic", "--thread", "--topic"} {
		if strings.Contains(out, bad) {
			t.Errorf("expected the help to leave out %q, got:\n%s", bad, out)
		}
	}
	for _, want := range []string{"--bead-channel", "--channel", "answers inside a post's thread"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the help to say %q, got:\n%s", want, out)
		}
	}
}

// A reply to a post mw sent, with no channel flag, goes to that post's channel.
func TestPosternSendReWithNoChannelFlagAnswersInTheRootsChannel(t *testing.T) {
	for name, c := range map[string]struct {
		flags  []string
		thread string
	}{
		"bead channel":  {[]string{"--bead-channel", "mw-a.1"}, `{"bead":"mw-a.1"}`},
		"named channel": {[]string{"--channel", "roadmap"}, `{"topic":"roadmap"}`},
	} {
		sent, bdLog := channelHome(t)
		out, err := runPostern(t, append(append([]string{"send"}, c.flags...), "the root")...)
		if err != nil {
			t.Fatalf("%s: the root failed to send: %v\n%s", name, err, out)
		}
		root := strings.TrimSpace(out)
		if !strings.HasPrefix(root, "direct:") {
			t.Fatalf("%s: expected the root's txid first, got:\n%s", name, out)
		}
		out, err = runPostern(t, "send", "--re", root, "the reply")
		if err != nil {
			t.Fatalf("%s: the reply failed to send: %v\n%s", name, err, out)
		}
		want := `{"thread":` + c.thread + `,"text":"the reply","re":"` + root + `"}`
		if len(*sent) != 2 || (*sent)[1] != want {
			t.Fatalf("%s: expected the reply %s, got %v", name, want, *sent)
		}
		if name == "bead channel" {
			if calls, _ := os.ReadFile(bdLog); !strings.Contains(string(calls), ": the reply") {
				t.Fatalf("expected the reply commented on the bead, bd was asked:\n%s", calls)
			}
		}
	}
}

// A root mw has not seen is refused, with the flags that would say where, and
// nothing is sent; a channel flag with --re still goes where it says.
func TestPosternSendReWithNoChannelFlagRefusesAnUnseenRoot(t *testing.T) {
	plaintexts, _, out, err := channelSend(t, "--re", channelRe, "x")
	if err == nil {
		t.Fatalf("expected a refusal, sent %v\n%s", plaintexts, out)
	}
	want := "--re <root> needs the root's channel: give --bead-channel <id> or --channel <name>"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("expected the refusal to say %q, got %v", want, err)
	}
	if len(plaintexts) != 0 {
		t.Errorf("expected nothing sent, got %v", plaintexts)
	}
}

func TestPosternSendHelpSaysWhereAReplyGoes(t *testing.T) {
	out, err := runPostern(t, "send", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ROOT's\nchannel", "never\nsilently to Factory", "A channel flag given with --re wins"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the help to say %q, got:\n%s", want, out)
		}
	}
}
