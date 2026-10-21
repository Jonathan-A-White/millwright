package steps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
)

// evalTalkContext is what features/grist_eval_talk.feature adds to a mill with
// its stand-in claude: the eval and the smoke sending their examples to that
// mill, each grist through the fake backend, ground at once.
type evalTalkContext struct {
	c *gristContext

	sent   []evalTalkSent
	out    bytes.Buffer
	err    error
	report application.GristSmokeReport
}

// evalTalkSent is one grist the eval or the smoke sent: the talk it named
// (empty for none) and the request it carried.
type evalTalkSent struct{ talk, request string }

// evalTalkSender is the smoke's sender made of the mill itself: it seals the
// example's request as the phone would, lets the mill grind it and hands back
// the answer the mill delivered.
type evalTalkSender struct{ e *evalTalkContext }

func (s evalTalkSender) Run(_ context.Context, req application.GristSendRequest) (application.GristSendReport, error) {
	c := s.e.c
	raw, err := os.ReadFile(req.RequestFile)
	if err != nil {
		return application.GristSendReport{}, err
	}
	s.e.sent = append(s.e.sent, evalTalkSent{talk: req.Talk, request: string(raw)})
	c.askInput, c.talk.next = raw, req.Talk
	defer func() { c.askInput, c.talk.next = nil, "" }()
	if err := c.thePhoneSends("cairn", "sweep", "1.1", 1); err != nil {
		return application.GristSendReport{}, err
	}
	report := application.GristSendReport{Txid: c.grist.Txid}
	if err := c.theMillGrinds(); err != nil {
		return report, err
	}
	_, _, answers, err := c.delivered()
	if err != nil {
		return report, err
	}
	for _, a := range answers {
		if a.Re != report.Txid {
			continue
		}
		report.Answer = &a
		if a.Status != application.GristAnswered {
			return report, &application.GristUnanswered{Txid: report.Txid, Answer: &a, Wait: req.Wait}
		}
		return report, nil
	}
	return report, &application.GristUnanswered{Txid: report.Txid, Wait: req.Wait}
}

func registerGristEvalTalk(ctx *godog.ScenarioContext, c *gristContext) {
	e := &c.evalTalk
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		e.c = c
		return ctx, nil
	})

	ctx.Given(`^the rig of "([^"]*)" holds the example "([^"]*)" containing:$`, e.holdsExample)
	ctx.When(`^mw grist eval is run for the example "([^"]*)" of "([^"]*)" kind "([^"]*)"$`, e.evalRuns)
	ctx.When(`^mw grist smoke is run for "([^"]*)" with talks$`, e.smokeRuns)
	ctx.Then(`^the eval passed$`, e.passed)
	ctx.Then(`^the eval failed, saying "([^"]*)"$`, e.failed)
	ctx.Then(`^the smoke with talks passed, saying "([^"]*)"$`, e.smokePassed)
	ctx.Then(`^the eval sent both turns under one talk named like "([^"]*)"$`, e.sentBothUnder)
	ctx.Then(`^the eval sent turn 2 with no history$`, e.sentTurn2WithoutHistory)
	ctx.Then(`^the eval sent no talk$`, e.sentNoTalk)
	ctx.Then(`^no transcript of a session is left$`, func() error { return e.transcripts(0) })
	ctx.Then(`^the mill keeps (\d+) transcripts of sessions$`, e.transcripts)
	ctx.Then(`^the mill keeps only the talks "([^"]*)" and "([^"]*)"$`, e.keepsOnly)
}

func (e *evalTalkContext) holdsExample(app, name string, doc *godog.DocString) error {
	c := e.c
	c.grinds.SetFile(c.checkout(app), c.commit, "grinds/examples/"+name+".json", []byte(doc.Content))
	return nil
}

func (e *evalTalkContext) smoke() application.GristSmoke {
	c := e.c
	return application.GristSmoke{
		Grinds: c.grinds, Send: evalTalkSender{e}, Apps: map[string]string{"cairn": c.checkout("cairn")},
		Wait: application.GristSmokeWait, TempDir: c.home,
		Talks: application.GristTalkSweep{Store: c.talk.store, Now: func() time.Time { return gristNow }},
	}
}

func (e *evalTalkContext) evalRuns(name, app, kind string) error {
	e.out.Reset()
	e.sent = nil
	e.err = application.GristEvalExamples{Smoke: e.smoke(), Out: &e.out}.Run(context.Background(), app, kind, []string{name})
	return nil
}

func (e *evalTalkContext) smokeRuns(app string) error {
	e.sent, e.err = nil, nil
	e.report, e.err = e.smoke().Run(context.Background(), app, "")
	return nil
}

func (e *evalTalkContext) passed() error {
	if e.err != nil {
		return fmt.Errorf("the eval did not pass: %v\n%s", e.err, e.out.String())
	}
	return nil
}

func (e *evalTalkContext) failed(said string) error {
	if !errors.Is(e.err, application.ErrGristEvalFailed) {
		return fmt.Errorf("the eval did not fail: %v\n%s", e.err, e.out.String())
	}
	if !strings.Contains(e.out.String(), said) {
		return fmt.Errorf("the eval did not say %q:\n%s", said, e.out.String())
	}
	return nil
}

func (e *evalTalkContext) smokePassed(said string) error {
	if e.err != nil || e.report.Failed() || !strings.Contains(e.report.Line(), said) {
		return fmt.Errorf("the smoke did not pass saying %q: %v, %s", said, e.err, e.report.Line())
	}
	return nil
}

func (e *evalTalkContext) sentBothUnder(pattern string) error {
	if len(e.sent) != 2 {
		return fmt.Errorf("%d grists were sent, not 2: %+v", len(e.sent), e.sent)
	}
	re := regexp.MustCompile("^" + strings.Replace(regexp.QuoteMeta(pattern), regexp.QuoteMeta("<nonce>"), "[0-9a-f]{8}", 1) + "$")
	if !re.MatchString(e.sent[0].talk) || e.sent[1].talk != e.sent[0].talk {
		return fmt.Errorf("the grists named the talks %q and %q, not both like %q", e.sent[0].talk, e.sent[1].talk, pattern)
	}
	return nil
}

func (e *evalTalkContext) sentTurn2WithoutHistory() error {
	if len(e.sent) != 2 || strings.Contains(e.sent[1].request, "history") || !strings.Contains(e.sent[1].request, "turn2") {
		return fmt.Errorf("turn 2 was sent as %+v", e.sent)
	}
	return nil
}

func (e *evalTalkContext) sentNoTalk() error {
	if len(e.sent) != 1 || e.sent[0].talk != "" {
		return fmt.Errorf("expected one grist with no talk, got %+v", e.sent)
	}
	return nil
}

// transcripts counts the sessions' transcripts the stand-in still keeps.
func (e *evalTalkContext) transcripts(want int) error {
	var found []string
	root := filepath.Join(e.c.talk.dir, "projects")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(found) != want {
		return fmt.Errorf("%d transcripts are kept, not %d: %v", len(found), want, found)
	}
	return nil
}

func (e *evalTalkContext) keepsOnly(a, b string) error {
	talks, err := e.c.talk.store.List(context.Background())
	if err != nil {
		return err
	}
	var names []string
	for _, t := range talks {
		names = append(names, t.Talk)
	}
	slices.Sort(names)
	if want := []string{a, b}; !slices.Equal(names, want) {
		return fmt.Errorf("the mill keeps the talks %q, not %q", names, want)
	}
	return nil
}
