package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

type mediaStore struct {
	runs    []application.GristMedia
	dropped []string
	failOn  string
}

func (m *mediaStore) RunMedia(context.Context) ([]application.GristMedia, error) { return m.runs, nil }
func (m *mediaStore) DropMedia(_ context.Context, name string) error {
	if name == m.failOn {
		return errors.New("disk said no")
	}
	m.dropped = append(m.dropped, name)
	return nil
}

type shelfStore struct {
	shelved []application.GristShelved
	dropped []string
}

func (s *shelfStore) Shelved(context.Context) ([]application.GristShelved, error) {
	return s.shelved, nil
}
func (s *shelfStore) DropShelved(_ context.Context, name string) error {
	s.dropped = append(s.dropped, name)
	return nil
}

type tidyLog struct{ lines []string }

func (l *tidyLog) Append(_ context.Context, line string) error {
	l.lines = append(l.lines, line)
	return nil
}
func (l *tidyLog) Read(context.Context) ([]string, error) { return l.lines, nil }

var tidyNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// mw-gq6.692: a run's media goes 30 days after the run, to the second; a run a
// second short of it is not touched.
func TestARunsMediaIsDeletedThirtyDaysAfterTheRunAndNotBefore(t *testing.T) {
	runs := &mediaStore{runs: []application.GristMedia{
		{Name: "old", Txid: "old", Received: tidyNow.Add(-31 * 24 * time.Hour), Files: 2, Bytes: 2048},
		{Name: "exactly", Txid: "exactly", Received: tidyNow.Add(-application.GristKeepAge), Files: 1, Bytes: 10},
		{Name: "almost", Txid: "almost", Received: tidyNow.Add(-application.GristKeepAge + time.Second), Files: 1, Bytes: 10},
		{Name: "new", Txid: "new", Received: tidyNow.Add(-time.Hour), Files: 1, Bytes: 10},
	}}
	var out bytes.Buffer
	log := &tidyLog{}
	report, err := application.GristTidy{Runs: runs, Log: log, Now: func() time.Time { return tidyNow }, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runs.dropped, ","); got != "old,exactly" {
		t.Fatalf("expected old and exactly 30 days dropped, got %q", got)
	}
	if len(report.Acts) != 2 || !strings.Contains(out.String(), "old") || strings.Contains(out.String(), "almost") {
		t.Fatalf("expected a report of the two, got %d acts:\n%s", len(report.Acts), out.String())
	}
	if len(log.lines) != 2 || !strings.Contains(log.lines[0], application.GristTidyLogPrefix) {
		t.Fatalf("expected one tick log line each, got %v", log.lines)
	}
}

// mw-gq6.692: a forward is kept whole for 30 days from the forward, then its
// directory goes.
func TestAForwardIsKeptWholeThirtyDaysAndThenDeleted(t *testing.T) {
	shelf := &shelfStore{shelved: []application.GristShelved{
		{Name: "gone", Forwarded: tidyNow.Add(-application.GristKeepAge), Files: 3, Bytes: 3 << 20},
		{Name: "almost", Forwarded: tidyNow.Add(-application.GristKeepAge + time.Second), Files: 1},
		{Name: "new", Forwarded: tidyNow.Add(-24 * time.Hour), Files: 1},
	}}
	report, err := application.GristTidy{Forwards: shelf, Now: func() time.Time { return tidyNow }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(shelf.dropped, ","); got != "gone" {
		t.Fatalf("expected only gone dropped, got %q", got)
	}
	if !strings.Contains(report.String(), "3.0 MB") {
		t.Fatalf("expected the size in the report, got:\n%s", report.String())
	}
}

// A dry run says what it would delete and deletes and logs nothing.
func TestAGristTidyDryRunDeletesNothing(t *testing.T) {
	runs := &mediaStore{runs: []application.GristMedia{{Name: "old", Txid: "old", Received: tidyNow.Add(-40 * 24 * time.Hour), Files: 1}}}
	log := &tidyLog{}
	report, err := application.GristTidy{Runs: runs, Log: log, DryRun: true, Now: func() time.Time { return tidyNow }}.Run(context.Background())
	if err != nil || len(runs.dropped) != 0 || len(log.lines) != 0 || len(report.Acts) != 1 {
		t.Fatalf("expected one act, none done, got %+v %v %v %v", report, runs.dropped, log.lines, err)
	}
	if !strings.HasPrefix(report.Summary(), "grist tidy would delete 1 item") {
		t.Fatalf("got %q", report.Summary())
	}
}

// One run that cannot be deleted is a note and the pass goes on.
func TestAFailedDeleteIsANoteAndThePassGoesOn(t *testing.T) {
	runs := &mediaStore{failOn: "a", runs: []application.GristMedia{
		{Name: "a", Txid: "a", Received: tidyNow.Add(-40 * 24 * time.Hour)},
		{Name: "b", Txid: "b", Received: tidyNow.Add(-40 * 24 * time.Hour)},
	}}
	report, err := application.GristTidy{Runs: runs, Now: func() time.Time { return tidyNow }}.Run(context.Background())
	if err == nil || len(report.Notes) != 1 || strings.Join(runs.dropped, ",") != "b" {
		t.Fatalf("expected an error, a note and b dropped, got %v %v %v", err, report.Notes, runs.dropped)
	}
}

func TestNothingOldEnoughIsSaidSo(t *testing.T) {
	var out bytes.Buffer
	_, err := application.GristTidy{Runs: &mediaStore{}, Forwards: &shelfStore{}, Out: &out}.Run(context.Background())
	if err != nil || !strings.Contains(out.String(), "nothing is old enough") {
		t.Fatalf("got %v %q", err, out.String())
	}
}

// mw-gq6.692: the Millhand's tick on the host that runs the mill sweeps it, says
// so in its line, and logs the act under the prefix the tick's counts read past.
func TestTheMillhandTickSweepsTheMillAndSaysSo(t *testing.T) {
	r := newResumeTick()
	runs := &mediaStore{runs: []application.GristMedia{{Name: "old", Txid: "old", Received: r.now.Add(-31 * 24 * time.Hour), Files: 1, Bytes: 100}}}
	r.gristTidy = application.GristTidy{Runs: runs}
	report := r.run(t)
	if strings.Join(runs.dropped, ",") != "old" || !strings.Contains(report.Line, "grist tidy deleted 1 item") {
		t.Fatalf("expected old dropped and said in the line, got %v %q", runs.dropped, report.Line)
	}
	lines, _ := r.log.Read(context.Background())
	found := false
	for _, l := range lines {
		_, words, _ := strings.Cut(l, " ")
		found = found || strings.HasPrefix(words, application.GristTidyLogPrefix)
		if strings.HasPrefix(words, application.GristTidyLogPrefix) && !strings.HasPrefix(words, application.TidyLogPrefix) {
			t.Fatalf("expected the act's line under mw tidy's prefix, got %q", l)
		}
	}
	if !found {
		t.Fatalf("expected an act line in the tick log, got %v", lines)
	}
}

type cwdStore struct {
	kept    []application.GristCwdItem
	dropped []string
}

func (c *cwdStore) Kept(context.Context) ([]application.GristCwdItem, error) { return c.kept, nil }
func (c *cwdStore) DropKept(_ context.Context, name string) error {
	c.dropped = append(c.dropped, name)
	return nil
}

type recordStore struct {
	old     application.GristRecordOld
	cutoffs []time.Time
	dropped bool
}

func (r *recordStore) OldRecord(_ context.Context, cutoff time.Time) (application.GristRecordOld, error) {
	r.cutoffs = append(r.cutoffs, cutoff)
	return r.old, nil
}
func (r *recordStore) DropOldRecord(context.Context, time.Time) error {
	r.dropped = true
	return nil
}

// mw-gq6.982: a talk directory's instructions file or scratch directory goes
// 30 days after it was last written, to the second, and not before.
func TestATalkDirectoryItemIsDeletedThirtyDaysAfterItWasWrittenAndNotBefore(t *testing.T) {
	cwd := &cwdStore{kept: []application.GristCwdItem{
		{Name: "lampas/system-prompt-old.md", Modified: tidyNow.Add(-application.GristKeepAge), Files: 1, Bytes: 100 << 10},
		{Name: "lampas/system-prompt-almost.md", Modified: tidyNow.Add(-application.GristKeepAge + time.Second), Files: 1},
		{Name: "lampas/grist-1", Modified: tidyNow.Add(-31 * 24 * time.Hour), Files: 2},
		{Name: "lampas/grist-2", Modified: tidyNow.Add(-time.Hour)},
	}}
	log := &tidyLog{}
	report, err := application.GristTidy{Cwd: cwd, Log: log, Now: func() time.Time { return tidyNow }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cwd.dropped, ","); got != "lampas/system-prompt-old.md,lampas/grist-1" {
		t.Fatalf("expected the two old ones dropped, got %q", got)
	}
	if len(report.Acts) != 2 || len(log.lines) != 2 || !strings.Contains(log.lines[0], application.GristTidyLogPrefix) {
		t.Fatalf("expected two acts and a log line each, got %v %v", report.Acts, log.lines)
	}
}

// mw-gq6.982: the record is cut at GristKeepAge before now: a line 30 days old
// goes, one a second younger stays, which the adapter decides from the cutoff
// handed to it (at or before it goes).
func TestTheRecordIsCutThirtyDaysBeforeNow(t *testing.T) {
	record := &recordStore{old: application.GristRecordOld{Lines: 3, Bytes: 2048}}
	log := &tidyLog{}
	report, err := application.GristTidy{Record: record, Log: log, Now: func() time.Time { return tidyNow }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := tidyNow.Add(-application.GristKeepAge); len(record.cutoffs) != 1 || !record.cutoffs[0].Equal(want) {
		t.Fatalf("expected the cutoff %v, got %v", want, record.cutoffs)
	}
	if !record.dropped || len(report.Acts) != 1 || !strings.Contains(report.String(), "3 lines") || len(log.lines) != 1 {
		t.Fatalf("expected the record cut and said, got %v %+v", record.dropped, report)
	}
}

func TestARecordWithNothingOldIsLeftAlone(t *testing.T) {
	record := &recordStore{}
	report, err := application.GristTidy{Record: record, Now: func() time.Time { return tidyNow }}.Run(context.Background())
	if err != nil || record.dropped || len(report.Acts) != 0 {
		t.Fatalf("expected nothing done, got %v %v %+v", err, record.dropped, report)
	}
}

func TestADryRunCutsNoRecordAndDeletesNoTalkItem(t *testing.T) {
	record := &recordStore{old: application.GristRecordOld{Lines: 1, Bytes: 10}}
	cwd := &cwdStore{kept: []application.GristCwdItem{{Name: "a/grist-1", Modified: tidyNow.Add(-40 * 24 * time.Hour)}}}
	report, err := application.GristTidy{Record: record, Cwd: cwd, DryRun: true, Now: func() time.Time { return tidyNow }}.Run(context.Background())
	if err != nil || record.dropped || len(cwd.dropped) != 0 || len(report.Acts) != 2 {
		t.Fatalf("expected two acts, none done, got %v %v %v %+v", err, record.dropped, cwd.dropped, report)
	}
}
