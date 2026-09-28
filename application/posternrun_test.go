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

// runNow is the clock every run test reads.
var runNow = time.Date(2026, 9, 28, 12, 10, 0, 0, time.UTC)

// runFixture is the apply fixture with hands steps on mw-e.3, a runner and a
// verifier, this host desktop and the laptop reached by ssh.
type runFixture struct {
	*applyFixture
	runner *apptest.FakeHandsRunner
	steps  map[string]domain.HandsStep
}

func newRunFixture(t *testing.T) *runFixture {
	t.Helper()
	f := &runFixture{
		applyFixture: newApplyFixture(t),
		runner:       &apptest.FakeHandsRunner{Outcome: application.HandsOutcome{Exit: 0, Output: "hello\n"}},
		steps: map[string]domain.HandsStep{
			"echo":    {ID: "echo", Host: "desktop", As: "user", Run: "echo hello", WayBack: ""},
			"linger":  {ID: "linger", Host: "desktop", As: "root", Run: "loginctl enable-linger jwhite", WayBack: "loginctl disable-linger jwhite"},
			"far":     {ID: "far", Host: "laptop", As: "user", Run: "systemctl --user restart mw-dispatch"},
			"nowhere": {ID: "nowhere", Host: "mars", As: "user", Run: "true"},
		},
	}
	var records []application.HandsStepRecord
	for _, id := range []string{"echo", "linger", "far", "nowhere"} {
		records = append(records, application.HandsStepRecord{HandsStep: f.steps[id], AddedAt: "2026-09-28T11:00:00Z"})
	}
	raw, _ := json.Marshal(records)
	mustDo(t, f.tracker.SetNote(context.Background(), application.HandsStepsKey("mw-e.3"), string(raw)))
	return f
}

func (f *runFixture) inbox() application.PosternInbox {
	inbox := f.applyFixture.inbox()
	inbox.Host = "desktop"
	inbox.HandsRunner = f.runner
	inbox.HandsVerifier = apptest.FakeHandsVerifier{}
	inbox.HandsHosts = map[string]string{"laptop": "ssh laptop"}
	inbox.Now = func() time.Time { return runNow }
	inbox.Sender = &application.PosternSend{
		Postern: f.backend, Cipher: f.cipher, Keys: stubPosternKeys{pubKey: applyInboxKey}, GovernorKey: releaseTapGovernorKey,
	}
	return inbox
}

func (f *runFixture) apply(t *testing.T) {
	t.Helper()
	if _, err := f.inbox().Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
}

// approve adds the Governor's approval of step id on mw-e.3, approved at, as
// txid — signed over the step's true hash unless sha says otherwise.
func (f *runFixture) approve(t *testing.T, txid, id string, at time.Time, sha string) {
	t.Helper()
	if sha == "" {
		sha = domain.HandsSHA256("mw-e.3", f.steps[id])
	}
	f.action(t, txid, map[string]any{
		"action": "run", "bead": "mw-e.3", "step": id, "sha256": sha,
		"approved_at": at.Unix(), "sig": apptest.FakeHandsSig(releaseTapGovernorKey, sha, at.Unix()),
	})
}

// sentBack is what went back to the Governor, the n-th message, decrypted.
func (f *runFixture) sentBack(t *testing.T, n int) application.PosternThreadedMessage {
	t.Helper()
	delivered := f.backend.Delivered()
	if len(delivered) <= n {
		t.Fatalf("expected at least %d message(s) back to the Governor, got %d", n+1, len(delivered))
	}
	var payload application.PosternPayload
	mustDo(t, json.Unmarshal(delivered[n], &payload))
	text, _, err := f.cipher.Decrypt("governor", payload.Ct)
	mustDo(t, err)
	var body application.PosternThreadedMessage
	mustDo(t, json.Unmarshal([]byte(text), &body))
	return body
}

// The Governor's approval of a step for this host, as the user, runs it
// here: how it ran is recorded, commented on the bead, sent back to him in
// the bead's thread and mailed to the Mayor — and the approval is spent.
func TestApplyRunsAnApprovedStepHere(t *testing.T) {
	f := newRunFixture(t)
	f.approve(t, "tx-run", "echo", runNow.Add(-2*time.Minute), "")

	f.apply(t)

	jobs := f.runner.Jobs()
	if len(jobs) != 1 || jobs[0].Remote != nil {
		t.Fatalf("expected one job run here, got %+v", jobs)
	}
	want := domain.HandsRequest{Bead: "mw-e.3", ID: "echo", Host: "desktop", As: "user", Run: "echo hello",
		SHA256: domain.HandsSHA256("mw-e.3", f.steps["echo"]), ApprovedAt: runNow.Add(-2 * time.Minute).Unix(),
		Sig: apptest.FakeHandsSig(releaseTapGovernorKey, domain.HandsSHA256("mw-e.3", f.steps["echo"]), runNow.Add(-2*time.Minute).Unix())}
	if jobs[0].Request != want {
		t.Fatalf("expected the request\n%+v\ngot\n%+v", want, jobs[0].Request)
	}
	ran, _ := f.tracker.Note(context.Background(), application.HandsRanKey("mw-e.3", "echo"))
	if ran != `{"at":"2026-09-28T12:10:00Z","exit":0,"host":"desktop"}` {
		t.Fatalf("expected the run recorded, got %q", ran)
	}
	outcome := "RAN step echo on desktop as user, exit 0 (approved by the Governor via postern, txid tx-run)\n\n```\nhello\n```"
	if got := f.tracker.Comments("mw-e.3"); len(got) != 1 || got[0] != outcome {
		t.Fatalf("expected the one outcome comment\n%s\ngot %q", outcome, got)
	}
	back := f.sentBack(t, 0)
	if back.Thread.Bead != "mw-e.3" || back.Re != "tx-run" || back.Text != outcome || back.Role != "" {
		t.Fatalf("expected the outcome sent back in the bead's thread, re the approval, got %+v", back)
	}
	if subjects := f.subjects(t); len(subjects) != 1 || subjects[0] != "Ran: mw-e.3 echo, exit 0" {
		t.Fatalf("expected one mail to the Mayor, got %v", subjects)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx-run")); note != "applied run mw-e.3 txid tx-run: exit 0" {
		t.Fatalf("expected the txid marked applied, got %q", note)
	}
}

// A root step is handed on whole, for mw-hands-root to check again; a step
// for another host goes by the ssh prefix config names for it.
func TestApplyHandsARootStepOnAndReachesAnotherHostBySSH(t *testing.T) {
	f := newRunFixture(t)
	f.approve(t, "tx-root", "linger", runNow, "")
	f.approve(t, "tx-far", "far", runNow, "")

	f.apply(t)

	jobs := f.runner.Jobs()
	if len(jobs) != 2 {
		t.Fatalf("expected two jobs, got %+v", jobs)
	}
	if jobs[0].Request.As != "root" || jobs[0].Request.WayBack != "loginctl disable-linger jwhite" || jobs[0].Remote != nil {
		t.Fatalf("expected the root step handed on whole, here, got %+v", jobs[0])
	}
	if strings.Join(jobs[1].Remote, " ") != "ssh laptop" || jobs[1].Request.Host != "laptop" {
		t.Fatalf("expected the laptop's step over ssh laptop, got %+v", jobs[1])
	}
	ran, _ := f.tracker.Note(context.Background(), application.HandsRanKey("mw-e.3", "far"))
	if !strings.Contains(ran, `"host":"laptop"`) {
		t.Fatalf("expected the run recorded on the laptop, got %q", ran)
	}
}

// Whatever cannot run is refused, never run, and said by the same three
// channels with the reason — commented, sent back, mailed — and marked so it
// is never tried again.
func TestApplyRefusesAnApprovalThatCannotRun(t *testing.T) {
	cases := map[string]struct {
		setup func(t *testing.T, f *runFixture)
		why   string
	}{
		"a changed step": {func(t *testing.T, f *runFixture) {
			f.approve(t, "tx", "echo", runNow, domain.HandsSHA256("mw-e.3", domain.HandsStep{ID: "echo", Host: "desktop", As: "user", Run: "echo old"}))
		}, "the step changed since you approved it"},
		"another key's signature": {func(t *testing.T, f *runFixture) {
			sha := domain.HandsSHA256("mw-e.3", f.steps["echo"])
			f.action(t, "tx", map[string]any{"action": "run", "bead": "mw-e.3", "step": "echo", "sha256": sha,
				"approved_at": runNow.Unix(), "sig": apptest.FakeHandsSig("someone-else", sha, runNow.Unix())})
		}, "signature"},
		"an approval 16 minutes old": {func(t *testing.T, f *runFixture) {
			f.approve(t, "tx", "echo", runNow.Add(-16*time.Minute), "")
		}, "old"},
		"an approval from 3 minutes ahead": {func(t *testing.T, f *runFixture) {
			f.approve(t, "tx", "echo", runNow.Add(3*time.Minute), "")
		}, "ahead"},
		"a step the bead has not": {func(t *testing.T, f *runFixture) {
			f.steps["ghost"] = domain.HandsStep{ID: "ghost", Host: "desktop", As: "user", Run: "true"}
			f.approve(t, "tx", "ghost", runNow, "")
		}, "no step ghost"},
		"a host config does not reach": {func(t *testing.T, f *runFixture) {
			f.approve(t, "tx", "nowhere", runNow, "")
		}, "[hands_hosts]"},
		"an approval that already ran": {func(t *testing.T, f *runFixture) {
			sha := domain.HandsSHA256("mw-e.3", f.steps["echo"])
			mustDo(t, f.tracker.SetNote(context.Background(), application.HandsApprovalKey(domain.HandsApprovalID(sha, runNow.Unix())), "tx-earlier"))
			f.approve(t, "tx", "echo", runNow, "")
		}, "already run"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newRunFixture(t)
			c.setup(t, f)

			f.apply(t)
			f.apply(t)

			if jobs := f.runner.Jobs(); len(jobs) != 0 {
				t.Fatalf("expected nothing run, got %+v", jobs)
			}
			comments := f.tracker.Comments("mw-e.3")
			if len(comments) != 1 || !strings.HasPrefix(comments[0], "NOT RUN step ") || !strings.Contains(comments[0], c.why) {
				t.Fatalf("expected one NOT RUN comment saying %q, got %q", c.why, comments)
			}
			if back := f.sentBack(t, 0); back.Re != "tx" || !strings.Contains(back.Text, c.why) || len(f.backend.Delivered()) != 1 {
				t.Fatalf("expected the refusal sent back once, got %+v", back)
			}
			if subjects := f.subjects(t); len(subjects) != 1 || !strings.HasPrefix(subjects[0], "Not run: mw-e.3 ") {
				t.Fatalf("expected one Not run mail, got %v", subjects)
			}
			if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx")); !strings.HasPrefix(note, "refused run mw-e.3 txid tx: ") {
				t.Fatalf("expected the refusal marked, got %q", note)
			}
		})
	}
}

// A run is applied once per txid, and an approval runs once however many
// messages carry it.
func TestApplyRunsAnApprovalOnce(t *testing.T) {
	f := newRunFixture(t)
	f.approve(t, "tx-1", "echo", runNow, "")
	f.approve(t, "tx-2", "echo", runNow, "")

	f.apply(t)
	f.apply(t)

	if jobs := f.runner.Jobs(); len(jobs) != 1 {
		t.Fatalf("expected the step run once, got %d", len(jobs))
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx-2")); !strings.Contains(note, "already run") {
		t.Fatalf("expected the second message's approval refused as already run, got %q", note)
	}
}

// The outcome carries the last 4000 characters of what the step printed,
// in a fence its own backticks cannot close; a step that could not start at
// all is an outcome too.
func TestApplyRunKeepsTheLastOutputAndSaysWhenAStepCouldNotStart(t *testing.T) {
	f := newRunFixture(t)
	f.runner.Outcome = application.HandsOutcome{Exit: 2, Output: strings.Repeat("é", 3000) + "```" + strings.Repeat("z", 3000)}
	f.approve(t, "tx-long", "echo", runNow, "")
	f.apply(t)

	comment := f.tracker.Comments("mw-e.3")[0]
	if !strings.Contains(comment, "exit 2") || !strings.Contains(comment, "````\n") {
		t.Fatalf("expected exit 2 and a four-backtick fence, got %.200q", comment)
	}
	body := comment[strings.Index(comment, "````\n")+5 : strings.LastIndex(comment, "\n````")]
	if n := len([]rune(body)); n != 4000 || !strings.HasSuffix(body, "zzz") {
		t.Fatalf("expected the last 4000 characters, got %d", n)
	}

	g := newRunFixture(t)
	g.runner.Err = fmt.Errorf("exec: sudo: not found")
	g.approve(t, "tx-nostart", "linger", runNow, "")
	g.apply(t)
	if comment := g.tracker.Comments("mw-e.3")[0]; !strings.Contains(comment, "exit -1") || !strings.Contains(comment, "sudo: not found") {
		t.Fatalf("expected a step that could not start reported with exit -1, got %q", comment)
	}
}

// With no runner configured, a run action is left for the Mayor to read.
func TestApplyLeavesARunForTheMayorWhenNoRunnerIsConfigured(t *testing.T) {
	f := newRunFixture(t)
	f.approve(t, "tx", "echo", runNow, "")
	inbox := f.inbox()
	inbox.HandsRunner = nil

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx")); note != "" {
		t.Fatalf("expected the run left unapplied, got %q", note)
	}
}
