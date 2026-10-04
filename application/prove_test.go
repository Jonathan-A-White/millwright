package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

type proveFixture struct {
	queue *apptest.FakeStampQueue
	chain *apptest.FakeChainLookup
	out   bytes.Buffer
}

// aSentStamp queues stamp and sends it under txid.
func (f *proveFixture) aSentStamp(t *testing.T, stamp domain.Stamp, txid string) {
	t.Helper()
	if err := f.queue.Append(context.Background(), stamp); err != nil {
		t.Fatal(err)
	}
	if err := f.queue.MarkSent(context.Background(), stamp, txid, time.Date(2026, 10, 4, 9, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
}

func newProveFixture() *proveFixture {
	return &proveFixture{queue: apptest.NewFakeStampQueue(), chain: apptest.NewFakeChainLookup()}
}

func (f *proveFixture) prove(rig, commit string) error {
	return application.Prove{Stamps: f.queue, Chain: f.chain, Out: &f.out}.Run(context.Background(), rig, commit)
}

func TestProvePrintsTheTxidHeightTimePreimageCommitmentAndURL(t *testing.T) {
	f := newProveFixture()
	s := stampForTest()
	f.aSentStamp(t, s, "abc123txid")
	f.chain.Confirm("abc123txid", 1650123, time.Date(2026, 10, 4, 9, 7, 30, 0, time.UTC))

	if err := f.prove("millwright", s.Commit); err != nil {
		t.Fatalf("Prove: %v", err)
	}
	for _, want := range []string{
		"abc123txid",
		"1650123",
		"2026-10-04 09:07:30 UTC",
		"millwright",
		s.Commit,
		s.Commitment(),
		"https://test.whatsonchain.com/tx/abc123txid",
	} {
		if !strings.Contains(f.out.String(), want) {
			t.Errorf("the output lacks %q:\n%s", want, f.out.String())
		}
	}
}

func TestProveFindsTheStampByACommitPrefix(t *testing.T) {
	f := newProveFixture()
	s := stampForTest()
	f.aSentStamp(t, s, "abc123txid")
	f.chain.Confirm("abc123txid", 7, time.Date(2026, 10, 4, 9, 7, 30, 0, time.UTC))

	if err := f.prove("millwright", s.Commit[:7]); err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if !strings.Contains(f.out.String(), s.Commit) {
		t.Fatalf("the full commit is not printed:\n%s", f.out.String())
	}
}

func TestProveWithNoStampSaysSoAndFails(t *testing.T) {
	f := newProveFixture()
	f.aSentStamp(t, stampForTest(), "abc123txid")

	err := f.prove("millwright", "deadbeef")
	if err == nil || err.Error() != "no stamp for millwright deadbeef" {
		t.Fatalf("err = %v, want 'no stamp for millwright deadbeef'", err)
	}
	if f.chain.Calls() != 0 {
		t.Fatal("the chain was asked about a stamp there is not")
	}
}

func TestProveDoesNotMatchAnotherRigsCommit(t *testing.T) {
	f := newProveFixture()
	s := stampForTest()
	f.aSentStamp(t, s, "abc123txid")
	if err := f.prove("other-rig", s.Commit); err == nil {
		t.Fatal("a stamp of another rig was proved")
	}
}

func TestProveWithAnEmptyCommitIsRefusedNotMatchedToEverything(t *testing.T) {
	f := newProveFixture()
	f.aSentStamp(t, stampForTest(), "abc123txid")
	if err := f.prove("millwright", ""); err == nil {
		t.Fatal("an empty commit matched")
	}
}

func TestProveSaysAStampStillInTheMempoolHasNoBlockYet(t *testing.T) {
	f := newProveFixture()
	s := stampForTest()
	f.aSentStamp(t, s, "abc123txid")
	f.chain.Mempool("abc123txid")

	if err := f.prove("millwright", s.Commit); err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if !strings.Contains(f.out.String(), "in the mempool, no block yet") {
		t.Fatalf("the output lacks the mempool line:\n%s", f.out.String())
	}
	if !strings.Contains(f.out.String(), "abc123txid") {
		t.Fatalf("the output lacks the txid:\n%s", f.out.String())
	}
}

func TestProveSaysAStampTheChainDoesNotKnowYet(t *testing.T) {
	f := newProveFixture()
	s := stampForTest()
	f.aSentStamp(t, s, "abc123txid")

	if err := f.prove("millwright", s.Commit); err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if !strings.Contains(f.out.String(), "not known to WhatsOnChain") {
		t.Fatalf("the output lacks the unknown line:\n%s", f.out.String())
	}
}

func TestProveStillPrintsWhatItHoldsWhenTheLookupFails(t *testing.T) {
	f := newProveFixture()
	s := stampForTest()
	f.aSentStamp(t, s, "abc123txid")
	f.chain.Err = errors.New("connection refused")

	err := f.prove("millwright", s.Commit)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err = %v, want the lookup's failure", err)
	}
	if !strings.Contains(f.out.String(), "abc123txid") || !strings.Contains(f.out.String(), s.Commitment()) {
		t.Fatalf("what the stamp store holds was not printed:\n%s", f.out.String())
	}
}

func TestProveWithTheStoreUnreadableFails(t *testing.T) {
	f := newProveFixture()
	f.queue.Err = errors.New("disk gone")
	if err := f.prove("millwright", "abcdef"); err == nil || !strings.Contains(err.Error(), "disk gone") {
		t.Fatalf("err = %v", err)
	}
}
