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
	"github.com/Jonathan-A-White/millwright/domain"
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

// mw-gq6.696: a flake of an example an open bead already names says so in its
// mail and is added to that bead as a dated note, so the Mayor has nothing to
// file; an example no open bead names keeps the ask to file a bug.
func TestAFlakyExampleAnOpenBeadNamesIsNotedOnItAndMailedAsNamed(t *testing.T) {
	ctx := context.Background()
	box := apptest.NewFakeMailbox()
	tracker := apptest.NewFakeTracker()
	tracker.AddStory("mw-5r3p30", domain.Story{ID: "mw-5r3p30.722", Title: "A bug the smoke found"})
	if err := tracker.SetStoryMetadata(ctx, "mw-5r3p30.722", map[string]string{application.SmokeExamplesField: "lampas/tutor/hello"}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 22, 1, 56, 0, 0, time.UTC)
	book := application.GristSmokeBook{Notes: tracker, Filed: tracker, Comments: tracker, Mailbox: box, Host: "laptop", Now: func() time.Time { return at }}
	smoke, _ := smokeWith(&slipScript{answers: []string{"slip", "ok", "ok"}})
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
	if !strings.Contains(inbox[0].Body, "already named on mw-5r3p30.722, nothing to file") {
		t.Errorf("expected the mail to say the example is named, got %q", inbox[0].Body)
	}
	if strings.Contains(inbox[0].Body, "file it as a bug") || strings.Contains(inbox[0].Body, "--filed") {
		t.Errorf("expected no ask to file a bug, got %q", inbox[0].Body)
	}
	comments := tracker.Comments("mw-5r3p30.722")
	if len(comments) != 1 {
		t.Fatalf("expected one note on the named bead, got %q", comments)
	}
	for _, want := range []string{"2026-10-22", "lampas/tutor/hello", "missed 1 of 3", "direct:send1", "confidence"} {
		if !strings.Contains(comments[0], want) {
			t.Errorf("expected the note to hold %q, got %q", want, comments[0])
		}
	}
}

func TestAFlakyExampleNoOpenBeadNamesKeepsTheAskToFileABug(t *testing.T) {
	ctx := context.Background()
	box := apptest.NewFakeMailbox()
	tracker := apptest.NewFakeTracker()
	tracker.AddStory("mw-5r3p30", domain.Story{ID: "mw-5r3p30.1", Title: "Another bug"})
	if err := tracker.SetStoryMetadata(ctx, "mw-5r3p30.1", map[string]string{application.SmokeExamplesField: "lampas/tutor/other"}); err != nil {
		t.Fatal(err)
	}
	book := application.GristSmokeBook{Notes: tracker, Filed: tracker, Comments: tracker, Mailbox: box, Host: "laptop"}
	smoke, _ := smokeWith(&slipScript{answers: []string{"slip", "ok", "ok"}})
	report, err := smoke.Run(ctx, "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Record(ctx, "millwright", report); err != nil {
		t.Fatal(err)
	}
	inbox, _ := box.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 || !strings.Contains(inbox[0].Body, "file it as a bug") || strings.Contains(inbox[0].Body, "already named") {
		t.Fatalf("expected today's ask to file a bug, got %+v", inbox)
	}
	if got := tracker.Comments("mw-5r3p30.1"); len(got) != 0 {
		t.Errorf("expected no note on a bead that does not name the example, got %q", got)
	}
}

// mw-gq6.743: the line before each send again says what the send before it came
// to, so the lines add up to the summary's "missed M of S".
func TestEachResendLineSaysWhatThePreviousSendCameTo(t *testing.T) {
	sender := &slipScript{answers: []string{"slip", "ok", "slip"}}
	smoke, _ := smokeWith(sender)
	var out strings.Builder
	smoke.Out = &out
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  wrong; sending it again (1 of 2) ...\n",
		"  right; sending it again (2 of 2) ...\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected the output to say %q, got %q", want, out.String())
		}
	}
	if strings.Count(out.String(), "wrong;") != 1 {
		t.Errorf("expected only the first send called wrong, got %q", out.String())
	}
	if !strings.Contains(out.String(), "flaky: lampas/tutor/hello missed 2 of 3") {
		t.Errorf("expected the summary to say missed 2 of 3, got %q", out.String())
	}
	if len(report.Flakes) != 1 {
		t.Errorf("expected one flake, got %+v", report.Flakes)
	}
}

// mw-gq6.743: a miss on every send says wrong before each send again, as before.
func TestEveryResendLineSaysWrongWhenEverySendWasWrong(t *testing.T) {
	sender := &slipScript{answers: []string{"slip", "bad", "slip"}}
	smoke, _ := smokeWith(sender)
	var out strings.Builder
	smoke.Out = &out
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"  wrong; sending it again (1 of 2) ...\n", "  wrong; sending it again (2 of 2) ...\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected the output to say %q, got %q", want, out.String())
		}
	}
	if strings.Contains(out.String(), "right;") || !report.Held() || len(report.Flakes) != 0 {
		t.Errorf("expected nothing right and the miss held, got %q %+v", out.String(), report)
	}
}

// mw-gq6.806: an open mail, a molecule or a molecule's step whose title names the
// example is no filing of it (the landing mail of a story that fixed it names it
// too, and stays open until it is read); an open bug that names it still is.
func TestAnExampleOnlyAnOpenMailOrMoleculeNamesIsNotNamed(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, id, kind string
		named          bool
	}{
		{"an open mail", "mw-h2omel", "mail", false},
		{"a molecule", "mw-mol-aaaaaa", "molecule", false},
		{"a molecule's step", "mw-mol-bbbbbb", "task", false},
		{"an open bug", "mw-bug1", "bug", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracker := apptest.NewFakeTracker()
			tracker.AddStory("mw-5r3p30", domain.Story{ID: tc.id, Title: "Fix lampas/tutor/hello so it answers"})
			if err := tracker.SetType(tc.id, tc.kind); err != nil {
				t.Fatal(err)
			}
			named, err := tracker.FiledSmokeExamples(ctx, []string{"lampas/tutor/hello"})
			if err != nil {
				t.Fatal(err)
			}
			if _, got := named["lampas/tutor/hello"]; got != tc.named {
				t.Errorf("%s: named = %v, want %v (%v)", tc.name, got, tc.named, named)
			}
		})
	}
}

func TestAFlakyExampleOnlyAnOpenMailNamesKeepsTheAskToFileABug(t *testing.T) {
	ctx := context.Background()
	box := apptest.NewFakeMailbox()
	tracker := apptest.NewFakeTracker()
	tracker.AddStory("mw-5r3p30", domain.Story{ID: "mw-h2omel", Title: "Landed: lampas/tutor/hello answers again"})
	if err := tracker.SetType("mw-h2omel", "mail"); err != nil {
		t.Fatal(err)
	}
	book := application.GristSmokeBook{Notes: tracker, Filed: tracker, Comments: tracker, Mailbox: box, Host: "laptop"}
	smoke, _ := smokeWith(&slipScript{answers: []string{"slip", "ok", "ok"}})
	report, err := smoke.Run(ctx, "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Record(ctx, "millwright", report); err != nil {
		t.Fatal(err)
	}
	inbox, _ := box.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 || !strings.Contains(inbox[0].Body, "file it as a bug") || strings.Contains(inbox[0].Body, "already named") {
		t.Fatalf("expected the ask to file a bug, got %+v", inbox)
	}
	if got := tracker.Comments("mw-h2omel"); len(got) != 0 {
		t.Errorf("expected no note on the mail, got %q", got)
	}
}
