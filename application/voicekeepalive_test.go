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
)

const voiceTestSecret = "abcd efgh ijkl mnop"

type voiceKeep struct {
	keep   *application.VoiceKeepAlive
	notes  *apptest.FakeTracker
	sender *apptest.FakePosternSender
	out    *bytes.Buffer
	mails  []application.VoiceMail
	now    time.Time
	creds  application.VoiceKeepAliveConfig
	credEr error
	sendEr error
	home   bool
}

func newVoiceKeep() *voiceKeep {
	v := &voiceKeep{
		notes: apptest.NewFakeTracker(), sender: &apptest.FakePosternSender{}, out: &bytes.Buffer{},
		now:   time.Date(2026, time.November, 20, 9, 0, 0, 0, time.UTC),
		creds: application.VoiceKeepAliveConfig{Account: "whiteplainsnac@gmail.com", AppPassword: voiceTestSecret, TextAddress: "19142698657.12345.abc@txt.voice.google.com"},
		home:  true,
	}
	v.keep = &application.VoiceKeepAlive{
		Notes: v.notes, Sender: v.sender, Out: v.out,
		Creds: func(context.Context) (application.VoiceKeepAliveConfig, error) { return v.creds, v.credEr },
		Send: func(_ context.Context, _ application.VoiceKeepAliveConfig, m application.VoiceMail) error {
			if v.sendEr != nil {
				return v.sendEr
			}
			v.mails = append(v.mails, m)
			return nil
		},
		Home: func(context.Context) (bool, error) { return v.home, nil },
		Now:  func() time.Time { return v.now },
	}
	return v
}

func TestVoiceKeepAliveDryRunPrintsTheEmailAndSendsNothing(t *testing.T) {
	v := newVoiceKeep()
	if err := v.keep.Run(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"From: whiteplainsnac@gmail.com", "To: 19142698657.12345.abc@txt.voice.google.com", "Testing"} {
		if !strings.Contains(v.out.String(), want) {
			t.Errorf("the dry run lacks %q:\n%s", want, v.out.String())
		}
	}
	if strings.Contains(v.out.String(), voiceTestSecret) {
		t.Errorf("the dry run printed the app password:\n%s", v.out.String())
	}
	if len(v.mails) != 0 || len(v.sender.Sent()) != 0 {
		t.Errorf("a dry run sent %d mails and %d alerts", len(v.mails), len(v.sender.Sent()))
	}
	// Nor does it count as the month's send.
	if err := v.keep.Run(context.Background(), false); err != nil || len(v.mails) != 1 {
		t.Errorf("the real run after a dry run: %v, %d mails", err, len(v.mails))
	}
}

func TestVoiceKeepAliveSendsTestingOnceAMonth(t *testing.T) {
	v := newVoiceKeep()
	ctx := context.Background()
	if err := v.keep.Run(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(v.mails) != 1 || v.mails[0].Body != "Testing" || v.mails[0].To != v.creds.TextAddress {
		t.Fatalf("expected one 'Testing' mail to the Voice address, got %+v", v.mails)
	}
	err := v.keep.Run(ctx, false)
	var skipped *application.SkippedPassError
	if !errors.As(err, &skipped) || len(v.mails) != 1 {
		t.Fatalf("a second run in the month should skip: %v, %d mails", err, len(v.mails))
	}
	v.now = time.Date(2026, time.December, 1, 9, 0, 0, 0, time.UTC)
	if err := v.keep.Run(ctx, false); err != nil || len(v.mails) != 2 {
		t.Fatalf("the next month sends again: %v, %d mails", err, len(v.mails))
	}
	if len(v.sender.Sent()) != 0 {
		t.Errorf("a good month alerted him: %+v", v.sender.Sent())
	}
}

func TestVoiceKeepAliveWithNoAppPasswordAlertsOnceAndSendsNothing(t *testing.T) {
	v := newVoiceKeep()
	v.credEr = application.ErrNoVoiceAppPassword
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = v.keep.Run(ctx, false)
	}
	sent := v.sender.Sent()
	if len(sent) != 1 || sent[0].Class != "alarm" || !strings.Contains(sent[0].Text, "no app password yet") {
		t.Fatalf("expected one alarm saying 'no app password yet', got %+v", sent)
	}
	if len(v.mails) != 0 {
		t.Errorf("sent %d mails with no password", len(v.mails))
	}
	// The file arrives mid-month: the send is not held back by the alert.
	v.credEr = nil
	if err := v.keep.Run(ctx, false); err != nil || len(v.mails) != 1 {
		t.Errorf("after the file came: %v, %d mails", err, len(v.mails))
	}
}

func TestVoiceKeepAliveWithNoTextAddressAlertsAndSaysSo(t *testing.T) {
	v := newVoiceKeep()
	v.creds.TextAddress = ""
	_ = v.keep.Run(context.Background(), false)
	sent := v.sender.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "no Voice text address") || len(v.mails) != 0 {
		t.Fatalf("expected one alarm about the address and no mail, got %+v, %d mails", sent, len(v.mails))
	}
}

func TestVoiceKeepAliveFailedSendAlertsOnceWithoutThePassword(t *testing.T) {
	v := newVoiceKeep()
	v.sendEr = errors.New("535 bad credentials for " + voiceTestSecret)
	ctx := context.Background()
	if err := v.keep.Run(ctx, false); err == nil {
		t.Fatal("a failed send should be an error")
	}
	_ = v.keep.Run(ctx, false)
	sent := v.sender.Sent()
	if len(sent) != 1 || sent[0].Class != "alarm" || !strings.Contains(sent[0].Text, "did not send") {
		t.Fatalf("expected one alarm about the failed send, got %+v", sent)
	}
	if strings.Contains(sent[0].Text, voiceTestSecret) || strings.Contains(sent[0].Summary, voiceTestSecret) {
		t.Errorf("the alert carries the app password: %+v", sent[0])
	}
	// It is tried again, and a good try counts for the month.
	v.sendEr = nil
	if err := v.keep.Run(ctx, false); err != nil || len(v.mails) != 1 {
		t.Errorf("the retry: %v, %d mails", err, len(v.mails))
	}
}

func TestVoiceKeepAliveSendsOnTheHomeOnly(t *testing.T) {
	v := newVoiceKeep()
	v.home = false
	err := v.keep.Run(context.Background(), false)
	var skipped *application.SkippedPassError
	if !errors.As(err, &skipped) || len(v.mails) != 0 || len(v.sender.Sent()) != 0 {
		t.Fatalf("off the home: %v, %d mails, %d alerts", err, len(v.mails), len(v.sender.Sent()))
	}
}

func TestVoiceKeepAliveEnvIsReadFromKeyValueLines(t *testing.T) {
	got, err := application.ParseVoiceKeepAliveEnv("# the church\nCHURCH_VOICE_APP_PASSWORD=\"" + voiceTestSecret + "\"\nCHURCH_VOICE_TEXT_ADDRESS=a@txt.voice.google.com\n")
	if err != nil || got.AppPassword != voiceTestSecret || got.TextAddress != "a@txt.voice.google.com" || got.Account != application.VoiceKeepAliveAccount {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := application.ParseVoiceKeepAliveEnv("CHURCH_VOICE_TEXT_ADDRESS=a@b\n"); !errors.Is(err, application.ErrNoVoiceAppPassword) {
		t.Errorf("a file with no password: %v", err)
	}
}

func TestVoiceKeepAliveJobRunsEveryFewHoursOnTheFollowersClock(t *testing.T) {
	job := application.VoiceKeepAliveJob(func(context.Context) error { return nil })
	if job.Name != "voice-keepalive" || job.Every <= 0 || job.Every > 24*time.Hour {
		t.Errorf("job %+v", job)
	}
}
