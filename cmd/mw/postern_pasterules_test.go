package main

import (
	"os"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

const base64Message = "run echo W1Vu | base64 -d"

// A message that breaks a paste rule is refused with the rule's name and
// sends nothing; --allow with a reason and a bead sends it and writes the
// reason on the bead.
func TestPosternSendRefusesWithTheRulesName(t *testing.T) {
	plaintexts, bd, out, err := channelSend(t, base64Message)
	if err == nil || !strings.Contains(err.Error(), "no-base64") {
		t.Fatalf("expected a refusal naming no-base64, got %v\n%s", err, out)
	}
	if len(plaintexts) != 0 || strings.Contains(bd, "comment") {
		t.Fatalf("expected nothing sent or written, got %v / %s", plaintexts, bd)
	}

	plaintexts, bd, out, err = channelSend(t, "--allow", "no-base64", "--allow-why", "a fixture of a test", "--bead-channel", "mw-a.1", base64Message)
	if err != nil {
		t.Fatalf("expected the allowed send to go, got %v\n%s", err, out)
	}
	if len(plaintexts) != 1 {
		t.Fatalf("expected one message sent, got %v", plaintexts)
	}
	if !strings.Contains(bd, "comment mw-a.1 ALLOWED no-base64: a fixture of a test") {
		t.Errorf("expected the reason written on the bead, bd was asked:\n%s", bd)
	}

	for name, args := range map[string][]string{
		"no bead named":   {"--allow", "no-base64", "--allow-why", "because", base64Message},
		"no reason":       {"--allow", "no-base64", "--bead-channel", "mw-a.1", base64Message},
		"an empty reason": {"--allow", "no-base64", "--allow-why", " ", "--bead-channel", "mw-a.1", base64Message},
		"an unknown rule": {"--allow", "no-such", "--allow-why", "because", "--bead-channel", "mw-a.1", base64Message},
	} {
		plaintexts, bd, out, err := channelSend(t, args...)
		if err == nil || len(plaintexts) != 0 || strings.Contains(bd, "comment") {
			t.Errorf("%s: expected a refusal and nothing sent or written, got %v / %v / %s\n%s", name, err, plaintexts, bd, out)
		}
	}
	if _, _, _, err := channelSend(t, "--allow", "no-base64", "--allow-why", "because", base64Message); err == nil || !strings.Contains(err.Error(), "name the bead the reason is written on") {
		t.Errorf("expected the refusal to say a bead must be named, got %v", err)
	}
	if _, _, _, err := channelSend(t, "--allow", "no-such", "--allow-why", "because", "--bead-channel", "mw-a.1", base64Message); err == nil || !strings.Contains(err.Error(), "windows-paste-one-line") {
		t.Errorf("expected an unknown rule refused with the table's names, got %v", err)
	}
}

// --long stays, an alias of --allow words, and needs no bead.
func TestPosternSendLongIsTheAliasOfAllowWords(t *testing.T) {
	plaintexts, bd, out, err := channelSend(t, "--long", nWords(150))
	if err != nil || len(plaintexts) != 1 || strings.Contains(bd, "ALLOWED") {
		t.Fatalf("expected --long to send with nothing written, got %v / %v / %s\n%s", err, plaintexts, bd, out)
	}
	plaintexts, bd, out, err = channelSend(t, "--allow", "words", "--allow-why", "a long report", "--bead-channel", "mw-a.1", nWords(150))
	if err != nil || len(plaintexts) != 1 || !strings.Contains(bd, "comment mw-a.1 ALLOWED words: a long report") {
		t.Fatalf("expected --allow words to send and write the reason, got %v / %v / %s\n%s", err, plaintexts, bd, out)
	}
}

// A card's options and recommendation are read by the code rules too.
func TestPosternSendChecksACardsOptions(t *testing.T) {
	withReviewQueue(t, 0, 8)
	plaintexts, _, _, err := channelSend(t, "--class", "decision-needed", "--bead", "mw-a.1", "--option", "A: run `git push origin main`", "--option", "B: no", "Ship it?")
	if err == nil || !strings.Contains(err.Error(), "code-in-one-fence") || len(plaintexts) != 0 {
		t.Fatalf("expected an inline command in an option refused, got %v / %v", err, plaintexts)
	}
}

// mw talk say is refused by the same table, and --allow sends it and writes
// the reason on the bead named with --bead.
func TestTalkSayAndHandsAddRunTheRules(t *testing.T) {
	t.Run("talk say", func(t *testing.T) {
		sent, bdLog := channelHome(t)
		out, err := runMw(t, "talk", "say", "--talk", "t1", "--turn", "1", base64Message)
		if err == nil || !strings.Contains(err.Error(), "no-base64") || len(*sent) != 0 {
			t.Fatalf("expected a say refused by no-base64 with nothing sent, got %v / %v\n%s", err, *sent, out)
		}
		if out, err := runMw(t, "talk", "say", "--talk", "t1", "--turn", "1", "--allow", "no-base64", "--allow-why", "a fixture", base64Message); err == nil || len(*sent) != 0 {
			t.Fatalf("expected --allow with no bead refused, got %v / %v\n%s", err, *sent, out)
		}
		out, err = runMw(t, "talk", "say", "--talk", "t1", "--turn", "1", "--allow", "no-base64", "--allow-why", "a fixture", "--bead", "mw-a.1", base64Message)
		if err != nil || len(*sent) != 1 {
			t.Fatalf("expected the allowed say sent, got %v / %v\n%s", err, *sent, out)
		}
		calls, _ := os.ReadFile(bdLog)
		if !strings.Contains(string(calls), "comment mw-a.1 ALLOWED no-base64: a fixture") {
			t.Errorf("expected the reason written on the bead, bd was asked:\n%s", calls)
		}
		// A long spoken answer is not stopped by the words row: that row is the phone's.
		if out, err := runMw(t, "talk", "say", "--talk", "t1", "--turn", "1", nWords(200)); err != nil {
			t.Errorf("expected a long spoken answer said, got %v\n%s", err, out)
		}
	})

	t.Run("hands add", func(t *testing.T) {
		posternHome(t, "http://unused", "", "")
		fakeHandsPush(t, nil)
		log := bdRecording(t, handsAddBead)
		add := func(step string, extra ...string) (string, error) {
			args := []string{"hands", "add", "mw-f758y.8", "--id", "linger", "--host", "desktop", "--as", "user"}
			return runMw(t, append(append(args, extra...), "--", step)...)
		}
		for name, step := range map[string]string{
			"base64":                    "echo W1Vu | base64 -d",
			"a multi-line Windows step": "wsl.exe --shutdown\nGet-ScheduledTask x",
		} {
			out, err := add(step)
			if err == nil {
				t.Errorf("%s: expected a refusal, got %q", name, out)
			}
		}
		if calls, _ := os.ReadFile(log); strings.Contains(string(calls), "kv set") {
			t.Fatalf("expected no step kept, got:\n%s", calls)
		}
		if out, err := add("loginctl enable-linger jwhite"); err != nil {
			t.Fatalf("expected a plain step added, got %v\n%s", err, out)
		}
		if out, err := add("wsl.exe --shutdown\nGet-ScheduledTask x", "--allow", "windows-paste-one-line", "--allow-why", "cannot be one line"); err != nil {
			t.Fatalf("expected the allowed step added, got %v\n%s", err, out)
		}
		calls, _ := os.ReadFile(log)
		if !strings.Contains(string(calls), "comment mw-f758y.8 ALLOWED windows-paste-one-line: cannot be one line") {
			t.Errorf("expected the reason written on the step's bead, bd was asked:\n%s", calls)
		}
	})
}

// The help is built from the table, so no rule can be left out of it.
func TestPosternSendHelpListsTheRules(t *testing.T) {
	for _, command := range [][]string{{"postern", "send"}, {"talk", "say"}, {"hands", "add"}} {
		out, err := runMw(t, append(command, "--help")...)
		if err != nil {
			t.Fatal(err)
		}
		surface := map[string]application.PasteSurface{"postern": application.PasteSurfaceSend, "talk": application.PasteSurfaceSay, "hands": application.PasteSurfaceHands}[command[0]]
		for _, r := range application.PasteRules {
			if !r.RunsOn(surface) {
				continue
			}
			if !strings.Contains(out, r.Name) || !strings.Contains(out, r.Reason) {
				t.Errorf("mw %s --help: expected the rule %s and its reason, got:\n%s", strings.Join(command, " "), r.Name, out)
			}
		}
		if !strings.Contains(out, "--allow-why") {
			t.Errorf("mw %s --help: expected --allow-why named", strings.Join(command, " "))
		}
	}
	// Every row appears in postern send's help.
	out, _ := runMw(t, "postern", "send", "--help")
	for _, r := range application.PasteRules {
		if !strings.Contains(out, r.Name) {
			t.Errorf("postern send --help leaves out %s", r.Name)
		}
	}
}
