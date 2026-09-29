package postern_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// The grist vectors round-trip through mw grist send's sealing: the grist and
// its photo that GristSend seals as Cairn's phone, with the real cipher, open
// with the mill key to the vectors' own plaintext; and the mill's answer in
// each status, sealed back to the phone, opens as GristSend's result.
func TestGristSendRoundTripsTheGristVectors(t *testing.T) {
	raw, err := os.ReadFile("../../application/testdata/grist-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Grist   application.GristPlaintext         `json:"grist"`
		Answers map[string]application.GristAnswer `json:"answers"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	millKeys, millPub := vectorKeyFile(t, "mill")
	phoneKeys, phonePub := vectorKeyFile(t, "cairnPhone")

	dir := t.TempDir()
	request := filepath.Join(dir, "request.json")
	if err := os.WriteFile(request, vectors.Grist.Input, 0o600); err != nil {
		t.Fatal(err)
	}
	photoBytes := []byte("the bytes of a jpeg")
	photo := filepath.Join(dir, "drawer.jpg")
	if err := os.WriteFile(photo, photoBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	txid := vectors.Answers["answered"].Re
	backend := apptest.NewFakePostern()
	backend.NextTxid = txid
	backend.Mine = application.PosternMe{Pubkey: phonePub, Mill: millPub, Network: "testnet"}
	send := application.GristSend{
		Postern: backend, Cipher: postern.NewCipher(phoneKeys), Keys: phoneKeys,
		Now: func() time.Time { return time.Unix(1_790_000_000, 0) },
	}
	report, err := send.Run(context.Background(), application.GristSendRequest{
		App: vectors.Grist.Grist.App, Kind: vectors.Grist.Grist.Kind, RequestFile: request, Photos: []string{photo},
	})
	if err != nil || report.Txid != txid {
		t.Fatalf("expected the grist sent as %s, got %q: %v", txid, report.Txid, err)
	}

	millWIF, _ := millKeys.PrivateKeyWIF()
	millCipher := postern.NewCipher(millKeys)

	// The photo, sealed to the mill key, opens to the photo.
	if len(backend.Uploaded()) != 1 {
		t.Fatalf("expected one blob uploaded, got %d", len(backend.Uploaded()))
	}
	blob := backend.Uploaded()[0]
	opened, from, err := millCipher.Decrypt(millWIF, base64.StdEncoding.EncodeToString(blob))
	if err != nil || from != phonePub || opened != string(photoBytes) {
		t.Fatalf("expected the photo opened by the mill, from the phone, got %q from %s: %v", opened, from, err)
	}

	// The grist opens to the vectors' plaintext, naming that blob.
	if len(backend.Delivered()) != 1 {
		t.Fatalf("expected one grist posted, got %d", len(backend.Delivered()))
	}
	var payload application.PosternPayload
	if err := json.Unmarshal(backend.Delivered()[0], &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Class != application.GristClass || payload.To != millPub || payload.From != phonePub {
		t.Fatalf("expected a grist record from the phone to the mill, got %+v", payload)
	}
	text, from, err := millCipher.Decrypt(millWIF, payload.Ct)
	if err != nil || from != phonePub {
		t.Fatalf("expected the grist opened by the mill, from the phone, got from %s: %v", from, err)
	}
	var plain application.GristPlaintext
	if err := json.Unmarshal([]byte(text), &plain); err != nil {
		t.Fatal(err)
	}
	var wantInput, gotInput bytes.Buffer
	if err := json.Compact(&wantInput, vectors.Grist.Input); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&gotInput, plain.Input); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(blob)
	wantAttachments := []application.PosternAttachment{{Hash: hex.EncodeToString(sum[:]), Size: int64(len(blob)), Mime: "image/jpeg"}}
	if plain.Grist != vectors.Grist.Grist || gotInput.String() != wantInput.String() ||
		len(plain.Attachments) != 1 || plain.Attachments[0] != wantAttachments[0] {
		t.Fatalf("the grist does not open to the vectors' plaintext:\n got %s\nwant %+v %s %+v", text, vectors.Grist.Grist, wantInput.String(), wantAttachments)
	}

	// The mill's answer in each status, sealed to the phone, is what a waiting send reports.
	for status, exit := range map[string]int{application.GristAnswered: 0, application.GristRefused: 1, application.GristFailed: 1} {
		t.Run(status, func(t *testing.T) {
			answer := vectors.Answers[status]
			body, err := json.Marshal(answer)
			if err != nil {
				t.Fatal(err)
			}
			sealed, err := millCipher.Encrypt(phonePub, string(body))
			if err != nil {
				t.Fatal(err)
			}
			waiting := apptest.NewFakePostern()
			waiting.NextTxid = txid
			waiting.Mine = backend.Mine
			waiting.AddRecord(application.PosternRecord{
				Txid: "direct:answer", Class: application.GristClass, From: millPub, To: phonePub, Signer: millPub, Ciphertext: sealed,
			})
			var out bytes.Buffer
			wait := send
			wait.Postern, wait.Out = waiting, &out
			report, err := wait.Run(context.Background(), application.GristSendRequest{
				App: "cairn", Kind: "sweep", RequestFile: request, Photos: []string{photo}, Wait: time.Minute,
			})
			if application.ExitStatus(err) != exit {
				t.Fatalf("expected %s to leave with %d, got %d: %v", status, exit, application.ExitStatus(err), err)
			}
			if report.Answer == nil || report.Answer.Status != status || report.Answer.Reason != answer.Reason {
				t.Fatalf("expected the %s answer, got %+v", status, report.Answer)
			}
			var printed application.GristAnswer
			if err := json.Unmarshal(out.Bytes(), &printed); err != nil || printed.Status != status || printed.Re != txid {
				t.Fatalf("expected the answer printed as JSON, got %q: %v", out.String(), err)
			}
		})
	}
}
