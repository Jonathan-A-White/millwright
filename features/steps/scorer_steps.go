package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// scorerContext is one `mw grist score`: fake engines, an audio file in a
// temp directory, and what the command printed and said.
type scorerContext struct {
	dir     string
	engines map[string]*apptest.FakeScorer
	out     bytes.Buffer
	err     error
}

// InitializeScorerScenario registers the steps of features/scorer.feature.
func InitializeScorerScenario(ctx *godog.ScenarioContext) {
	c := &scorerContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = scorerContext{}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^the scorer engines "([^"]*)" and "([^"]*)" are configured$`, c.enginesConfigured)
	ctx.Given(`^the engine "([^"]*)" scores any reading as the words "([^"]*)", "([^"]*)" and "([^"]*)"$`, c.engineScores)
	ctx.Given(`^the engine "([^"]*)" fails with "([^"]*)"$`, c.engineFails)
	ctx.Given(`^an audio file "([^"]*)" holding a reading$`, c.anAudioFile)
	ctx.When(`^mw grist score reads "([^"]*)" against "([^"]*)" with the engine "([^"]*)"$`, c.scoreDefaultLang)
	ctx.When(`^mw grist score reads "([^"]*)" against "([^"]*)" in "([^"]*)" with the engine "([^"]*)"$`, c.score)
	ctx.Then(`^mw grist score printed a result from the engine "([^"]*)" with (\d+) words$`, c.printedResult)
	ctx.Then(`^the engine "([^"]*)" was given the target "([^"]*)" in "([^"]*)" as "([^"]*)"$`, c.engineWasGiven)
	ctx.Then(`^the engine "([^"]*)" was given nothing$`, c.engineWasGivenNothing)
	ctx.Then(`^mw grist score refused, saying "(.*)"$`, c.refusedSaying)
	ctx.Then(`^mw grist score printed nothing$`, c.printedNothing)
}

func (c *scorerContext) enginesConfigured(a, b string) error {
	d, err := os.MkdirTemp("", "mw-scorer-")
	if err != nil {
		return err
	}
	c.dir = d
	c.engines = map[string]*apptest.FakeScorer{a: {}, b: {}}
	return nil
}

func (c *scorerContext) engineScores(name, w1, w2, w3 string) error {
	engine := c.engines[name]
	if engine == nil {
		return fmt.Errorf("no engine %q is configured", name)
	}
	engine.Result = application.ReadingResult{Engine: name, Accuracy: 80, Seconds: 1.5}
	for _, text := range []string{w1, w2, w3} {
		engine.Result.Words = append(engine.Result.Words, application.ReadingWord{
			Text: text, ExpectedPhonemes: []string{"X"}, ProducedPhonemes: []string{"X"}, Error: application.ErrNone, Accuracy: 80,
		})
	}
	return nil
}

func (c *scorerContext) engineFails(name, why string) error {
	engine := c.engines[name]
	if engine == nil {
		return fmt.Errorf("no engine %q is configured", name)
	}
	engine.Err = fmt.Errorf("%s", why)
	return nil
}

func (c *scorerContext) anAudioFile(name string) error {
	return os.WriteFile(filepath.Join(c.dir, name), []byte("not really audio; the fake never listens"), 0o644)
}

func (c *scorerContext) scoreDefaultLang(file, target, engine string) error {
	return c.score(file, target, "en", engine)
}

func (c *scorerContext) score(file, target, lang, engine string) error {
	engines := map[string]application.Scorer{}
	for name, fake := range c.engines {
		engines[name] = fake
	}
	_, c.err = application.GristScore{
		Scorers: application.NewScorerRegistry(engines),
		Out:     &c.out,
	}.Run(context.Background(), application.GristScoreRequest{
		Engine: engine, Target: target, AudioFile: filepath.Join(c.dir, file), Lang: lang,
	})
	return nil
}

func (c *scorerContext) printedResult(engine string, words int) error {
	if c.err != nil {
		return fmt.Errorf("expected a result, got the error: %v", c.err)
	}
	var result application.ReadingResult
	if err := json.Unmarshal(c.out.Bytes(), &result); err != nil {
		return fmt.Errorf("expected JSON, got %q: %v", c.out.String(), err)
	}
	if result.Engine != engine || len(result.Words) != words {
		return fmt.Errorf("expected %d words from %s, got %d from %q", words, engine, len(result.Words), result.Engine)
	}
	return nil
}

func (c *scorerContext) engineWasGiven(name, target, lang, mime string) error {
	calls := c.engines[name].Calls()
	if len(calls) != 1 {
		return fmt.Errorf("expected %s to be asked once, it was asked %d times", name, len(calls))
	}
	if got := calls[0]; got.Target != target || got.Lang != lang || got.Mime != mime || len(got.Audio) == 0 {
		return fmt.Errorf("%s was given %q in %q as %q with %d bytes of audio", name, got.Target, got.Lang, got.Mime, len(got.Audio))
	}
	return nil
}

func (c *scorerContext) engineWasGivenNothing(name string) error {
	if n := len(c.engines[name].Calls()); n != 0 {
		return fmt.Errorf("expected %s not to be asked, it was asked %d times", name, n)
	}
	return nil
}

func (c *scorerContext) refusedSaying(want string) error {
	if c.err == nil {
		return fmt.Errorf("expected a refusal, got none; printed %q", c.out.String())
	}
	want = strings.ReplaceAll(want, `\"`, `"`)
	if !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected the refusal to say %q, it said %q", want, c.err.Error())
	}
	return nil
}

func (c *scorerContext) printedNothing() error {
	if c.out.Len() != 0 {
		return fmt.Errorf("expected nothing printed, got %q", c.out.String())
	}
	return nil
}
