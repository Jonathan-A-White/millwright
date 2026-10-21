package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// slipScript is a mill that answers each send with the next scripted answer:
// "ok" fits the example (confidence low), "slip" misses its expect, "bad" does
// not fit the schema. Each send has its own txid.
type slipScript struct {
	answers []string
	sent    int
}

func (s *slipScript) Run(_ context.Context, _ application.GristSendRequest) (application.GristSendReport, error) {
	s.sent++
	if len(s.answers) == 0 {
		return application.GristSendReport{}, fmt.Errorf("no send was scripted")
	}
	next := s.answers[0]
	s.answers = s.answers[1:]
	body := map[string]string{
		"ok":   `{"price": null, "confidence": "low"}`,
		"slip": `{"price": null, "confidence": "high"}`,
		"bad":  `{"price": "twelve", "confidence": "low"}`,
	}[next]
	return application.GristSendReport{Txid: fmt.Sprintf("direct:send%d", s.sent), Answer: &application.GristAnswer{
		Status: application.GristAnswered, Answer: json.RawMessage(body)}}, nil
}

// mw-gq6.664: a failing example is sent again twice before it holds; one that
// passes on a send again holds nothing and is said as flaky.
func TestAMissThatDoesNotRepeatIsFlakyAndHoldsNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		answers []string
		said    string
	}{
		"missed once":  {[]string{"slip", "ok", "ok"}, "flaky: lampas/tutor/hello missed 1 of 3"},
		"missed twice": {[]string{"slip", "slip", "ok"}, "flaky: lampas/tutor/hello missed 2 of 3"},
		"bad then ok":  {[]string{"bad", "ok", "ok"}, "flaky: lampas/tutor/hello missed 1 of 3"},
	} {
		t.Run(name, func(t *testing.T) {
			sender := &slipScript{answers: tc.answers}
			smoke, _ := smokeWith(sender)
			report, err := smoke.Run(context.Background(), "lampas", "")
			if err != nil {
				t.Fatal(err)
			}
			if sender.sent != 3 {
				t.Errorf("expected the failing example sent 3 times, got %d", sender.sent)
			}
			if report.Failed() || report.Held() {
				t.Fatalf("expected nothing failed or held, got %+v", report)
			}
			if !strings.Contains(report.Line(), tc.said) {
				t.Errorf("expected the line to say %q, got %q", tc.said, report.Line())
			}
			if len(report.Flakes) != 1 || report.Flakes[0].Txid != "direct:send1" {
				t.Errorf("expected one flake with the failing run's txid, got %+v", report.Flakes)
			}
		})
	}
}

// A miss on every send holds as it did.
func TestAMissOnEverySendHoldsAsBefore(t *testing.T) {
	sender := &slipScript{answers: []string{"slip", "slip", "slip"}}
	smoke, _ := smokeWith(sender)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if sender.sent != 3 {
		t.Errorf("expected 3 sends, got %d", sender.sent)
	}
	if !report.Held() || len(report.Failures) != 1 || len(report.Flakes) != 0 {
		t.Fatalf("expected one failure that holds and no flake, got %+v", report)
	}
	if want := "lampas/tutor/hello: the answer misses what the example expects: confidence: wanted"; !strings.HasPrefix(report.Failures[0], want) {
		t.Errorf("expected the old words %q, got %q", want, report.Failures[0])
	}
}

// An example that passes is sent once; a smoke that is not run sends nothing more.
func TestAPassingExampleAndANotRunSmokeAreSentOnce(t *testing.T) {
	sender := &slipScript{answers: []string{"ok"}}
	smoke, _ := smokeWith(sender)
	if _, err := smoke.Run(context.Background(), "lampas", ""); err != nil || sender.sent != 1 {
		t.Fatalf("expected one send, got %d %v", sender.sent, err)
	}
	refused := refusedWith(503, "down")
	notRun := &smokeScript{errs: []error{refused, refused}}
	smoke, _ = smokeWith(notRun)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil || notRun.sent != 2 || len(report.NotRun) != 1 || report.Failed() {
		t.Fatalf("expected the backend's one retry only and not run, got %d sends %+v %v", notRun.sent, report, err)
	}
}

// A send that cannot be made again leaves the first miss to hold.
func TestAMissWhoseResendCannotBeMadeHolds(t *testing.T) {
	sender := &slipScript{answers: []string{"slip"}}
	smoke, _ := smokeWith(sender)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Held() || len(report.Flakes) != 0 {
		t.Fatalf("expected the miss held, got %+v", report)
	}
}

// A flaky example mails the Mayor once, naming the example, the field, what
// was wanted and got, and the failing run's txid.
func TestAFlakyExampleMailsTheMayorOnce(t *testing.T) {
	ctx := context.Background()
	box := apptest.NewFakeMailbox()
	book := application.GristSmokeBook{Notes: apptest.NewFakeTracker(), Mailbox: box, Host: "laptop"}
	sender := &slipScript{answers: []string{"slip", "ok", "ok"}}
	smoke, _ := smokeWith(sender)
	report, err := smoke.Run(ctx, "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Record(ctx, "millwright", report); err != nil {
		t.Fatal(err)
	}
	inbox, err := box.Inbox(ctx, application.MayorMailbox)
	if err != nil || len(inbox) != 1 {
		t.Fatalf("expected one mail to the Mayor, got %v %v", inbox, err)
	}
	if inbox[0].Subject != "Smoke flake: lampas/tutor/hello" {
		t.Errorf("subject: %q", inbox[0].Subject)
	}
	for _, want := range []string{"confidence", "wanted", "high", "direct:send1", "missed 1 of 3"} {
		if !strings.Contains(inbox[0].Body, want) {
			t.Errorf("expected the body to hold %q, got %q", want, inbox[0].Body)
		}
	}
	records, _ := book.Records(ctx)
	if len(records) != 1 || records[0].Failed || strings.Join(records[0].Warnings, ";") != "flaky: lampas/tutor/hello missed 1 of 3" {
		t.Errorf("expected the record to warn flaky and not be failed, got %+v", records)
	}
}
