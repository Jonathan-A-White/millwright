package steps

import (
	"context"
	"fmt"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The steps of features/batch_land.feature that watch the vault's seat ledger
// while mw land runs (mw-gq6.502): mw is told to stop the moment the first
// ledger line is written, and the vault is read each time a story is closed.

// ledgerWatch is what a scenario puts between mw land and the vault.
type ledgerWatch struct {
	mu sync.Mutex
	// atClose is, for each story closed, what the vault held uncommitted at
	// that moment.
	atClose map[string][]string
	err     error
}

// attach wraps the ports of next: the vault's ledger append stops mw land once
// the first line is in, and the tracker's close reads the vault first.
func (w *ledgerWatch) attach(next *application.Next, vaultDir string, stop context.CancelFunc) {
	next.Vault = &stoppingVault{Vault: next.Vault, stop: stop}
	next.Tracker = &closeWatcher{WorkTracker: next.Tracker, vaultDir: vaultDir, watch: w}
}

// stoppingVault ends mw land's context right after the first ledger line.
type stoppingVault struct {
	application.Vault
	stop func()
	once sync.Once
}

func (s *stoppingVault) AppendToLedger(ctx context.Context, seat, line string) error {
	err := s.Vault.AppendToLedger(ctx, seat, line)
	s.once.Do(s.stop)
	return err
}

// closeWatcher reads what the vault holds uncommitted as a story is closed.
type closeWatcher struct {
	application.WorkTracker
	vaultDir string
	watch    *ledgerWatch
}

func (c *closeWatcher) CloseStory(ctx context.Context, id, reason string) error {
	changed, err := vault.New(c.vaultDir).Uncommitted(context.Background())
	c.watch.mu.Lock()
	if c.watch.atClose == nil {
		c.watch.atClose = map[string][]string{}
	}
	c.watch.atClose[id] = changed
	if err != nil && c.watch.err == nil {
		c.watch.err = err
	}
	c.watch.mu.Unlock()
	return c.WorkTracker.CloseStory(ctx, id, reason)
}

func registerLedgerWatchSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.When(`^mw lands the batch of the rig, stopped as soon as the first ledger line is written$`, func() error {
		c.batch.ledgerWatch = &ledgerWatch{}
		return c.land(false)
	})
	ctx.Then(`^the vault held no modified tracked file as each story was closed$`, func() error {
		watch := c.batch.ledgerWatch
		if watch == nil || len(watch.atClose) == 0 {
			return fmt.Errorf("no story was closed while the vault was watched\n%s", c.said())
		}
		if watch.err != nil {
			return watch.err
		}
		for id, changed := range watch.atClose {
			if len(changed) != 0 {
				return fmt.Errorf("expected the vault to hold nothing uncommitted as %s was closed, got %q", id, changed)
			}
		}
		return nil
	})
}
