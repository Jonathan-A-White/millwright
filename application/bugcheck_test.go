package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// An example sent alone that fails while the host is far over its cores
// (mw-gq6.1083), as a spec rechecked alone does (mw-gq6.1070).

// answersInTurn is a grist sender whose answers are said in turn, the last again.
type answersInTurn struct {
	answers []string
	sent    int
}

func (s *answersInTurn) Run(_ context.Context, _ application.GristSendRequest) (application.GristSendReport, error) {
	say := s.answers[min(s.sent, len(s.answers)-1)]
	s.sent++
	return application.GristSendReport{Txid: "direct:t", Answer: &application.GristAnswer{
		Status: application.GristAnswered, Answer: json.RawMessage(`{"answer": "` + say + `"}`),
	}}, nil
}

// loadedExampleRecheck is a GristExampleRecheck of the "greets" example (which
// expects the answer "ref") on a host that reads readings in turn, whose waits
// take no time, and whose mill says answers in turn.
func loadedExampleRecheck(t *testing.T, answers []string, readings ...application.LoadReading) (application.GristExampleRecheck, *answersInTurn, *apptest.FakeHostLoad) {
	t.Helper()
	sender := &answersInTurn{answers: answers}
	smoke, _ := evalRig(t, sender)
	load := &apptest.FakeHostLoad{Readings: readings}
	return application.GristExampleRecheck{
		Smoke: smoke,
		Calm: application.Suite{
			Host: "cloud1", Load: load,
			LoadPoll: time.Second, LoadBound: 3 * time.Second,
			LoadWait: func(context.Context, time.Duration) error { return nil },
		},
	}, sender, load
}

func TestAnExampleThatFailsFarOverTheCoresAndPassesWhenTheHostCalmsPasses(t *testing.T) {
	g, sender, _ := loadedExampleRecheck(t, []string{"main", "ref"}, farOverHost, calmHost)

	got, err := g.RecheckExamples(context.Background(), "lampas", "tutor", []string{"greets"})
	if err != nil {
		t.Fatalf("re-running: %v", err)
	}
	if !got.Passed {
		t.Errorf("expected the second run to stand: passed, got %+v", got)
	}
	if sender.sent != 2 {
		t.Errorf("expected the example sent twice, sent %d times", sender.sent)
	}
}

func TestAnExampleThatFailsFarOverTheCoresAndFailsAgainAfterTheHostCalmsFails(t *testing.T) {
	g, sender, _ := loadedExampleRecheck(t, []string{"main"}, farOverHost, calmHost)

	got, err := g.RecheckExamples(context.Background(), "lampas", "tutor", []string{"greets"})
	if err != nil || got.Passed {
		t.Errorf("expected the second failure to stand as an answer, got %+v, %v", got, err)
	}
	if sender.sent != 2 {
		t.Errorf("expected the example sent twice, sent %d times", sender.sent)
	}
}

func TestAnExampleThatFailsFarOverTheCoresOnAHostThatNeverCalmsIsABusyHostNotAnAnswer(t *testing.T) {
	g, sender, _ := loadedExampleRecheck(t, []string{"main", "ref"}, farOverHost)

	got, err := g.RecheckExamples(context.Background(), "lampas", "tutor", []string{"greets"})
	if err == nil {
		t.Fatalf("expected an error for a host that never calmed, got %+v", got)
	}
	if want := "not re-checked: cloud1 stayed at load 40.0 of 16"; !strings.Contains(err.Error(), want) {
		t.Errorf("expected %q in the error, got %q", want, err.Error())
	}
	if sender.sent != 1 {
		t.Errorf("expected no second send on a host that never calmed, sent %d times", sender.sent)
	}
}

func TestAnExampleThatFailsBusyButNotFarOverTheCoresIsSentOnce(t *testing.T) {
	g, sender, _ := loadedExampleRecheck(t, []string{"main", "ref"}, busyHost)

	got, err := g.RecheckExamples(context.Background(), "lampas", "tutor", []string{"greets"})
	if err != nil || got.Passed {
		t.Errorf("expected the failure to stand at once, got %+v, %v", got, err)
	}
	if sender.sent != 1 {
		t.Errorf("expected one send at 20 of 16, sent %d times", sender.sent)
	}
}

func TestAnExampleThatPassesReadsNoLoad(t *testing.T) {
	g, _, load := loadedExampleRecheck(t, []string{"ref"}, farOverHost)

	if got, err := g.RecheckExamples(context.Background(), "lampas", "tutor", []string{"greets"}); err != nil || !got.Passed {
		t.Fatalf("got %+v, %v", got, err)
	}
	if load.Reads != 0 {
		t.Errorf("a pass needs no look at the load, it was read %d times", load.Reads)
	}
}

func TestADispatchKeepsAnExampleOnAHostThatNeverCalmedInTheRecheckLog(t *testing.T) {
	g, _, _ := loadedExampleRecheck(t, []string{"main", "ref"}, farOverHost)
	dispatch, tracker, _, _, _ := aFactory(t)
	aGristBug(t, &dispatch, tracker, "", "Check: mw grist eval lampas tutor --example greets\n")
	dispatch.ExampleRechecks = g
	log := &apptest.FakeTickLog{}
	dispatch.RecheckSkips = log

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	lines := log.Lines()
	if len(lines) != 1 || !strings.Contains(lines[0], "mw-gq6.1 not re-checked: cloud1 stayed at load 40.0 of 16") {
		t.Errorf("recheck log = %q, want the busy host named with the story", lines)
	}
}
