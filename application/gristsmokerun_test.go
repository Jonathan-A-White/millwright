package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// movesMain is a sender that, on its first send, lands a commit c2 on the
// app's main that rewrites the files in changes (path to text), as a fix does
// under a smoke that takes an hour.
type movesMain struct {
	inner   application.GristSmokeSender
	grinds  *apptest.FakeGrinds
	changes map[string]string
	moved   bool
}

func (m *movesMain) Run(ctx context.Context, req application.GristSendRequest) (application.GristSendReport, error) {
	if !m.moved {
		m.moved = true
		if len(m.changes) == 0 {
			m.grinds.SetRemoteFile("/rigs/lampas", "c2", "README.md", []byte("unrelated"))
		}
		for path, text := range m.changes {
			m.grinds.SetRemoteFile("/rigs/lampas", "c2", path, []byte(text))
		}
	}
	return m.inner.Run(ctx, req)
}

var smokeRunAt = time.Date(2026, 10, 26, 6, 32, 52, 0, time.UTC)

// flakeMailed runs a smoke whose first send misses and the others pass, with
// main moved by changes during the run, and returns the Mayor's one mail.
func flakeMailed(t *testing.T, changes map[string]string) application.Message {
	t.Helper()
	ctx := context.Background()
	smoke, _ := smokeWith(nil)
	grinds := smoke.Grinds.(*apptest.FakeGrinds)
	smoke.Send = &movesMain{inner: &slipScript{answers: []string{"slip", "ok", "ok"}}, grinds: grinds, changes: changes}
	smoke.Now = func() time.Time { return smokeRunAt }
	report, err := smoke.Run(ctx, "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	box := apptest.NewFakeMailbox()
	book := application.GristSmokeBook{Notes: apptest.NewFakeTracker(), Mailbox: box, Host: "laptop"}
	if err := book.Record(ctx, "millwright", report); err != nil {
		t.Fatal(err)
	}
	inbox, err := box.Inbox(ctx, application.MayorMailbox)
	if err != nil || len(inbox) != 1 {
		t.Fatalf("expected one mail to the Mayor, got %v %v", inbox, err)
	}
	return inbox[0]
}

// mw-gq6.807: the flake mail names the commit the grinds were read at and when
// the failing run went.
func TestAFlakeMailNamesTheCommitTheGrindsWereReadAtAndWhenTheRunWent(t *testing.T) {
	mail := flakeMailed(t, nil)
	for _, want := range []string{"read at c1", "2026-10-26 06:32:52Z"} {
		if !strings.Contains(mail.Body, want) {
			t.Errorf("expected the mail to hold %q, got %q", want, mail.Body)
		}
	}
}

// mw-gq6.807: a later commit of main that touches the example's file, or its
// kind's instructions, gets the one changed-after line; one that does not, none.
func TestAFlakeMailSaysWhenTheExamplesGrindChangedAfterTheRun(t *testing.T) {
	const line = "lampas/tutor/hello's grind changed after this run, at c2: check that run's commit before filing"
	for name, tc := range map[string]struct {
		changes map[string]string
		said    bool
	}{
		"the example's file":      {map[string]string{"grinds/examples/tutor/hello.json": `{"schemaVersion": "1", "request": {"note": "hello"}, "expect": {"confidence": "low"}}`}, true},
		"the kind's instructions": {map[string]string{"grinds/tutor.md": "Be careful."}, true},
		"another file":            {nil, false},
		"another example":         {map[string]string{"grinds/examples/tutor/other.json": `{"request": {}}`}, false},
	} {
		t.Run(name, func(t *testing.T) {
			body := flakeMailed(t, tc.changes).Body
			if got := strings.Count(body, line); (got == 1) != tc.said || got > 1 {
				t.Errorf("expected the changed line %v (once), counted %d in %q", tc.said, got, body)
			}
			if !tc.said && strings.Contains(body, "changed after this run") {
				t.Errorf("expected no changed line, got %q", body)
			}
		})
	}
}

// The main that did not move gets no changed line.
func TestAFlakeMailOfAMainThatDidNotMoveHasNoChangedLine(t *testing.T) {
	ctx := context.Background()
	smoke, _ := smokeWith(&slipScript{answers: []string{"slip", "ok", "ok"}})
	smoke.Now = func() time.Time { return smokeRunAt }
	report, err := smoke.Run(ctx, "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	box := apptest.NewFakeMailbox()
	book := application.GristSmokeBook{Notes: apptest.NewFakeTracker(), Mailbox: box, Host: "laptop"}
	if err := book.Record(ctx, "millwright", report); err != nil {
		t.Fatal(err)
	}
	inbox, _ := box.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 || strings.Contains(inbox[0].Body, "changed after") || !strings.Contains(inbox[0].Body, "read at c1") {
		t.Fatalf("expected the commit and no changed line, got %+v", inbox)
	}
}

// The note added to the bead that names a flaky example says the same.
func TestTheNoteOnTheBeadNamingAFlakeNamesTheCommitAndTime(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddStory("mw-5r3p30", domain.Story{ID: "mw-5r3p30.722", Title: "A bug the smoke found"})
	if err := tracker.SetStoryMetadata(ctx, "mw-5r3p30.722", map[string]string{application.SmokeExamplesField: "lampas/tutor/hello"}); err != nil {
		t.Fatal(err)
	}
	smoke, _ := smokeWith(nil)
	grinds := smoke.Grinds.(*apptest.FakeGrinds)
	smoke.Send = &movesMain{inner: &slipScript{answers: []string{"slip", "ok", "ok"}}, grinds: grinds, changes: map[string]string{"grinds/tutor.md": "Be careful."}}
	smoke.Now = func() time.Time { return smokeRunAt }
	report, err := smoke.Run(ctx, "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	book := application.GristSmokeBook{Notes: tracker, Filed: tracker, Comments: tracker, Mailbox: apptest.NewFakeMailbox(), Host: "laptop"}
	if err := book.Record(ctx, "millwright", report); err != nil {
		t.Fatal(err)
	}
	comments := tracker.Comments("mw-5r3p30.722")
	if len(comments) != 1 {
		t.Fatalf("expected one note, got %q", comments)
	}
	for _, want := range []string{"read at c1", "2026-10-26 06:32:52Z", "changed after this run, at c2"} {
		if !strings.Contains(comments[0], want) {
			t.Errorf("expected the note to hold %q, got %q", want, comments[0])
		}
	}
}

// Sweep: a miss that holds, and a send not run, say their commit and time in
// the report's line and in mw status's, and the failure note on a bead that
// names it.
func TestAMissThatHoldsAndASendNotRunNameTheirCommitAndTime(t *testing.T) {
	ctx := context.Background()
	smoke, _ := smokeWith(nil)
	grinds := smoke.Grinds.(*apptest.FakeGrinds)
	smoke.Send = &movesMain{inner: &slipScript{answers: []string{"slip", "slip", "slip"}}, grinds: grinds, changes: map[string]string{"grinds/tutor.md": "Be careful."}}
	smoke.Now = func() time.Time { return smokeRunAt }
	report, err := smoke.Run(ctx, "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	tracker := apptest.NewFakeTracker()
	book := application.GristSmokeBook{Notes: tracker, Host: "laptop", Now: func() time.Time { return smokeRunAt }}
	if err := book.Record(ctx, "millwright", report); err != nil {
		t.Fatal(err)
	}
	records, _ := book.Records(ctx)
	for name, line := range map[string]string{"the report": report.Line(), "mw status": records[0].Line()} {
		for _, want := range []string{"lampas/tutor/hello: the answer misses", "run went at 2026-10-26 06:32:52Z on the grinds read at c1", "changed after this run, at c2"} {
			if !strings.Contains(line, want) {
				t.Errorf("%s: expected %q in %q", name, want, line)
			}
		}
	}

	refused := refusedWith(503, "down")
	notRun, _ := smokeWith(&smokeScript{errs: []error{refused, refused}})
	notRun.Now = func() time.Time { return smokeRunAt }
	report, err = notRun.Run(ctx, "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if line := report.Line(); !strings.Contains(line, "the first send went at 2026-10-26 06:32:52Z on the grinds read at c1") {
		t.Errorf("expected the not-run line to name its time and commit, got %q", line)
	}

	// A bead that names the failure gets it with the same words as a dated note.
	tracker.AddStory("mw-5r3p30", domain.Story{ID: "mw-5r3p30.9", Title: "Bug"})
	if err := tracker.SetStoryMetadata(ctx, "mw-5r3p30.9", map[string]string{application.SmokeExamplesField: "lampas/tutor/hello"}); err != nil {
		t.Fatal(err)
	}
	named := application.GristSmokeBook{Notes: tracker, Filed: tracker, Comments: tracker, Host: "laptop", Now: func() time.Time { return smokeRunAt }}
	smoke2, _ := smokeWith(nil)
	smoke2.Send = &movesMain{inner: &slipScript{answers: []string{"slip", "slip", "slip"}}, grinds: smoke2.Grinds.(*apptest.FakeGrinds)}
	smoke2.Now = func() time.Time { return smokeRunAt }
	held, err := smoke2.Run(ctx, "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := named.Record(ctx, "", held); err != nil {
		t.Fatal(err)
	}
	comments := tracker.Comments("mw-5r3p30.9")
	if len(comments) != 1 || !strings.Contains(comments[0], "run went at 2026-10-26 06:32:52Z on the grinds read at c1") || strings.Contains(comments[0], "changed after") {
		t.Errorf("expected the note to name the run and no change, got %q", comments)
	}
}
