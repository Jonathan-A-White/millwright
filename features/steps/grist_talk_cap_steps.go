package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// capKeyPoints is the key points the stand-in writes when it is asked for
// them; it is in talkStandIn.
const capKeyPoints = "The reader's son Luke is 14."

// registerGristTalkCap adds the steps of features/grist_talk_cap.feature to
// those of the talk feature, whose stand-in claude it shares: it answers the
// key-points schema (a call whose --json-schema names key_points) with
// capKeyPoints, exits 1 when key-points-fail exists and answers empty key
// points when key-points-empty does.
func registerGristTalkCap(ctx *godog.ScenarioContext, c *gristContext) {
	t := &c.talk

	ctx.Given(`^the talk context cap is (\d+) tokens$`, func(n int) error {
		c.ceilings.TalkContextCap = n
		return nil
	})
	ctx.Given(`^the talk turn cap is (\d+)$`, func(n int) error {
		c.ceilings.TalkMaxTurns = n
		return nil
	})
	ctx.Given(`^every key-points call exits 1$`, func() error {
		return os.WriteFile(filepath.Join(t.dir, "key-points-fail"), nil, 0o644)
	})
	ctx.Given(`^every key-points call answers with empty key points$`, func() error {
		return os.WriteFile(filepath.Join(t.dir, "key-points-empty"), nil, 0o644)
	})
	ctx.Given(`^the talk's ended session ended (\d+) hours ago$`, t.endedAgo)
	ctx.Given(`^the talk "([^"]*)" last turned (\d+) days ago$`, func(talk string, days int) error {
		return t.talkLastTurnedAgo(talk, days*24*60)
	})
	ctx.When(`^mw grist talks --end is run for cairn's "([^"]*)"$`, t.end)

	ctx.Then(`^session (\d+) resumed session (\d+)'s with the key-points prompt and schema$`, t.keyPointsCall)
	ctx.Then(`^the talk's record holds the key points "([^"]*)" and its session is ended$`, t.recordHoldsKeyPoints)
	ctx.Then(`^the talk's record holds no key points and its session is ended$`, func() error { return t.recordHoldsKeyPoints("") })
	ctx.Then(`^the talk's record holds no key points$`, func() error { return t.recordKeyPoints("") })
	ctx.Then(`^session (\d+)'s message opens with the key points "([^"]*)" and then the request with its history$`, t.messageOpensWith)
	ctx.Then(`^session (\d+)'s message opens with no key points$`, func(n int) error {
		message, err := t.message(n)
		if err != nil {
			return err
		}
		if strings.Contains(message, "key_points_of_this_talk_so_far") {
			return fmt.Errorf("session %d's message holds key points: %q", n, message)
		}
		return nil
	})
	ctx.Then(`^the answer to "([^"]*)" says the talk's session is "([^"]*)", at turn (\d+), with the key points "([^"]*)"$`, t.refreshedSays)
	ctx.Then(`^session (\d+)'s transcript is (kept|gone)$`, t.transcript)
	ctx.Then(`^the other talk's session is kept$`, func() error {
		record, err := t.record("other")
		if err != nil {
			return err
		}
		if record.Session == "" {
			return fmt.Errorf("the other talk has no session")
		}
		return nil
	})
	ctx.Then(`^the talk "([^"]*)" has no live session and no key points$`, func(talk string) error {
		record, err := t.record(talk)
		if err != nil {
			return err
		}
		if record.Session != "" || record.KeyPoints != "" {
			return fmt.Errorf("the talk %q holds session %q and key points %q", talk, record.Session, record.KeyPoints)
		}
		return nil
	})
	ctx.Then(`^it printed that it ended (\d+) talks?$`, func(n int) error {
		if want := fmt.Sprintf("ended %d talk", n); !strings.Contains(t.out.String(), want) {
			return fmt.Errorf("mw grist talks --end printed %q, want %q", t.out.String(), want)
		}
		return nil
	})
	ctx.Then(`^it refused, saying the mill keeps no live talk "([^"]*)" of cairn$`, func(talk string) error {
		if t.endErr == nil || !strings.Contains(t.endErr.Error(), fmt.Sprintf("no live talk %q of cairn", talk)) {
			return fmt.Errorf("mw grist talks --end returned %v", t.endErr)
		}
		return nil
	})
}

// endedAgo moves the end of the talk's one ended session back to hours ago.
func (t *talkContext) endedAgo(hours int) error {
	record, err := t.record("rom.8@1")
	if err != nil {
		return err
	}
	if len(record.EndedAt) != 1 {
		return fmt.Errorf("the talk's record names %d ended sessions with a time: %+v", len(record.EndedAt), record)
	}
	for session := range record.EndedAt {
		record.EndedAt[session] = gristNow.Add(-time.Duration(hours) * time.Hour)
	}
	return t.store.Save(context.Background(), record)
}

func (t *talkContext) end(talk string) error {
	t.out.Reset()
	_, err := application.GristTalksEnd{Store: t.store, Out: &t.out, Now: func() time.Time { return gristNow }}.Run(context.Background(), "cairn", talk)
	t.endErr = err
	return nil
}

// message is the message session n was given.
func (t *talkContext) message(n int) (string, error) {
	pid, err := t.session(fmt.Sprintf("session %d", n))
	if err != nil {
		return "", err
	}
	args, err := t.args(pid)
	if err != nil {
		return "", err
	}
	return args[len(args)-1], nil
}

func (t *talkContext) keyPointsCall(n, of int) error {
	pid, err := t.session(fmt.Sprintf("session %d", n))
	if err != nil {
		return err
	}
	args, err := t.talkSession(pid)
	if err != nil {
		return err
	}
	want, err := t.sessionID(of)
	if err != nil {
		return err
	}
	if got, _ := flag(args, "--resume"); got != want {
		return fmt.Errorf("session %d resumed %q, not session %d's %q", n, got, of, want)
	}
	if got := args[len(args)-1]; got != application.GristKeyPointsPrompt {
		return fmt.Errorf("session %d's message is %q, not the key-points prompt", n, got)
	}
	// The harness is given the schema compacted, its keys in order.
	var got, want2 any
	given, _ := flag(args, "--json-schema")
	if json.Unmarshal([]byte(given), &got) != nil || json.Unmarshal([]byte(application.GristKeyPointsSchema), &want2) != nil || !reflect.DeepEqual(got, want2) {
		return fmt.Errorf("session %d's --json-schema is %q, not the key-points schema", n, given)
	}
	return nil
}

func (t *talkContext) recordHoldsKeyPoints(points string) error {
	if err := t.recordKeyPoints(points); err != nil {
		return err
	}
	record, err := t.record("")
	if err != nil {
		return err
	}
	first, err := t.sessionID(1)
	if err != nil {
		return err
	}
	if record.Session != "" || len(record.Ended) == 0 || record.Ended[len(record.Ended)-1] != first {
		return fmt.Errorf("the talk's record is on session %q with ended %q, not ended on session 1's %s", record.Session, record.Ended, first)
	}
	if _, ok := record.EndedAt[first]; !ok {
		return fmt.Errorf("the talk's record gives no time for the end of session 1's %s: %+v", first, record.EndedAt)
	}
	return nil
}

func (t *talkContext) recordKeyPoints(points string) error {
	record, err := t.record("")
	if err != nil {
		return err
	}
	if record.KeyPoints != points {
		return fmt.Errorf("the talk's record holds the key points %q, want %q", record.KeyPoints, points)
	}
	return nil
}

func (t *talkContext) messageOpensWith(n int, points string) error {
	message, err := t.message(n)
	if err != nil {
		return err
	}
	if lead := "key_points_of_this_talk_so_far: " + points; !strings.HasPrefix(message, lead) {
		return fmt.Errorf("session %d's message does not open with %q: %q", n, lead, message)
	}
	rest := message[strings.Index(message, "Grind this grist"):]
	if !strings.Contains(rest, `"history"`) || !strings.Contains(rest, `"q":"q3"`) {
		return fmt.Errorf("session %d's message holds no request with its history: %q", n, message)
	}
	return nil
}

func (t *talkContext) refreshedSays(q, session string, turns int, points string) error {
	a, err := t.answerTo(q)
	if err != nil {
		return err
	}
	want := application.GristAnswerTalk{Session: session, Turns: turns, ContextTokens: 10 + 500 + 1000*turns, KeyPoints: points}
	if a.Talk == nil || *a.Talk != want {
		return fmt.Errorf("the answer to %q says of its talk %+v, want %+v", q, a.Talk, want)
	}
	return nil
}

func (t *talkContext) transcript(n int, state string) error {
	id, err := t.sessionID(n)
	if err != nil {
		return err
	}
	cwd, err := t.store.Cwd("cairn")
	if err != nil {
		return err
	}
	kept, err := t.store.Transcript(cwd, id)
	if err != nil {
		return err
	}
	if kept != (state == "kept") {
		return fmt.Errorf("session %d's transcript is kept: %v, want %s (%s)", n, kept, state, grist.TranscriptPath(t.store.Projects, cwd, id))
	}
	return nil
}
