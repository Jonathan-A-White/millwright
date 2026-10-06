package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/stampqueue"

	"github.com/cucumber/godog"
)

// chainStampContext is features/chain_stamp.feature: the real stamp files in a
// directory of the scenario's own, a fake backend and a fake tracker. It is
// built by the first Given, never in a Before hook, which would run for every
// feature's scenarios.
type chainStampContext struct {
	dir     string
	queue   application.StampQueue
	backend *apptest.FakePostern
	tracker *apptest.FakeTracker
	said    bytes.Buffer
	err     error
}

// chainStampKeys is a key file that signs whatever it is handed.
type chainStampKeys struct{}

func (chainStampKeys) Path() string          { return "" }
func (chainStampKeys) Exists() (bool, error) { return true, nil }
func (chainStampKeys) Generate() error       { return nil }
func (chainStampKeys) PublicKey() (string, string, error) {
	return "03bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "mfactory", nil
}
func (chainStampKeys) PrivateKeyWIF() (string, error) { return "priv", nil }
func (chainStampKeys) Sign(_ []application.PosternUtxo, payload []byte) (string, error) {
	return "rawtx:" + string(payload), nil
}
func (chainStampKeys) MarkSpent([]application.PosternUtxo) error { return nil }
func (chainStampKeys) MarkSent(string) error                     { return nil }

// InitializeChainStampScenario registers the steps of features/chain_stamp.feature.
func InitializeChainStampScenario(ctx *godog.ScenarioContext) {
	c := &chainStampContext{}

	// Only this feature's own scenarios have a directory to remove; for every
	// other feature's, the hook does nothing.
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
			c.dir = ""
		}
		return ctx, nil
	})

	ctx.Given(`^a stamp queue holding stamps for the stories (.+)$`, c.aQueueHolding)
	ctx.Given(`^the backend refuses every broadcast, saying: (.+)$`, c.theBackendRefuses)
	ctx.When(`^the backend takes broadcasts again$`, func() error { c.backend.ChainErr = nil; return nil })
	ctx.When(`^the chain-stamp job runs$`, c.theJobRuns)
	ctx.Given(`^the chain-stamp job runs$`, c.theJobRuns)

	ctx.Then(`^the job succeeded$`, func() error {
		if c.err != nil {
			return fmt.Errorf("the job failed: %v", c.err)
		}
		return nil
	})
	ctx.Then(`^(\d+) transactions were broadcast$`, func(n int) error {
		if got := len(c.backend.Broadcasts()); got != n {
			return fmt.Errorf("%d transactions broadcast, want %d", got, n)
		}
		return nil
	})
	ctx.Then(`^no stamp is pending$`, func() error {
		if pending, err := c.queue.Pending(context.Background()); err != nil || len(pending) != 0 {
			return fmt.Errorf("pending = %+v, %v", pending, err)
		}
		return nil
	})
	ctx.Then(`^(\d+) stamp is pending, with (\d+) failed try$`, c.pendingWithTries)
	ctx.Then(`^sent.jsonl holds (\d+) stamps, each with the txid its broadcast returned$`, c.sentHolds)
	ctx.Then(`^the story "([^"]*)" carries the comment "([^"]*)"$`, func(id, want string) error {
		for _, got := range c.tracker.Comments(id) {
			if got == want {
				return nil
			}
		}
		return fmt.Errorf("%s carries %q, want %q", id, c.tracker.Comments(id), want)
	})
	ctx.Then(`^the story "([^"]*)" carries no comment$`, func(id string) error {
		if got := c.tracker.Comments(id); len(got) != 0 {
			return fmt.Errorf("%s carries %q", id, got)
		}
		return nil
	})
}

func (c *chainStampContext) aQueueHolding(list string) error {
	dir, err := os.MkdirTemp("", "mw-chain-stamp-")
	if err != nil {
		return err
	}
	*c = chainStampContext{
		dir: dir, queue: stampqueue.New(filepath.Join(dir, "stamps")),
		backend: apptest.NewFakePostern(), tracker: apptest.NewFakeTracker(),
	}
	c.tracker.AddEpic("mw-epic", domain.Path{})
	for _, id := range strings.Split(strings.ReplaceAll(list, `"`, ""), " and ") {
		id = strings.TrimSpace(id)
		c.tracker.AddStory("mw-epic", domain.Story{ID: id, Title: "A story"})
		err := c.queue.Append(context.Background(), domain.Stamp{
			Rig: "millwright", Branch: "main", Commit: "commit-of-" + id, Story: id, Title: "A story",
			Host: "laptop", At: time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *chainStampContext) theBackendRefuses(said string) error {
	c.backend.ChainErr = errors.New(said)
	return nil
}

func (c *chainStampContext) theJobRuns() error {
	cipher := apptest.NewFakeCipher()
	c.err = application.ChainStamp{
		Queue: c.queue, Chain: apptest.NewFakeChain(c.backend, chainStampKeys{}), Keys: chainStampKeys{}, Cipher: cipher, Tracker: c.tracker,
		GovernorKey: "02aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Now:         func() time.Time { return time.Date(2026, 10, 4, 9, 1, 0, 0, time.UTC) },
		Err:         &c.said,
	}.Run(context.Background())
	return nil
}

func (c *chainStampContext) pendingWithTries(n, tries int) error {
	pending, err := c.queue.Pending(context.Background())
	if err != nil {
		return err
	}
	if len(pending) != n || (n > 0 && pending[0].Attempts != tries) {
		return fmt.Errorf("pending = %+v, want %d with %d failed try", pending, n, tries)
	}
	return nil
}

// sentHolds reads sent.jsonl itself, the file the Governor would read.
func (c *chainStampContext) sentHolds(n int) error {
	held, err := os.ReadFile(filepath.Join(c.dir, "stamps", "sent.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(held)), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != n {
		return fmt.Errorf("sent.jsonl holds %d stamps, want %d:\n%s", len(lines), n, held)
	}
	for i, line := range lines {
		var sent application.SentStamp
		if err := json.Unmarshal([]byte(line), &sent); err != nil {
			return err
		}
		if want := fmt.Sprintf("fake-txid-%d", i+1); sent.Txid != want {
			return fmt.Errorf("line %d has txid %q, want %q", i+1, sent.Txid, want)
		}
	}
	return nil
}
