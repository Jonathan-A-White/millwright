package pause

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

type committer struct{ paths [][]string }

func (c *committer) Commit(_ context.Context, _ string, paths []string) ([]string, error) {
	c.paths = append(c.paths, paths)
	return paths, nil
}

type notes map[string]string

func (n notes) Note(_ context.Context, k string) (string, error) { return n[k], nil }
func (n notes) SetNote(_ context.Context, k, v string) error     { n[k] = v; return nil }
func (n notes) ClearNote(_ context.Context, k string) error      { delete(n, k); return nil }

func TestBookKeepsTheSetInTheVaultAndCommitsIt(t *testing.T) {
	root := t.TempDir()
	files, kv := &committer{}, notes{}
	book := Book{Root: root, Files: files, Notes: kv}
	set := application.PausedSet{Date: "2026-10-22", Reason: "x", Stories: []application.PausedStory{{Story: "mw-a.1", Branch: "mw/mw-a.1"}}}

	if err := book.Write(context.Background(), "plans/pause-2026-10-22", set); err != nil {
		t.Fatal(err)
	}
	if got := files.paths[0][0]; got != "plans/pause-2026-10-22/paused.json" {
		t.Fatalf("committed %q", got)
	}
	back, err := book.Read(context.Background(), "plans/pause-2026-10-22")
	if err != nil || len(back.Stories) != 1 || back.Stories[0].Branch != "mw/mw-a.1" {
		t.Fatalf("read back %+v, %v", back, err)
	}
	if _, ok, _ := book.Current(context.Background()); ok {
		t.Fatal("a pause is in force before one was marked")
	}
	_ = book.SetCurrent(context.Background(), "plans/pause-2026-10-22")
	if dir, ok, _ := book.Current(context.Background()); !ok || dir != "plans/pause-2026-10-22" {
		t.Fatalf("current %q %v", dir, ok)
	}
	_ = book.ClearCurrent(context.Background())
	if _, ok, _ := book.Current(context.Background()); ok {
		t.Fatal("still in force after it was cleared")
	}
}

func TestFleetReachesAnotherHostByAnSSHRunningMwHere(t *testing.T) {
	var got []string
	fleet := Fleet{
		Home:  "laptop",
		Reach: map[string]string{"desktop": "ssh desktop", "cloud1": "ssh mw@10.88.0.5"},
		Run: func(_ context.Context, argv []string) (string, error) {
			got = argv
			return `{"host":"desktop","cap":"4","hold_until":"2026-10-23T09:30:00Z"}`, nil
		},
	}
	hosts, _ := fleet.Hosts(context.Background())
	if strings.Join(hosts, ",") != "laptop,cloud1,desktop" {
		t.Fatalf("hosts %v", hosts)
	}
	paused, err := fleet.Pause(context.Background(), "desktop", application.HostPauseOrder{Reason: "it's late"})
	if err != nil || paused.Cap != "4" || paused.Host != "desktop" {
		t.Fatalf("paused %+v, %v", paused, err)
	}
	want := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "desktop"}
	if strings.Join(got[:6], " ") != strings.Join(want, " ") {
		t.Fatalf("ssh argv %q", got)
	}
	if !strings.HasPrefix(got[6], "bash -lc ") || !strings.Contains(got[6], "mw pause --here --order ") || !strings.Contains(got[6], "late") {
		t.Fatalf("remote command %q", got[6])
	}
}

func TestFleetRefusesAHostItHasNoWayToReach(t *testing.T) {
	_, err := Fleet{Home: "laptop"}.Resume(context.Background(), "desktop", application.HostResumeOrder{})
	if err == nil || !strings.Contains(err.Error(), "hands_hosts") {
		t.Fatalf("error %v", err)
	}
}

func TestFleetListsABoxItCannotReachAndSaysWhy(t *testing.T) {
	fleet := Fleet{Home: "laptop", Reach: map[string]string{"desktop": "ssh desktop"},
		Gaps: map[string]string{"cloud1": "cloud1 is not a peer of the hub: it has no address"}}
	hosts, _ := fleet.Hosts(context.Background())
	if strings.Join(hosts, ",") != "laptop,cloud1,desktop" {
		t.Errorf("hosts = %v, want the box that cannot be reached among them", hosts)
	}
	_, err := fleet.Pause(context.Background(), "cloud1", application.HostPauseOrder{})
	if err == nil || !strings.Contains(err.Error(), "no way to reach cloud1: cloud1 is not a peer of the hub") {
		t.Fatalf("expected the reason, got %v", err)
	}
}

func TestFleetRefusesWhatMwHereDidNotPrint(t *testing.T) {
	fleet := Fleet{Home: "laptop", Reach: map[string]string{"desktop": "ssh desktop"},
		Run: func(context.Context, []string) (string, error) { return "mw: command not found", nil }}
	if _, err := fleet.Pause(context.Background(), "desktop", application.HostPauseOrder{}); err == nil {
		t.Fatal("garbage was taken for a report")
	}
}

func TestTimerSchedulesAOneShotUserTimerAtTheTimeInUTC(t *testing.T) {
	var got []string
	timer := Timer{Mw: "/usr/local/bin/mw", Path: "/usr/bin", Run: func(_ context.Context, argv []string) (string, error) { got = argv; return "", nil }}
	unit, err := timer.Schedule(context.Background(), time.Date(2026, 10, 23, 9, 30, 0, 0, time.UTC), "mw-resume-2026-10-22")
	if err != nil || unit != "mw-resume-2026-10-22.timer" {
		t.Fatalf("unit %q, %v", unit, err)
	}
	line := strings.Join(got, " ")
	for _, want := range []string{"systemd-run --user", "--unit=mw-resume-2026-10-22", "--on-calendar=2026-10-23 09:30:00 UTC", "Persistent=true", "--setenv=PATH=/usr/bin", "/usr/local/bin/mw resume"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q is not in %q", want, line)
		}
	}
}

func TestTimerCancelIgnoresATimerThatIsGone(t *testing.T) {
	timer := Timer{Run: func(context.Context, []string) (string, error) {
		return "", os.ErrNotExist
	}}
	if err := timer.Cancel(context.Background(), "mw-resume-x.timer"); err == nil {
		t.Fatal("a real failure was swallowed")
	}
	gone := Timer{Run: func(context.Context, []string) (string, error) {
		return "", &notLoaded{}
	}}
	if err := gone.Cancel(context.Background(), "mw-resume-x.timer"); err != nil {
		t.Fatalf("a timer that is not loaded is no fault: %v", err)
	}
}

type notLoaded struct{}

func (*notLoaded) Error() string { return "Unit mw-resume-x.timer not loaded." }

func TestConfigReadsAndSetsMaxBoxesWithTheWayBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("cap = 5\n\n[cloud]\nmax_boxes = 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cfg := Config{Path: path, Out: &out}
	if v, set, err := cfg.MaxBoxes(context.Background()); err != nil || !set || v != "4" {
		t.Fatalf("max_boxes %q %v %v", v, set, err)
	}
	if err := cfg.SetMaxBoxes(context.Background(), "0"); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := cfg.MaxBoxes(context.Background()); v != "0" {
		t.Fatalf("max_boxes is %q", v)
	}
	if !strings.Contains(out.String(), "way back: cp ") {
		t.Fatalf("no way back printed: %q", out.String())
	}
	if err := cfg.SetMaxBoxes(context.Background(), "0"); err != nil {
		t.Fatalf("setting what it already holds is no fault: %v", err)
	}
}
