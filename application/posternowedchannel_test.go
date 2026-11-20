package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// oursIn adds a record of ours as a direct send leaves it in the index: sealed
// to him, and naming its bead or channel in the clear (both empty for Factory).
func (f *owedFixture) oursIn(t *testing.T, txid string, ago time.Duration, bead, channel string) {
	t.Helper()
	ct, err := f.cipher.Encrypt(releaseTapGovernorKey, "whatever we said")
	if err != nil {
		t.Fatal(err)
	}
	f.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: sendMayorKey, To: releaseTapGovernorKey,
		Ts: owedNow.Add(-ago), Ciphertext: ct, Bead: bead, Channel: channel,
	})
}

// A post of ours in a bead's channel, or a named one, is no answer to his flat
// Factory post before it (mw-tdayrp7).
func TestPosternOwedChannelOursInAnotherChannelAnswersNothing(t *testing.T) {
	for name, in := range map[string][2]string{"a bead's channel": {"mw-a.1", ""}, "a named channel": {"", "ideas"}} {
		t.Run(name, func(t *testing.T) {
			f := newOwedFixture()
			f.his(t, owedHis1, "a flat post in Factory", "", 60*time.Minute)
			f.oursIn(t, "direct:ours1", 30*time.Minute, in[0], in[1])

			out, report := f.owed(t)
			if len(report.Owed) != 1 || report.Owed[0].Txid != owedHis1 || report.Owed[0].OnlyReceipt {
				t.Fatalf("expected his Factory post still owed with no reply, got %+v\n%s", report.Owed, out)
			}
		})
	}
}

// A post of ours in the same channel as his, or in Factory for a Factory post,
// still answers it.
func TestPosternOwedChannelOursInHisOwnChannelAnswersIt(t *testing.T) {
	f := newOwedFixture()
	f.his(t, owedHis1, "a flat post in Factory", "", 60*time.Minute)
	f.oursIn(t, "direct:ours1", 30*time.Minute, "", "")

	f.his(t, owedHis2, `{"thread":{"topic":"ideas"},"text":"in ideas"}`, "", 50*time.Minute)
	f.oursIn(t, "direct:ours2", 20*time.Minute, "", "ideas")

	if out, report := f.owed(t); len(report.Owed) != 0 {
		t.Fatalf("expected nothing owed, got %+v\n%s", report.Owed, out)
	}
}

// A reply in Factory does not answer his post in a named channel.
func TestPosternOwedChannelAFactoryReplyAnswersNotHisChannelPost(t *testing.T) {
	f := newOwedFixture()
	f.his(t, owedHis1, `{"thread":{"topic":"ideas"},"text":"in ideas"}`, "", 60*time.Minute)
	f.oursIn(t, "direct:ours1", 30*time.Minute, "", "")

	_, report := f.owed(t)
	if len(report.Owed) != 1 || report.Owed[0].Txid != owedHis1 {
		t.Fatalf("expected his ideas post owed, got %+v", report.Owed)
	}
}

// CheckAddressing refuses a flat send while his Factory post has no record of
// ours in Factory after it, whatever we said in another channel.
func TestPosternOwedChannelCheckAddressingSeesPastAPostElsewhere(t *testing.T) {
	f := newOwedFixture()
	f.his(t, owedHis1, "a flat post in Factory", "", 60*time.Minute)
	f.oursIn(t, "direct:ours1", 30*time.Minute, "mw-a.1", "")

	s := f.sender(owedNow)
	err := s.CheckAddressing(context.Background(), application.PosternSendRequest{Text: "hello"})
	if err == nil || !strings.Contains(err.Error(), owedHis1) {
		t.Fatalf("expected a refusal naming %s, got %v", owedHis1, err)
	}

	// A record of ours in Factory after it answers it.
	f.oursIn(t, "direct:ours2", 10*time.Minute, "", "")
	if err := f.sender(owedNow).CheckAddressing(context.Background(), application.PosternSendRequest{Text: "hello"}); err != nil {
		t.Fatalf("expected no refusal once answered in Factory, got %v", err)
	}
}
