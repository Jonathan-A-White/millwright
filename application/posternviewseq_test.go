package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// movingTracker is a tracker that writes an event to the log whenever the
// beads are read, as the factory does while a view is being built.
type movingTracker struct {
	*apptest.FakeTracker
	log *apptest.FakeEventLog
}

func (m movingTracker) LiveEpics(ctx context.Context) ([]string, error) {
	_, err := m.log.Append(ctx, []events.Event{{Kind: events.KindMessage, Ts: viewNow, Actor: "x", Lane: events.LaneNormal}})
	if err != nil {
		return nil, err
	}
	return m.FakeTracker.LiveEpics(ctx)
}

func seqView(log application.EventLog, tracker application.WorkTracker, notes application.PosternNotes) application.PosternView {
	return application.PosternView{Tracker: tracker, Notes: notes, Host: "desktop", Now: func() time.Time { return viewNow }, Events: log}
}

func appendMessages(t *testing.T, log *apptest.FakeEventLog, n int) {
	t.Helper()
	for range n {
		if _, err := log.Append(context.Background(), []events.Event{{Kind: events.KindMessage, Ts: viewNow, Actor: "x", Lane: events.LaneNormal}}); err != nil {
			t.Fatalf("appending: %v", err)
		}
	}
}

func TestTheViewCarriesTheEventLogHeadAsSeq(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{})
	log := &apptest.FakeEventLog{}
	appendMessages(t, log, 3)
	doc, err := seqView(log, tracker, tracker).Build(context.Background())
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if doc.Seq != 3 {
		t.Fatalf("seq is %d, want the head, 3", doc.Seq)
	}
}

func TestTheViewSeqIsTheHeadAtTheStartOfBuild(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{})
	log := &apptest.FakeEventLog{}
	appendMessages(t, log, 3)
	doc, err := seqView(log, movingTracker{tracker, log}, tracker).Build(context.Background())
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if head, _ := log.Head(context.Background()); head != 4 {
		t.Fatalf("the head is %d: the beads were not read", head)
	}
	if doc.Seq != 3 {
		t.Fatalf("seq is %d, want 3: the head before the beads were read, not the one after", doc.Seq)
	}
}

func TestTheViewHasNoSeqWithoutAnEventLogOrWithAnEmptyOne(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{})
	for name, log := range map[string]application.EventLog{"nil": nil, "empty": &apptest.FakeEventLog{}} {
		doc, err := seqView(log, tracker, tracker).Build(context.Background())
		if err != nil {
			t.Fatalf("%s: building: %v", name, err)
		}
		if doc.Seq != 0 {
			t.Errorf("%s: seq is %d, want none", name, doc.Seq)
		}
		encoded, _ := json.Marshal(doc)
		if strings.Contains(string(encoded), `"seq"`) {
			t.Errorf("%s: the JSON carries a seq: %s", name, encoded)
		}
	}
}

// Two builds that differ only in seq and written_at are one view to the memo:
// the second is not written again.
func TestTwoViewsDifferingOnlyInSeqAndWrittenAtShareADigest(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{})
	log := &apptest.FakeEventLog{}
	appendMessages(t, log, 1)
	now := viewNow
	file := apptest.NewFakeSnapshotFile("/tmp/view")
	view := seqView(log, tracker, tracker)
	view.Now = func() time.Time { return now }
	view.Cipher, view.GovernorKey, view.File = apptest.NewFakeCipher(), "governor-key", file
	view.Memo = &application.PosternViewMemo{}
	first, err := view.Run(context.Background())
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	appendMessages(t, log, 5)
	now = now.Add(time.Minute)
	second, err := view.Run(context.Background())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if first.Seq == second.Seq || first.WrittenAt == second.WrittenAt {
		t.Fatalf("the builds should differ in seq and written_at: %d %s / %d %s", first.Seq, first.WrittenAt, second.Seq, second.WrittenAt)
	}
	if got := file.Writes(); got != 1 {
		t.Fatalf("the file was written %d times, want once: the second view differed only in seq and written_at", got)
	}
}
