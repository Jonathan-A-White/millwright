package application_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// readReply reads one message, from sender, in thread and answering root,
// through the inbox with threads as its index.
func readReply(t *testing.T, threads *apptest.FakePosternThreadIndex, sender string, thread application.PosternThread, root string) {
	t.Helper()
	f := newReleaseTapFixture(t)
	f.cipher.From = sender
	ct, err := f.cipher.Encrypt(releaseTapInboxPubKey, threaded(t, thread, root))
	if err != nil {
		t.Fatal(err)
	}
	f.backend.AddRecord(application.PosternRecord{
		Txid: "direct:reply", Class: "message", From: sender, To: releaseTapInboxPubKey, Signer: sender, Ciphertext: ct,
	})
	inbox := f.inbox()
	inbox.Threads = threads
	if _, err := inbox.Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
}

// A reply's root, unseen on this host, is remembered in the reply's channel,
// so that the answer line's --re finds it (mw-gq6.435).
func TestPosternInboxRemembersAnUnseenRootInItsReplysChannel(t *testing.T) {
	threads := apptest.NewFakePosternThreadIndex()

	readReply(t, threads, releaseTapGovernorKey, application.PosternThread{Topic: "roadmap"}, "direct:root")

	got, found, err := threads.Lookup("direct:root")
	if err != nil || !found || got != (application.PosternThread{Topic: "roadmap"}) {
		t.Fatalf("expected the root remembered in channel roadmap, got %+v found %v err %v", got, found, err)
	}
}

// A root mw already knows keeps the channel it knows.
func TestPosternInboxKeepsAKnownRootsChannel(t *testing.T) {
	threads := apptest.NewFakePosternThreadIndex()
	if err := threads.Remember("direct:root", application.PosternThread{Bead: "mw-a.1"}); err != nil {
		t.Fatal(err)
	}

	readReply(t, threads, releaseTapGovernorKey, application.PosternThread{}, "direct:root")

	if got, _, _ := threads.Lookup("direct:root"); got != (application.PosternThread{Bead: "mw-a.1"}) {
		t.Fatalf("expected the root left in mw-a.1's channel, got %+v", got)
	}
}

// A reply that is not verified says nothing of where its root is.
func TestPosternInboxLearnsNoRootFromAnUnverifiedReply(t *testing.T) {
	threads := apptest.NewFakePosternThreadIndex()
	f := newReleaseTapFixture(t)
	f.cipher.From = releaseTapGovernorKey
	ct, err := f.cipher.Encrypt(releaseTapInboxPubKey, threaded(t, application.PosternThread{Topic: "roadmap"}, "direct:root"))
	if err != nil {
		t.Fatal(err)
	}
	// The payload claims another sender than the envelope's: not verified.
	f.backend.AddRecord(application.PosternRecord{
		Txid: "direct:reply", Class: "message", From: "someone-else", To: releaseTapInboxPubKey, Ciphertext: ct,
	})
	inbox := f.inbox()
	inbox.Threads = threads
	if _, err := inbox.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := threads.Lookup("direct:root"); found {
		t.Fatal("expected no channel remembered for the root of an unverified reply")
	}
	if _, found, _ := threads.Lookup("direct:reply"); !found {
		t.Fatal("expected the reply's own channel remembered, as before")
	}
}
