package application_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// markFailsOnce is a book whose first SetCurrent fails, as a pause killed after
// it wrote paused.json and before it marked the pause in force does.
type markFailsOnce struct {
	*apptest.FakePauseBook
	failed bool
}

func (b *markFailsOnce) SetCurrent(ctx context.Context, dir string) error {
	if !b.failed {
		b.failed = true
		return errors.New("context canceled")
	}
	return b.FakePauseBook.SetCurrent(ctx, dir)
}

// mw-gq6.1110: a second mw pause, run after a first that set cloud.max_boxes to 0
// and wrote paused.json but never marked itself in force, saw 0 as the value to
// restore and wrote it over the real one.
func TestASecondPauseKeepsTheMaxBoxesTheFirstSaved(t *testing.T) {
	book := &markFailsOnce{FakePauseBook: apptest.NewFakePauseBook()}
	config := &apptest.FakePauseConfig{Value: "4", Set: true}
	now := time.Date(2026, 11, 12, 15, 4, 0, 0, time.UTC)
	until := time.Date(2026, 11, 13, 5, 51, 8, 0, time.UTC)
	run := func(at time.Time) {
		t.Helper()
		fleet := apptest.NewFakePauseFleet()
		fleet.Add(application.FleetHostPause{Host: "laptop", Cap: "3", Notes: apptest.NewFakeTracker(), Stories: apptest.NewFakeTracker(),
			Runner: apptest.NewFakeRunner(), Git: apptest.NewFakePauseGit()})
		var out bytes.Buffer
		_, _ = application.Pause{
			Fleet: fleet, Cloud: apptest.NewFakePauseCloud(), Config: config, Book: book,
			Host: "laptop", Now: func() time.Time { return at }, Out: &out,
		}.Run(context.Background(), application.PauseRequest{Reason: "99.0% used", Until: until})
	}

	run(now)
	if got, _, _ := config.MaxBoxes(context.Background()); got != "0" {
		t.Fatalf("the first pause left cloud.max_boxes at %q, want 0", got)
	}
	run(now.Add(30 * time.Minute))

	set, ok := book.Set(application.PauseDir(now))
	if !ok {
		t.Fatal("no paused.json was kept")
	}
	if !set.MaxBoxesSet || set.MaxBoxes != "4" {
		t.Errorf("paused.json holds max_boxes %q (set %v), want the original 4", set.MaxBoxes, set.MaxBoxesSet)
	}
	if len(set.Hosts) != 1 || set.Hosts[0].Cap != "3" {
		t.Errorf("paused.json holds hosts %+v, want the laptop with its cap 3", set.Hosts)
	}
}
