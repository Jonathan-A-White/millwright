package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// keptForwards is a GristForwardStore on a temp directory: file-N.<ext> for
// each file kept, as the real store's picture-N.<ext>.
type keptForwards struct{ dir string }

func (k keptForwards) Keep(_ context.Context, txid string, files []application.GristRunAttachment) ([]string, error) {
	var paths []string
	for i, f := range files {
		path := filepath.Join(k.dir, application.GristRunName(txid), fmt.Sprintf("picture-%d%s", i+1, f.Ext))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// aCSVMill is a mill with one grist waiting from a licensed phone, carrying one
// file of mime and content, for the app's grind kind; the grind forwards to the
// Mayor when forward is set, and is read by a model otherwise.
func aCSVMill(t *testing.T, kind, forward, mime string, content []byte, ceilings application.GristCeilings) (application.GristGrind, *apptest.FakePostern, *apptest.FakeMailbox, keptForwards) {
	t.Helper()
	const phone = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
	keys := &fakeMillKey{made: true}
	mill, _, _ := keys.PublicKey()
	backend := apptest.NewFakePostern()
	sealer := &apptest.FakeCipher{From: phone}
	sealed, _ := sealer.EncryptBytes(mill, content)
	hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, sealed))
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "cairn", Kind: kind, V: "1"}, Input: json.RawMessage(`{"text":"my library"}`),
		Attachments: []application.PosternAttachment{{Hash: hash, Size: size, Mime: mime}},
	})
	ct, _ := sealer.Encrypt(mill, string(plain))
	backend.AddRecord(application.PosternRecord{Txid: "direct:c1", Class: application.GristClass, From: phone, To: mill,
		Signer: phone, SignerApps: []string{"cairn"}, Ciphertext: ct})

	grind := map[string]any{
		"grind": 1, "app": "cairn", "kind": kind, "versions": []string{"1"},
		"attachments": map[string]any{"min": 0, "max": 2, "mime": []string{"image/png", "text/csv"}, "maxBytes": 1 << 20},
	}
	grinds := apptest.NewFakeGrinds()
	if forward != "" {
		grind["forward"] = forward
	} else {
		grind["model"], grind["effort"] = "sonnet", "low"
		grind["instructions"], grind["answerSchema"] = "grinds/"+kind+".md", "schema.json"
		grinds.SetFile("/rigs/cairn", "c0ffee", "grinds/"+kind+".md", []byte("Read it."))
		grinds.SetFile("/rigs/cairn", "c0ffee", "schema.json", []byte(`{"type":"object","required":["items"]}`))
	}
	raw, _ := json.Marshal(grind)
	grinds.SetFile("/rigs/cairn", "c0ffee", "grinds/"+kind+".json", raw)
	mailbox := apptest.NewFakeMailbox()
	store := keptForwards{dir: t.TempDir()}
	return application.GristGrind{
		Postern: backend, Cipher: &apptest.FakeCipher{From: mill}, Keys: keys,
		State: apptest.NewFakeGristState(), Grinds: grinds, Grinder: &apptest.FakeGrinder{},
		Pass: &apptest.FakeGristLock{}, Grinding: &apptest.FakeGristLock{}, Host: "laptop",
		Apps: map[string]string{"cairn": "/rigs/cairn"}, TempDir: t.TempDir(), Ceilings: ceilings,
		Mailbox: mailbox, Forwards: store,
		Now: func() time.Time { return time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC) },
	}, backend, mailbox, store
}

func csvOf(size int) []byte {
	var b bytes.Buffer
	b.WriteString("title,author\n")
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, "Book %d,Author %d\n", i, i)
	}
	return b.Bytes()[:size]
}

// A text/csv file on a grist that forwards to the Mayor is accepted and
// forwarded: the mail names a .csv path, and the file there holds the bytes
// that were sent.
func TestAForwardingGristKeepsAndMailsATextCSV(t *testing.T) {
	content := csvOf(150 << 10)
	mill, backend, mailbox, store := aCSVMill(t, "feedback", application.GristForwardMayor, "text/csv", content, application.GristCeilings{MaxAttachmentBytes: 200 << 10})
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	delivered := backend.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("expected one answer, got %d", len(delivered))
	}
	if answer := openFakeAnswer(t, delivered[0]); answer.Status != application.GristAnswered {
		t.Fatalf("expected the grist answered, got %+v", answer)
	}
	mail, err := mailbox.Inbox(context.Background(), application.GristForwardMayor)
	if err != nil || len(mail) != 1 {
		t.Fatalf("expected one mail to the Mayor, got %d (%v)", len(mail), err)
	}
	var path string
	for _, line := range strings.Split(mail[0].Body, "\n") {
		if strings.HasPrefix(line, store.dir) {
			path = line
		}
	}
	if !strings.HasSuffix(path, ".csv") {
		t.Fatalf("expected the mail to name a .csv path under %s, got body:\n%s", store.dir, mail[0].Body)
	}
	if !strings.Contains(mail[0].Body, "Files: 1, kept on the mill's host") {
		t.Fatalf("expected the mail to count a file, not a picture, got body:\n%s", mail[0].Body)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("expected the kept file to hold the %d bytes sent, got %d bytes (%v)", len(content), len(got), err)
	}
}

// The same file on a grind a model reads is refused with its type named; and a
// text/csv over the attachment ceiling is refused, though the grind forwards.
func TestATextCSVIsRefusedWhereNoMailCarriesIt(t *testing.T) {
	for _, tc := range []struct {
		name, kind, forward string
		size                int
		ceilings            application.GristCeilings
		reason              func(string) bool
	}{
		{"a grind a model reads", "bible-talk", "", 150 << 10, application.GristCeilings{},
			func(r string) bool { return strings.Contains(r, "text/csv") }},
		{"over the attachment ceiling", "feedback", application.GristForwardMayor, 300 << 10, application.GristCeilings{MaxAttachmentBytes: 200 << 10},
			func(r string) bool { return r == application.GristReasonTooLarge }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mill, backend, mailbox, _ := aCSVMill(t, tc.kind, tc.forward, "text/csv", csvOf(tc.size), tc.ceilings)
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			delivered := backend.Delivered()
			if len(delivered) != 1 {
				t.Fatalf("expected one answer, got %d", len(delivered))
			}
			answer := openFakeAnswer(t, delivered[0])
			if answer.Status != application.GristRefused || !tc.reason(answer.Reason) {
				t.Fatalf("expected a refusal, got %+v", answer)
			}
			if mail, _ := mailbox.Inbox(context.Background(), application.GristForwardMayor); len(mail) != 0 {
				t.Fatalf("expected no mail, got %d", len(mail))
			}
		})
	}
}

// A text/csv is not a type any grind may take unless it forwards: a grind that
// forwards still refuses a type it does not list.
func TestAForwardingGristRefusesATextTypeItsGrindDoesNotList(t *testing.T) {
	mill, backend, _, _ := aCSVMill(t, "feedback", application.GristForwardMayor, "text/html", []byte("<p>x</p>"), application.GristCeilings{})
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	answer := openFakeAnswer(t, backend.Delivered()[0])
	if answer.Status != application.GristRefused || answer.Reason != application.GristReasonMime {
		t.Fatalf("expected the mime refusal, got %+v", answer)
	}
}
