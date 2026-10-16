package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// backendRefusal is an error as the postern client's: the backend answered
// outside 2xx, and its status can be read from the error.
type backendRefusal struct {
	code int
	said string
}

func (e *backendRefusal) Error() string      { return e.said }
func (e *backendRefusal) PosternStatus() int { return e.code }

func refusedWith(code int, body string) error {
	return fmt.Errorf("mw grist send: %w", &backendRefusal{code: code,
		said: fmt.Sprintf("the postern backend at http://10.88.0.2:8787 said %d to GET /api/me: %s", code, body)})
}

// smokeScript is a sender that answers each send with the next error in its
// list; a nil entry is an answer that fits the example.
type smokeScript struct {
	errs []error
	sent int
}

func (s *smokeScript) Run(_ context.Context, _ application.GristSendRequest) (application.GristSendReport, error) {
	s.sent++
	if len(s.errs) == 0 {
		return application.GristSendReport{}, fmt.Errorf("no send was scripted")
	}
	next := s.errs[0]
	s.errs = s.errs[1:]
	if next != nil {
		return application.GristSendReport{}, next
	}
	return application.GristSendReport{Txid: "direct:smoke", Answer: &application.GristAnswer{
		Status: application.GristAnswered, Answer: json.RawMessage(`{"price": null, "confidence": "low"}`)}}, nil
}

func smokeWith(sender application.GristSmokeSender) (application.GristSmoke, *[]time.Duration) {
	grinds := apptest.NewFakeGrinds()
	const checkout, commit = "/rigs/lampas", "c1"
	grinds.SetFile(checkout, commit, "grinds/tutor.json", []byte(`{"grind": 1, "app": "lampas", "kind": "tutor", "versions": ["1"], "model": "sonnet", "effort": "low", "instructions": "grinds/tutor.md", "answerSchema": "schemas/tutor.json"}`))
	grinds.SetFile(checkout, commit, "schemas/tutor.json", []byte(`{"type": "object", "required": ["price", "confidence"], "properties": {"price": {"type": ["number", "null"]}, "confidence": {"enum": ["low", "medium", "high"]}}}`))
	grinds.SetFile(checkout, commit, "grinds/examples/tutor/hello.json", []byte(`{"schemaVersion": "1", "request": {"note": "hi"}, "expect": {"confidence": "low"}}`))
	var waits []time.Duration
	return application.GristSmoke{
		Grinds: grinds, Send: sender, Apps: map[string]string{"lampas": checkout}, TempDir: "",
		Sleep: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil },
	}, &waits
}

// mw-gq6.540: a send the backend refuses with 429, 502, 503 or 504 is the
// backend's fault: sent once more after a wait, and if that passes, so does the
// example.
func TestABackendRefusalOfASendIsRetriedOnceAndPassesWhenTheRetryDoes(t *testing.T) {
	for _, code := range []int{429, 502, 503, 504} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			sender := &smokeScript{errs: []error{refusedWith(code, "checking licence: WhatsOnChain said 429"), nil}}
			smoke, waits := smokeWith(sender)
			report, err := smoke.Run(context.Background(), "lampas", "")
			if err != nil {
				t.Fatal(err)
			}
			if sender.sent != 2 || len(*waits) != 1 {
				t.Errorf("expected two sends with one wait, got %d sends, waits %v", sender.sent, *waits)
			}
			if report.Failed() || len(report.NotRun) != 0 {
				t.Errorf("expected the example to pass, got %+v", report)
			}
		})
	}
}

// If the retry is refused too, the example is not run: said, and holding nothing.
func TestABackendRefusalThatRepeatsIsNotRunAndHoldsNothing(t *testing.T) {
	for _, code := range []int{429, 502, 503, 504} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			again := refusedWith(code, "checking licence: WhatsOnChain said 429")
			sender := &smokeScript{errs: []error{again, again}}
			smoke, _ := smokeWith(sender)
			report, err := smoke.Run(context.Background(), "lampas", "")
			if err != nil {
				t.Fatal(err)
			}
			if sender.sent != 2 {
				t.Errorf("expected the send retried once, got %d sends", sender.sent)
			}
			if report.Failed() || report.Held() {
				t.Errorf("expected nothing failed or held, got %+v", report)
			}
			if len(report.NotRun) != 1 || !strings.Contains(report.NotRun[0], fmt.Sprintf("refused it with %d", code)) {
				t.Errorf("expected the line to say the backend refused it with %d, got %v", code, report.NotRun)
			}
			if !strings.Contains(report.Line(), "not run") {
				t.Errorf("expected a not-run line, got %q", report.Line())
			}
		})
	}
}

// A refusal about the request itself still fails the example, at once.
func TestARefusalAboutTheRequestStillFailsTheExample(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			sender := &smokeScript{errs: []error{refusedWith(code, "no"), nil}}
			smoke, waits := smokeWith(sender)
			report, err := smoke.Run(context.Background(), "lampas", "")
			if err != nil {
				t.Fatal(err)
			}
			if sender.sent != 1 || len(*waits) != 0 {
				t.Errorf("expected one send and no wait, got %d sends, waits %v", sender.sent, *waits)
			}
			if !report.Failed() || !report.Held() || len(report.NotRun) != 0 {
				t.Errorf("expected a failure that holds, got %+v", report)
			}
			if !strings.Contains(report.Failures[0], "it could not be sent") {
				t.Errorf("expected the old words, got %v", report.Failures)
			}
		})
	}
}

// A backend that cannot be reached at all stays not run, as it was.
func TestAnUnreachableBackendStaysNotRun(t *testing.T) {
	sender := &smokeScript{errs: []error{fmt.Errorf("reaching it: %w", application.ErrPosternUnreachable)}}
	smoke, _ := smokeWith(sender)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() || len(report.NotRun) != 1 || sender.sent != 1 {
		t.Errorf("expected one send, not run, got %d sends, %+v", sender.sent, report)
	}
}

// A 500 whose body names an upstream 429 is the backend's too.
func TestAServerErrorNamingAnUpstream429IsTheBackendsFault(t *testing.T) {
	again := refusedWith(500, "checking licence: WhatsOnChain said 429")
	sender := &smokeScript{errs: []error{again, again}}
	smoke, _ := smokeWith(sender)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() || len(report.NotRun) != 1 || sender.sent != 2 {
		t.Errorf("expected two sends and not run, got %d sends, %+v", sender.sent, report)
	}
}
