package giveway

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestStoreKeepsTheMarkerWholeAndTakesItAway(t *testing.T) {
	ctx := context.Background()
	s := Store{Dir: filepath.Join(t.TempDir(), "state")}
	if _, there, err := s.Read(ctx); err != nil || there {
		t.Fatalf("an empty store: there=%v err=%v", there, err)
	}
	marker := application.GiveWayMarker{
		Since: time.Date(2026, 11, 13, 16, 40, 0, 0, time.UTC), By: "mw@desktop", Seen: time.Date(2026, 11, 13, 16, 45, 0, 0, time.UTC),
		Sessions: []application.GiveWaySession{{Name: "mw-a_1", Story: "mw-a.1"}},
		Stopped:  []application.GiveWayStopped{{Pid: 12, Start: "345", Cmd: "claude"}},
	}
	if err := s.Write(ctx, marker); err != nil {
		t.Fatal(err)
	}
	got, there, err := s.Read(ctx)
	if err != nil || !there || !reflect.DeepEqual(got, marker) {
		t.Fatalf("read %+v there=%v err=%v, want %+v", got, there, err, marker)
	}
	if err := s.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx); err != nil {
		t.Fatalf("removing none: %v", err)
	}
	if _, there, _ := s.Read(ctx); there {
		t.Fatal("the marker is still there")
	}
}

func TestStoreRefusesAFileThatIsNotAMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MarkerFile), []byte("half a ma"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (Store{Dir: dir}).Read(context.Background()); err == nil || !strings.Contains(err.Error(), "remove it") {
		t.Fatalf("err = %v, want a refusal saying to remove it", err)
	}
}

func TestStoreAppendsLinesAndTailsTheLastOnes(t *testing.T) {
	ctx := context.Background()
	s := Store{Dir: t.TempDir()}
	if lines, err := s.Tail(ctx, 5); err != nil || len(lines) != 0 {
		t.Fatalf("an empty log: %q %v", lines, err)
	}
	for _, l := range []string{"a", "b", "c", "d"} {
		if err := s.Append(ctx, l+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	if lines, _ := s.Tail(ctx, 2); !reflect.DeepEqual(lines, []string{"c", "d"}) {
		t.Fatalf("tail = %q", lines)
	}
}

func TestParsePanesReadsSessionPidAndDeadness(t *testing.T) {
	got := ParsePanes("mw-a_1|200|0\nodd|name|301|0\nmw-b_2|0|1\nbroken\n")
	want := []application.GiveWayPane{{Session: "mw-a_1", Pid: 200}, {Session: "odd|name", Pid: 301}, {Session: "mw-b_2", Dead: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("panes = %+v, want %+v", got, want)
	}
}
