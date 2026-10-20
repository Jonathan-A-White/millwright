package eventlog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
)

// writeLog puts n events straight into the log file, numbered from 1, the
// event at each seq being make(seq): appending 100000 events one at a time
// would fsync 100000 times.
func writeLog(t *testing.T, path string, n int, make func(seq uint64) events.Event) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for i := 1; i <= n; i++ {
		e := make(uint64(i))
		e.Seq = uint64(i)
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	seq := strings.TrimSuffix(path, filepath.Ext(path)) + ".seq"
	if err := os.WriteFile(seq, []byte(strconv.Itoa(n)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func jobEvent(actor, from, to, detail string) events.Event {
	return events.Event{Ts: ts, Kind: events.KindJob, Actor: actor, From: from, To: to, Detail: detail, Lane: events.LaneNormal}
}

// farBack is a log of 100000 events in which a batch-land job's scheduled and
// running events are the 5th and 6th, and nothing job-like follows but mail.
func farBack(t *testing.T) (*eventlog.Log, string) {
	path := filepath.Join(t.TempDir(), "events", "log.jsonl")
	writeLog(t, path, 100000, func(seq uint64) events.Event {
		switch seq {
		case 5:
			return jobEvent("batch-land@laptop", events.Start, events.JobScheduled, "landing of millwright (3 queued)")
		case 6:
			return jobEvent("batch-land@laptop", events.JobScheduled, events.JobRunning, "")
		}
		return mail("mw-" + strconv.FormatUint(seq, 10))
	})
	return eventlog.New(path), path
}

func TestNewestFindsAJobsEventsAHundredThousandEventsBack(t *testing.T) {
	log, _ := farBack(t)
	n, err := log.Newest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	job := n.Jobs["batch-land@laptop"]
	if n.Seq != 100000 || job.Last.Seq != 6 || job.Last.To != events.JobRunning || job.Scheduled.Seq != 5 || job.Running.Seq != 6 || job.Ended.Seq != 0 {
		t.Fatalf("newest = seq %d, batch-land %+v; want the job last running at seq 6 and scheduled at 5", n.Seq, job)
	}
	if n.Other.Seq != 100000 || n.First.Seq != 1 {
		t.Fatalf("newest other event = %d, first = %d; want 100000 and 1", n.Other.Seq, n.First.Seq)
	}
}

// The second read must not read the log's old events: they are made unreadable,
// and only a read that reaches them would fail. What it must return is the
// state it left and the events appended since.
func TestNewestReadsOnlyTheEventsSinceItsLastRead(t *testing.T) {
	log, path := farBack(t)
	if _, err := log.Newest(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Spoil the first 50000 lines: a read of them fails.
	lines := bytes.SplitAfter(data, []byte("\n"))
	for i := 0; i < 50000; i++ {
		lines[i] = []byte("not an event\n")
	}
	if err := os.WriteFile(path, bytes.Join(lines, nil), 0o600); err != nil {
		t.Fatal(err)
	}
	appendOK(t, log, jobEvent("batch-land@laptop", events.JobRunning, events.JobDone, ""), mail("mw-late"))
	n, err := log.Newest(context.Background())
	if err != nil {
		t.Fatalf("newest read the spoiled old events: %v", err)
	}
	job := n.Jobs["batch-land@laptop"]
	if n.Seq != 100002 || job.Last.To != events.JobDone || job.Last.Seq != 100001 || job.Ended.Seq != 100001 || job.Running.Seq != 6 || job.Scheduled.Detail != "landing of millwright (3 queued)" {
		t.Fatalf("newest = seq %d, batch-land %+v; want the job done at 100001 and its running event still seq 6", n.Seq, job)
	}
}

func TestNewestIsMadeAnewFromAStateFileThatCoversMoreThanTheLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "log.jsonl")
	log := eventlog.New(path)
	appendOK(t, log, jobEvent("dispatch@laptop", events.Start, events.JobScheduled, ""), mail("mw-1"), mail("mw-2"))
	if _, err := log.Newest(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A home begun again: the log and its head start over from 1.
	for _, p := range []string{path, strings.TrimSuffix(path, ".jsonl") + ".seq"} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	appendOK(t, log, mail("mw-new"))
	n, err := log.Newest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n.Seq != 1 || len(n.Jobs) != 0 || n.Other.Bead != "mw-new" {
		t.Fatalf("newest = %+v; want it made anew from the one event now in the log", n)
	}
}

func TestNewestSurvivesATrimOfTheLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "log.jsonl")
	log := eventlog.New(path)
	appendOK(t, log, jobEvent("doctor@laptop", events.Start, events.JobScheduled, ""), jobEvent("doctor@laptop", events.JobScheduled, events.JobRunning, ""), mail("mw-1"), mail("mw-2"))
	if _, err := log.Newest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Trim(context.Background(), 3, "2026-10-01"); err != nil {
		t.Fatal(err)
	}
	n, err := log.Newest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := n.Jobs["doctor@laptop"].Last; got.Seq != 2 || got.To != events.JobRunning {
		t.Fatalf("doctor's last event after a trim = %+v; want seq 2, running", got)
	}
}

func TestSinceBeyondTheFirstEventReadsFromTheLogsEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "log.jsonl")
	// Long lines make the backward read cross chunk edges and need a bigger chunk.
	long := strings.Repeat("x", 300<<10)
	writeLog(t, path, 40, func(seq uint64) events.Event {
		e := mail("mw-" + strconv.FormatUint(seq, 10))
		if seq%10 == 5 {
			e.Detail = long
		}
		return e
	})
	log := eventlog.New(path)
	for _, after := range []uint64{0, 1, 9, 10, 11, 25, 39, 40, 41, 100} {
		evs, err := log.Since(context.Background(), after)
		if err != nil {
			t.Fatalf("Since(%d): %v", after, err)
		}
		var want []uint64
		for s := after + 1; s <= 40; s++ {
			want = append(want, s)
		}
		if got := seqs(evs); len(got) != len(want) || (len(got) > 0 && (got[0] != want[0] || got[len(got)-1] != want[len(want)-1])) {
			t.Fatalf("Since(%d) = %v, want seqs %v", after, got, want)
		}
	}
}

func TestSinceSkipsATornLastLineAfterTheSeq(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "log.jsonl")
	log := eventlog.New(path)
	appendOK(t, log, mail("mw-1"), mail("mw-2"), mail("mw-3"))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"seq":4,"ts":"2026`)
	f.Close()
	evs, err := log.Since(context.Background(), 1)
	if err != nil || len(evs) != 2 || evs[1].Seq != 3 {
		t.Fatalf("Since(1) = %v, %v; want seqs 2 and 3 with the torn line left out", seqs(evs), err)
	}
}

var _ application.EventNewestLog = (*eventlog.Log)(nil)

// A state file an older build made lacks the pauses and the cancels: it is made
// anew from the whole log, once, so that PausedHost and CancelsSince find a
// pause far back (mw-gq6.659).
func TestNewestIsMadeAnewFromAStateFileOfAnOlderBuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "log.jsonl")
	writeLog(t, path, 3000, func(seq uint64) events.Event {
		switch seq {
		case 2:
			return events.Event{Ts: ts, Kind: events.KindControl, Actor: "mw@laptop", Detail: "pause-host vps", Lane: events.LaneNormal}
		case 3:
			return events.Event{Ts: ts, Kind: events.KindControl, Bead: "mw-a", Actor: "governor@postern", Detail: "cancel", Lane: events.LaneNormal}
		}
		return mail("mw-" + strconv.FormatUint(seq, 10))
	})
	old := `{"seq":3000,"first":{"seq":1},"other":{"seq":3000},"jobs":{}}` + "\n"
	if err := os.WriteFile(strings.TrimSuffix(path, ".jsonl")+".newest.json", []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	log := eventlog.New(path)

	pause, paused, err := application.PausedHost(context.Background(), log, "vps")
	if err != nil || !paused || pause.Seq != 2 || pause.Actor != "mw@laptop" {
		t.Fatalf("got pause %+v paused=%v err=%v, want the pause at seq 2 by mw@laptop", pause, paused, err)
	}
	cancels, err := application.CancelsSince(context.Background(), log, ts.Add(-time.Hour))
	if err != nil || len(cancels) != 1 || cancels[0].Bead != "mw-a" {
		t.Fatalf("got cancels %+v (%v), want one of mw-a", cancels, err)
	}
}
