package application_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// wordsHash is the txid of a message of his that came with two pictures and
// words, as the inbox saves them: the pictures as <txid>-1.jpg and <txid>-2.jpg,
// the words once as <txid>.message.json (mw-gq6.1345).
const wordsHash = "ff89786870a6fe9e8c00758ed5653d72"

const wordsHeSent = "I don't see Greg Laurie sermons anymore"

func writePictureWithWords(t *testing.T, g application.SleepGather, inbox string) {
	t.Helper()
	now := g.Now()
	for _, name := range []string{"direct-" + wordsHash + "-1.jpg", "direct-" + wordsHash + "-2.jpg"} {
		path := filepath.Join(inbox, name)
		if err := os.WriteFile(path, []byte("JPEG"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	kept, _ := json.Marshal(map[string]string{"txid": "direct:" + wordsHash, "text": wordsHeSent})
	if err := os.WriteFile(filepath.Join(inbox, "direct-"+wordsHash+".message.json"), kept, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSleepPictureWithWordsReadsAsWordsInEvidence(t *testing.T) {
	g, inbox := evidenceGather(t)
	writePictureWithWords(t, g, inbox)
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	text := partsByFile(bundle)[application.SleepEvidenceFile].Text
	if want := "the words he sent with the picture: " + wordsHeSent; !strings.Contains(text, want) {
		t.Errorf("evidence.txt does not give the pictures their words, want %q:\n%s", want, text)
	}
	if strings.Contains(text, "direct:"+wordsHash+"-") {
		t.Errorf("evidence.txt makes a message of each picture, under a txid that is not the message's:\n%s", text)
	}
	if n := strings.Count(text, "## direct:"+wordsHash+","); n != 1 {
		t.Errorf("the message is in evidence.txt %d times, want once:\n%s", n, text)
	}
}

func TestSleepPictureWithWordsKeepsNoWordlessClaimForIt(t *testing.T) {
	g, inbox := evidenceGather(t)
	writePictureWithWords(t, g, inbox)
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	f := newSleepFixture()
	f.gatherer.bundle = bundle
	f.pass.InboxDir = inbox
	claim := "# Questions\\n4. Greg Laurie: direct:" + wordsHash + " is wordless, he sent a picture and no reply. Restore the sermons?\\n" +
		"5. Keep Lampas polish in Later?\\n"
	f.grinder.Result.Answer = json.RawMessage(strings.Replace(fixtureAnswer, `# Questions\n1. Keep Lampas polish in Later?\n`, claim, 1))
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
		t.Fatal(err)
	}
	got := f.vault.files["sleep-2026-10-16/questions.md"]
	for _, bad := range []string{"wordless", "no reply"} {
		if strings.Contains(got, bad) {
			t.Errorf("questions.md keeps the claim %q for a message whose words came:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, wordsHeSent) || !strings.Contains(got, "Keep Lampas polish in Later?") {
		t.Errorf("questions.md lacks his words or the line that was right:\n%s", got)
	}
}

func TestSleepWordlessClaimOfAMessageWithNoWordsIsKept(t *testing.T) {
	g, inbox := evidenceGather(t)
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	f := newSleepFixture()
	f.gatherer.bundle = bundle
	f.pass.InboxDir = inbox
	claim := "# Questions\\n1. direct:bbb is wordless: only a picture. Keep it?\\n"
	f.grinder.Result.Answer = json.RawMessage(strings.Replace(fixtureAnswer, `# Questions\n1. Keep Lampas polish in Later?\n`, claim, 1))
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := f.vault.files["sleep-2026-10-16/questions.md"]; !strings.Contains(got, "direct:bbb is wordless") {
		t.Errorf("a true claim was dropped:\n%s", got)
	}
}
