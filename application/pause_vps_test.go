package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// vpsFleet is a fleet of the laptop and the VPS, which runs no mw: asked to
// pause or resume, the VPS answers as a shell with no mw on its PATH does.
type vpsFleet struct {
	mu    sync.Mutex
	asked []string
}

func (f *vpsFleet) Hosts(context.Context) ([]string, error) { return []string{"laptop", "vps"}, nil }

func (f *vpsFleet) ask(host string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, host)
	if host == "vps" {
		return errors.New("ssh root@allmymind.org: exit status 127: bash: mw: command not found")
	}
	return nil
}

func (f *vpsFleet) Pause(_ context.Context, host string, _ application.HostPauseOrder) (application.HostPaused, error) {
	return application.HostPaused{Host: host}, f.ask(host)
}

func (f *vpsFleet) Resume(_ context.Context, host string, _ application.HostResumeOrder) (application.HostResumed, error) {
	return application.HostResumed{Host: host, Cleared: true}, f.ask(host)
}

func TestPauseSaysTheVPSRunsNoMwAndDoesNotCountItUnreached(t *testing.T) {
	fleet, book := &vpsFleet{}, apptest.NewFakePauseBook()
	var out bytes.Buffer
	set, err := application.Pause{
		Fleet: fleet, Cloud: apptest.NewFakePauseCloud(), Config: &apptest.FakePauseConfig{}, Book: book,
		Host: "laptop", Now: func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }, Out: &out,
	}.Run(context.Background(), application.PauseRequest{Reason: "the week is spent", Until: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "vps: no mw, skipped") {
		t.Fatalf("the pause does not say the VPS runs no mw:\n%s", out.String())
	}
	if strings.Contains(out.String(), "NOT REACHED") || len(set.Unreached) != 0 {
		t.Fatalf("the VPS is counted as not reached (%v):\n%s", set.Unreached, out.String())
	}
	if len(fleet.asked) != 1 || fleet.asked[0] != "laptop" {
		t.Fatalf("hosts asked to run mw: %v, want only the laptop", fleet.asked)
	}

	// A second mw pause finds nothing left to reach.
	var again bytes.Buffer
	if _, err := (application.Pause{
		Fleet: fleet, Cloud: apptest.NewFakePauseCloud(), Config: &apptest.FakePauseConfig{}, Book: book,
		Host: "laptop", Out: &again,
	}).Run(context.Background(), application.PauseRequest{Reason: "again"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(again.String(), "already paused") {
		t.Fatalf("a second pause tried the VPS again:\n%s", again.String())
	}
}

func TestResumeSaysTheVPSRunsNoMwAndDoesNotCountItUnreached(t *testing.T) {
	for name, set := range map[string]application.PausedSet{
		// A set this pause wrote, and one an earlier mw pause left with the VPS
		// among the hosts it did not reach.
		"new": {Hosts: []application.PausedSetHost{{Host: "laptop"}}, NoMw: []string{"vps"}},
		"old": {Hosts: []application.PausedSetHost{{Host: "laptop"}}, Unreached: []string{"vps"}},
	} {
		t.Run(name, func(t *testing.T) {
			set.PausedAt = time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
			book := apptest.NewFakePauseBook()
			const dir = "plans/pause-2026-10-30"
			if err := book.Write(context.Background(), dir, set); err != nil {
				t.Fatal(err)
			}
			if err := book.SetCurrent(context.Background(), dir); err != nil {
				t.Fatal(err)
			}
			fleet := &vpsFleet{}
			var out bytes.Buffer
			if err := (application.Resume{Fleet: fleet, Book: book, Retry: &apptest.FakePauseRetry{}, Host: "laptop", Out: &out}).Run(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "vps: no mw, skipped") {
				t.Fatalf("the resume does not say the VPS runs no mw:\n%s", out.String())
			}
			if strings.Contains(out.String(), "not reached") || strings.Contains(out.String(), "NOT REACHED") {
				t.Fatalf("the VPS is counted as not reached:\n%s", out.String())
			}
		})
	}
}
