package application_test

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// chainReplyFixture is a direct-channel send whose keys sign by echoing the
// payload, so a broadcast can be read back.
func chainReplyFixture() (*sendFixture, application.PosternSend) {
	f := newSendFixture()
	send := f.send("")
	send.Keys = echoKeys{stubPosternKeys{pubKey: sendMayorKey}}
	return f, send
}

// writeAttachment writes a small text file to attach.
func writeAttachment(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// chainCopy is the n-th broadcast payload, decoded.
func chainCopy(t *testing.T, f *sendFixture, n int) []byte {
	t.Helper()
	broadcasts := f.backend.Broadcasts()
	if len(broadcasts) <= n {
		t.Fatalf("expected at least %d broadcasts, got %d", n+1, len(broadcasts))
	}
	payload, err := hex.DecodeString(broadcasts[n])
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

// An answer to a post that came by chain goes on chain too, the direct copy
// as well, and the chain copy carries no summary.
func TestPosternSendReplyToAChainPostAlsoGoesOnChain(t *testing.T) {
	f, send := chainReplyFixture()

	txid, err := send.Run(context.Background(), application.PosternSendRequest{Text: "Done.", Re: "0f3a9c-chain-txid"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(txid, "direct:") {
		t.Fatalf("expected the direct id returned, got %q", txid)
	}
	if len(f.backend.Delivered()) != 1 {
		t.Fatalf("expected one direct delivery, got %d", len(f.backend.Delivered()))
	}
	if len(f.backend.Broadcasts()) != 1 {
		t.Fatalf("expected one broadcast, got %d", len(f.backend.Broadcasts()))
	}
	_, fields := clearOf(t, chainCopy(t, f, 0))
	if _, has := fields["summary"]; has {
		t.Errorf("expected no summary on the chain copy, got %s", fields["summary"])
	}
	if direct, _ := clearOf(t, f.backend.Delivered()[0]); direct.Summary != "Message" {
		t.Errorf("expected the direct copy to keep its summary, got %+v", direct)
	}
	if out := f.out.String(); !strings.Contains(out, txid) || !strings.Contains(out, "chain txid fake-txid-1") {
		t.Errorf("expected both txids printed, got %q", out)
	}
}

// An answer to a direct post, with the Governor's last record direct, goes
// direct only, and the chain is not so much as looked at.
func TestPosternSendReplyToADirectPostGoesDirectOnly(t *testing.T) {
	f, send := chainReplyFixture()
	if err := f.tracker.SetNote(context.Background(), application.TalkWaitChannelKey, application.PosternChannelDirect); err != nil {
		t.Fatal(err)
	}

	if _, err := send.Run(context.Background(), application.PosternSendRequest{Text: "Done.", Re: "direct:abc123"}); err != nil {
		t.Fatal(err)
	}
	if len(f.backend.Delivered()) != 1 || len(f.backend.Broadcasts()) != 0 || f.backend.ChainCalls() != 0 {
		t.Fatalf("expected one direct delivery and no chain traffic, got %d, %d and %d calls",
			len(f.backend.Delivered()), len(f.backend.Broadcasts()), f.backend.ChainCalls())
	}
}

// A send with no --re and no note is as it always was.
func TestPosternSendWithNothingToGoOnStaysDirect(t *testing.T) {
	f, send := chainReplyFixture()
	if _, err := send.Run(context.Background(), application.PosternSendRequest{Text: "Hello."}); err != nil {
		t.Fatal(err)
	}
	if len(f.backend.Broadcasts()) != 0 || f.backend.ChainCalls() != 0 {
		t.Fatal("expected nothing on the chain")
	}
}

// With no --re, the Governor's last record having come by chain is enough.
func TestPosternSendAfterAChainBornLastRecordAlsoGoesOnChain(t *testing.T) {
	f, send := chainReplyFixture()
	if err := f.tracker.SetNote(context.Background(), application.TalkWaitChannelKey, application.PosternChannelChain); err != nil {
		t.Fatal(err)
	}
	for _, re := range []string{"", "direct:abc123"} {
		if _, err := send.Run(context.Background(), application.PosternSendRequest{Text: "Done.", Re: re}); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.backend.Delivered()) != 2 || len(f.backend.Broadcasts()) != 2 {
		t.Fatalf("expected two sends direct and on chain, got %d and %d", len(f.backend.Delivered()), len(f.backend.Broadcasts()))
	}
}

// Chain forces the chain copy, whatever came before.
func TestPosternSendChainForcesTheChainCopy(t *testing.T) {
	f, send := chainReplyFixture()
	if _, err := send.Run(context.Background(), application.PosternSendRequest{Text: "Done.", Re: "direct:abc123", Chain: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.backend.Delivered()) != 1 || len(f.backend.Broadcasts()) != 1 {
		t.Fatalf("expected the direct copy and a chain copy, got %d and %d", len(f.backend.Delivered()), len(f.backend.Broadcasts()))
	}
}

// The float cap holds for the chain copy: asked for, an excess is an error
// naming the txid that did go direct; added by itself, it is only said.
func TestPosternSendChainCopyKeepsTheFloatCap(t *testing.T) {
	f, send := chainReplyFixture()
	f.backend.SetBalance("", 150000)

	txid, err := send.Run(context.Background(), application.PosternSendRequest{Text: "Done.", Chain: true})
	if err == nil || !strings.Contains(err.Error(), "50000") || !strings.Contains(err.Error(), "direct:") {
		t.Fatalf("expected the excess and the direct txid named, got %v", err)
	}
	if txid == "" || len(f.backend.Delivered()) != 1 || len(f.backend.Broadcasts()) != 0 {
		t.Fatalf("expected the direct copy sent and nothing broadcast, got %q, %d, %d", txid, len(f.backend.Delivered()), len(f.backend.Broadcasts()))
	}

	f.out.Reset()
	if _, err := send.Run(context.Background(), application.PosternSendRequest{Text: "Done.", Re: "chain-txid"}); err != nil {
		t.Fatalf("a chain copy added by itself must not fail the send: %v", err)
	}
	if len(f.backend.Delivered()) != 2 || len(f.backend.Broadcasts()) != 0 || !strings.Contains(f.out.String(), "chain: not sent:") {
		t.Fatalf("expected the direct copy, no broadcast and a note, got %d, %d, %q", len(f.backend.Delivered()), len(f.backend.Broadcasts()), f.out.String())
	}
}

// A chain copy added by itself that the chain refuses is only said.
func TestPosternSendAutomaticChainCopyThatFailsIsOnlySaid(t *testing.T) {
	f, send := chainReplyFixture()
	f.backend.ChainErr = errors.New("chain down")
	if _, err := send.Run(context.Background(), application.PosternSendRequest{Text: "Done.", Re: "chain-txid"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "chain: not sent: chain down") {
		t.Fatalf("expected the note, got %q", f.out.String())
	}
	if _, err := send.Run(context.Background(), application.PosternSendRequest{Text: "Done.", Chain: true}); err == nil || !strings.Contains(err.Error(), "chain down") {
		t.Fatalf("expected a forced chain copy to fail the send, got %v", err)
	}
}

// Several files are several transactions: forced, refused before anything is
// sent; added by itself, the chain copy is left out.
func TestPosternSendChainCopyOfSeveralFilesIsRefusedOrSkipped(t *testing.T) {
	f, send := chainReplyFixture()
	files := []string{writeAttachment(t, "a.txt", "one"), writeAttachment(t, "b.txt", "two")}
	if _, err := send.Run(context.Background(), application.PosternSendRequest{Text: "x", Attachments: files, Chain: true}); err == nil {
		t.Fatal("expected several files with --chain refused")
	}
	if len(f.backend.Delivered()) != 0 || len(f.backend.Uploaded()) != 0 {
		t.Fatal("expected nothing sent")
	}
	if _, err := send.Run(context.Background(), application.PosternSendRequest{Text: "x", Attachments: files, Re: "chain-txid"}); err != nil {
		t.Fatal(err)
	}
	if len(f.backend.Delivered()) != 2 || len(f.backend.Broadcasts()) != 0 {
		t.Fatalf("expected both files direct and nothing on chain, got %d and %d", len(f.backend.Delivered()), len(f.backend.Broadcasts()))
	}
}
