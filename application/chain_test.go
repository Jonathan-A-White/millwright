package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// directOnlyBackend has the direct backend's methods and no coin method: it
// is a Postern all the same, the chain being the Chain port's.
type directOnlyBackend struct{}

func (directOnlyBackend) Messages(context.Context, int64) ([]application.PosternRecord, error) {
	return nil, nil
}
func (directOnlyBackend) Deliver(context.Context, []byte) (string, error) { return "", nil }
func (directOnlyBackend) Blob(context.Context, string) ([]byte, error)    { return nil, nil }
func (directOnlyBackend) UploadBlob(context.Context, []byte) (string, int64, error) {
	return "", 0, nil
}
func (directOnlyBackend) DeleteBlob(context.Context, string) error { return nil }
func (directOnlyBackend) Me(context.Context) (application.PosternMe, error) {
	return application.PosternMe{}, nil
}

var _ application.Postern = directOnlyBackend{}

// recordingChain is a Chain that keeps every payload it is asked to send and
// answers a txid of its own for each; its balance is whatever is set.
type recordingChain struct {
	sent    [][]byte
	balance int64
}

var _ application.Chain = (*recordingChain)(nil)

func (c *recordingChain) Send(_ context.Context, payload []byte) (string, error) {
	c.sent = append(c.sent, append([]byte(nil), payload...))
	return fmt.Sprintf("chain-txid-%d", len(c.sent)), nil
}

func (c *recordingChain) Balance(context.Context) (int64, error) { return c.balance, nil }

func (c *recordingChain) Tx(context.Context, string) (application.ChainTx, error) {
	return application.ChainTx{}, fmt.Errorf("recordingChain looks nothing up")
}

func chainSend(chain application.Chain, backend application.Postern, channel string, out *bytes.Buffer) application.PosternSend {
	cipher := apptest.NewFakeCipher()
	cipher.From = sendMayorKey
	return application.PosternSend{
		Postern: backend, Chain: chain, Cipher: cipher, Keys: stubPosternKeys{pubKey: sendMayorKey},
		GovernorKey: releaseTapGovernorKey, FloatSats: 100000, Channel: channel,
		Now: func() time.Time { return time.Unix(1758700000, 0) }, Out: out,
	}
}

// A send on the chain channel is one Send of the record's payload, and the
// txid the chain answers is the one printed.
func TestASendOnTheChainChannelIsOneSendOfTheRecord(t *testing.T) {
	chain := &recordingChain{balance: 5000}
	var out bytes.Buffer
	txid, err := chainSend(chain, directOnlyBackend{}, application.PosternChannelChain, &out).
		Run(context.Background(), application.PosternSendRequest{Text: "Ready for review."})
	if err != nil {
		t.Fatalf("sending: %v", err)
	}
	if len(chain.sent) != 1 {
		t.Fatalf("expected Send called once, got %d", len(chain.sent))
	}
	if txid != "chain-txid-1" || strings.TrimSpace(out.String()) != "chain-txid-1" {
		t.Fatalf("expected the chain's txid reported and printed, got %q and %q", txid, out.String())
	}
	var payload application.PosternPayload
	if err := json.Unmarshal(chain.sent[0], &payload); err != nil {
		t.Fatalf("the payload sent is not a record: %v", err)
	}
	text, _, err := apptest.NewFakeCipher().Decrypt("governor-private-key", payload.Ct)
	if err != nil {
		t.Fatalf("decrypting the record: %v", err)
	}
	if payload.Kind != "msg" || payload.Class != "message" || payload.To != releaseTapGovernorKey ||
		payload.From != sendMayorKey || payload.Ts != 1758700000 || payload.Summary != "" || text != "Ready for review." {
		t.Fatalf("expected the message's record, no summary, got %+v with %q", payload, text)
	}
}

// A chain copy asked for while the balance is over the float cap is refused
// in today's words, after the direct copy went, and the chain is never sent
// to.
func TestAChainCopyOverTheFloatCapIsRefusedAndNeverSent(t *testing.T) {
	chain := &recordingChain{balance: 150000}
	backend := apptest.NewFakePostern()
	var out bytes.Buffer
	txid, err := chainSend(chain, backend, application.PosternChannelDirect, &out).
		Run(context.Background(), application.PosternSendRequest{Text: "Done.", Chain: true})
	want := "mw postern send: sent direct as " + txid + ", but the chain broadcast failed: " +
		"mw postern send: the postern key's balance is 150000 satoshis, over the float cap of 100000 by 50000: " +
		"it refuses to send until the balance is back under the cap"
	if err == nil || err.Error() != want {
		t.Fatalf("expected\n%s\ngot\n%v", want, err)
	}
	if len(chain.sent) != 0 {
		t.Fatalf("expected Send never called, got %d", len(chain.sent))
	}
	if len(backend.Delivered()) != 1 || !strings.HasPrefix(txid, "direct:") {
		t.Fatalf("expected the direct copy delivered, got %d and %q", len(backend.Delivered()), txid)
	}
}

// A stamp goes on chain through Send: its sealed record is the payload, and
// the txid Send answers is the one the stamp is recorded as sent under.
func TestAStampIsBroadcastThroughSend(t *testing.T) {
	chain := &recordingChain{}
	queue := apptest.NewFakeStampQueue()
	stamp := stampFor("", "aaaa")
	queue.Append(context.Background(), stamp)
	cipher := apptest.NewFakeCipher()
	cipher.From = stampTestSender
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	var said strings.Builder

	err := application.ChainStamp{
		Queue: queue, Chain: chain, Keys: &stampKeys{}, Cipher: cipher, Tracker: apptest.NewFakeTracker(),
		GovernorKey: stampTestKey, Now: func() time.Time { return now }, Err: &said,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v (said %q)", err, said.String())
	}
	_, raw, err := application.SealStamp(cipher, stampTestKey, stampTestSender, stamp, now)
	if err != nil {
		t.Fatalf("sealing: %v", err)
	}
	if len(chain.sent) != 1 || !bytes.Equal(chain.sent[0], raw) {
		t.Fatalf("expected one Send of the sealed stamp, got %q", chain.sent)
	}
	sent := queue.Sent()
	if len(sent) != 1 || sent[0].Txid != "chain-txid-1" || sent[0].Stamp.Commit != stamp.Commit {
		t.Fatalf("expected the stamp sent under the chain's txid, got %+v", sent)
	}
}
