package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// aGristSend is a GristSend over a fake backend that names a mill, and a
// request file and a photo it can send.
func aGristSend(t *testing.T) (application.GristSend, *apptest.FakePostern, application.GristSendRequest) {
	t.Helper()
	dir := t.TempDir()
	request := filepath.Join(dir, "request.json")
	photo := filepath.Join(dir, "drawer.png")
	for path, body := range map[string]string{request: `{"schemaVersion":"1.1"}`, photo: "png"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	backend := apptest.NewFakePostern()
	backend.Mine = application.PosternMe{Mill: "03mill"}
	send := application.GristSend{Postern: backend, Cipher: &apptest.FakeCipher{From: "034f"}, Keys: &fakeMillKey{}}
	return send, backend, application.GristSendRequest{App: "cairn", Kind: "sweep", RequestFile: request, Photos: []string{photo}}
}

// A request that cannot be sent is refused before anything is uploaded or
// posted.
func TestGristSendRefusesWhatCannotBeSentBeforeSendingAnything(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(t *testing.T, r *application.GristSendRequest, b *apptest.FakePostern)
		want   string
	}{
		"a photo of a type a grist does not carry": {func(t *testing.T, r *application.GristSendRequest, _ *apptest.FakePostern) {
			gif := filepath.Join(t.TempDir(), "a.gif")
			os.WriteFile(gif, []byte("gif"), 0o600)
			r.Photos = append(r.Photos, gif)
		}, "is not a photo a grist carries"},
		"a fifth photo": {func(_ *testing.T, r *application.GristSendRequest, _ *apptest.FakePostern) {
			r.Photos = append(r.Photos, r.Photos[0], r.Photos[0], r.Photos[0], r.Photos[0])
		}, "over the 4 a grist may carry"},
		"an app that is not a name": {func(_ *testing.T, r *application.GristSendRequest, _ *apptest.FakePostern) {
			r.App = "Cairn App"
		}, "must each be lower-case"},
		"a request that is not JSON": {func(t *testing.T, r *application.GristSendRequest, _ *apptest.FakePostern) {
			os.WriteFile(r.RequestFile, []byte("sweep the drawer"), 0o600)
		}, "is not a JSON object"},
		"a request naming no schema version": {func(t *testing.T, r *application.GristSendRequest, _ *apptest.FakePostern) {
			os.WriteFile(r.RequestFile, []byte(`{"requestType":"sweep"}`), 0o600)
		}, "give --schema-version"},
		"a backend with no mill": {func(_ *testing.T, _ *application.GristSendRequest, b *apptest.FakePostern) {
			b.Mine.Mill = ""
		}, "names no mill key"},
	} {
		t.Run(name, func(t *testing.T) {
			send, backend, req := aGristSend(t)
			tc.change(t, &req, backend)
			_, err := send.Run(context.Background(), req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected a refusal saying %q, got %v", tc.want, err)
			}
			if len(backend.Uploaded()) != 0 || len(backend.Delivered()) != 0 {
				t.Fatalf("expected nothing sent, got %d blobs and %d grist", len(backend.Uploaded()), len(backend.Delivered()))
			}
		})
	}
}

// --schema-version says the version when the request does not.
func TestGristSendTakesTheVersionFromTheFlagBeforeTheRequest(t *testing.T) {
	send, backend, req := aGristSend(t)
	os.WriteFile(req.RequestFile, []byte(`{"requestType":"sweep"}`), 0o600)
	req.Version = "2.0"
	if _, err := send.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if got := string(backend.Delivered()[0]); !strings.Contains(got, `"ct"`) || len(backend.Delivered()) != 1 {
		t.Fatalf("expected one grist posted, got %q", got)
	}
}

// Only an answer the mill sealed to the sender, for this grist, is the
// answer: another grist's, another sender's, and one whose backend-proven
// signer is not the mill are all read past, and the wait times out.
func TestGristSendReadsPastAnswersThatAreNotItsOwn(t *testing.T) {
	send, backend, req := aGristSend(t)
	backend.NextTxid = "direct:mine"
	req.Wait = time.Minute
	now := time.Unix(1_790_000_000, 0)
	send.Now = func() time.Time { return now }
	send.Sleep = func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }

	mill := &apptest.FakeCipher{From: "03mill"}
	sealed := func(re string) string {
		ct, _ := mill.Encrypt("034f", `{"re":"`+re+`","status":"answered","answer":{}}`)
		return ct
	}
	forged := &apptest.FakeCipher{From: "03other"}
	forgedCt, _ := forged.Encrypt("034f", `{"re":"direct:mine","status":"answered","answer":{}}`)
	for _, r := range []application.PosternRecord{
		{Class: "grist", From: "03mill", To: "034f", Signer: "03mill", Ciphertext: sealed("direct:another")},
		{Class: "grist", From: "03mill", To: "034f", Signer: "03other", Ciphertext: sealed("direct:mine")},
		{Class: "grist", From: "03mill", To: "034f", Signer: "03mill", Ciphertext: forgedCt},
		{Class: "grist", From: "03mill", To: "03someone", Signer: "03mill", Ciphertext: sealed("direct:mine")},
		{Class: "message", From: "03mill", To: "034f", Signer: "03mill", Ciphertext: sealed("direct:mine")},
	} {
		backend.AddRecord(r)
	}

	_, err := send.Run(context.Background(), req)
	if unanswered, ok := application.GristUnansweredIn(err); !ok || unanswered.Answer != nil || application.ExitStatus(err) != 2 {
		t.Fatalf("expected no answer and status 2, got %v", err)
	}
}
