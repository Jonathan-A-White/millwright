package application_test

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// retryMirrorer answers the boost's index with indexErrs in turn (nil: the boost
// has no index), then the copies with copyErrs in turn; it counts its calls.
type retryMirrorer struct {
	indexErrs, copyErrs []error
	indexCalls, copies  int
}

func (r *retryMirrorer) LastIndexTime(_ context.Context, ssh []string, _ string) (time.Time, bool, error) {
	if len(ssh) == 0 {
		return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), true, nil
	}
	r.indexCalls++
	if len(r.indexErrs) > 0 {
		err := r.indexErrs[0]
		r.indexErrs = r.indexErrs[1:]
		return time.Time{}, false, err
	}
	return time.Time{}, false, nil
}

func (r *retryMirrorer) LastGrindTime(context.Context, []string, string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

func (r *retryMirrorer) DirExists(context.Context, string) (bool, error) { return false, nil }

func (r *retryMirrorer) ReadMillCursor(context.Context, string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

func (r *retryMirrorer) WriteMillCursor(context.Context, string, time.Time) error { return nil }

func (r *retryMirrorer) MillNames(context.Context, string, time.Time) ([]string, error) {
	return []string{"."}, nil
}

func (r *retryMirrorer) Copy(context.Context, string, []string, []string, string) error {
	r.copies++
	if len(r.copyErrs) > 0 {
		err := r.copyErrs[0]
		r.copyErrs = r.copyErrs[1:]
		return err
	}
	return nil
}

const connectTimedOut = "ssh desktop: exit status 255: ssh: connect to host 10.88.0.3 port 22: Connection timed out"

func runRetryMirror(t *testing.T, m *retryMirrorer) (string, []time.Duration, error) {
	t.Helper()
	out := &bytes.Buffer{}
	var waits []time.Duration
	err := application.PosternMirror{
		Files:    &apptest.FakeHomeFile{Text: "laptop 2026-10-01T00:00:00Z mw@laptop\n"},
		Mirrorer: m,
		Out:      out,
		Host:     "laptop",
		DataDir:  "/data",
		Reach:    map[string]string{"desktop": "ssh desktop"},
		Sleep: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			return nil
		},
	}.Run(context.Background())
	return out.String(), waits, err
}

func TestPosternMirrorRetriesOnceAfterAnSSHConnectTimeoutAndSaysSo(t *testing.T) {
	for _, text := range []string{
		connectTimedOut,
		"ssh desktop: exit status 255: Connection timed out during banner exchange",
	} {
		m := &retryMirrorer{indexErrs: []error{errors.New(text)}}
		out, waits, err := runRetryMirror(t, m)
		if err != nil {
			t.Fatalf("a lone timeout failed the run: %v", err)
		}
		if m.indexCalls != 2 || m.copies != 1 {
			t.Fatalf("index asked %d times, copied %d times; want 2 and 1", m.indexCalls, m.copies)
		}
		if len(waits) != 1 || waits[0] < 10*time.Second || waits[0] > 20*time.Second {
			t.Fatalf("waits = %v, want one of 10 to 20 s", waits)
		}
		if !strings.Contains(out, "retried") || !strings.Contains(out, "timed out") {
			t.Fatalf("no retry line saying why in:\n%s", out)
		}
	}
}

func TestPosternMirrorRetriesTheCopyToTheBoostToo(t *testing.T) {
	m := &retryMirrorer{copyErrs: []error{errors.New("rsync to desktop:/data/: exit status 255: ssh: connect to host 10.88.0.3 port 22: Connection timed out")}}
	out, _, err := runRetryMirror(t, m)
	if err != nil || m.copies != 2 {
		t.Fatalf("err = %v, copies = %d; want success after 2", err, m.copies)
	}
	if !strings.Contains(out, "retried") {
		t.Fatalf("no retry line in:\n%s", out)
	}
}

func TestPosternMirrorFailsWhenTheSSHTimesOutTwice(t *testing.T) {
	m := &retryMirrorer{indexErrs: []error{errors.New(connectTimedOut), errors.New(connectTimedOut)}}
	_, waits, err := runRetryMirror(t, m)
	if err == nil || !strings.Contains(err.Error(), "Connection timed out") {
		t.Fatalf("err = %v, want the second timeout", err)
	}
	if m.indexCalls != 2 || len(waits) != 1 {
		t.Fatalf("index asked %d times with %d waits; want 2 and 1", m.indexCalls, len(waits))
	}
}

func TestPosternMirrorFailsAtOnceOnAnyOtherSSHFailure(t *testing.T) {
	for _, text := range []string{
		"ssh desktop: exit status 255: Permission denied (publickey).",
		"ssh desktop: exit status 255: ssh: connect to host 10.88.0.3 port 22: Connection refused",
		"ssh desktop: exit status 1: tail: cannot open: timed out",
		"rsync to desktop:/data/: exit status 23: some files vanished",
	} {
		m := &retryMirrorer{indexErrs: []error{errors.New(text)}}
		out, waits, err := runRetryMirror(t, m)
		if err == nil || m.indexCalls != 1 || len(waits) != 0 || strings.Contains(out, "retried") {
			t.Fatalf("%q: err = %v, asked %d times, waits %v, out %q; want an immediate failure", text, err, m.indexCalls, waits, out)
		}
	}
}

func TestPosternMirrorTakesFilesVanishedMidCopyForASuccessAndSaysSo(t *testing.T) {
	m := &retryMirrorer{copyErrs: []error{&application.CopyVanished{Files: 2}}}
	out, waits, err := runRetryMirror(t, m)
	if err != nil {
		t.Fatalf("a copy that lost files to a vanishing blob failed the run: %v", err)
	}
	if m.copies != 1 || len(waits) != 0 {
		t.Fatalf("copied %d times with waits %v; want 1 and none", m.copies, waits)
	}
	want := "copied /data to desktop (2 files vanished mid-copy, the next run catches up)\n"
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestPosternMirrorSaysOneFileVanishedInTheSingular(t *testing.T) {
	m := &retryMirrorer{copyErrs: []error{&application.CopyVanished{Files: 1}}}
	out, _, err := runRetryMirror(t, m)
	if err != nil || !strings.Contains(out, "(1 file vanished mid-copy, the next run catches up)") {
		t.Fatalf("err = %v, out = %q", err, out)
	}
}

func TestPosternMirrorStillFailsOnARsyncPartialTransfer(t *testing.T) {
	m := &retryMirrorer{copyErrs: []error{errors.New("rsync to desktop:/data/: exit status 23: some files could not be transferred")}}
	out, _, err := runRetryMirror(t, m)
	if err == nil || strings.Contains(out, "copied") {
		t.Fatalf("err = %v, out = %q; want a failure and no copied line", err, out)
	}
}

// millMirrorer is a boost with no mill record, and a mill state directory whose
// run directories (runs) each have a change time; it keeps the copy cursor in
// memory and logs each copy's names.
type millMirrorer struct {
	retryMirrorer
	runs      map[string]time.Time
	cursor    time.Time
	hasCursor bool
	copies    [][]string
	failCopy  error
	listed    []time.Time
}

func (m *millMirrorer) DirExists(context.Context, string) (bool, error) { return true, nil }

func (m *millMirrorer) ReadMillCursor(context.Context, string) (time.Time, bool, error) {
	return m.cursor, m.hasCursor, nil
}

func (m *millMirrorer) WriteMillCursor(_ context.Context, _ string, at time.Time) error {
	m.cursor, m.hasCursor = at, true
	return nil
}

func (m *millMirrorer) MillNames(_ context.Context, _ string, since time.Time) ([]string, error) {
	m.listed = append(m.listed, since)
	names := []string{"grinds.jsonl"}
	if since.IsZero() {
		return append(names, "runs"), nil
	}
	for run, at := range m.runs {
		if at.After(since) {
			names = append(names, "runs/"+run)
		}
	}
	sort.Strings(names[1:])
	return names, nil
}

func (m *millMirrorer) Copy(_ context.Context, dir string, names []string, _ []string, dest string) error {
	if strings.HasPrefix(dest, "desktop:/mill") {
		m.copies = append(m.copies, names)
		return m.failCopy
	}
	return nil
}

func runMillMirror(t *testing.T, m *millMirrorer, now time.Time) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	err := application.PosternMirror{
		Files:        &apptest.FakeHomeFile{Text: "laptop 2026-10-01T00:00:00Z mw@laptop\n"},
		Mirrorer:     m,
		Out:          out,
		Host:         "laptop",
		DataDir:      "/data",
		MillStateDir: "/mill",
		Reach:        map[string]string{"desktop": "ssh desktop"},
		Now:          func() time.Time { return now },
	}.Run(context.Background())
	return out.String(), err
}

func TestPosternMirrorSecondMillCopyNamesOnlyTheRunsThatChangedSinceTheFirst(t *testing.T) {
	t0 := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m := &millMirrorer{runs: map[string]time.Time{"direct:old": t0.Add(-time.Hour)}}
	if _, err := runMillMirror(t, m, t0); err != nil {
		t.Fatal(err)
	}
	m.runs["direct:new"] = t0.Add(5 * time.Minute)
	if _, err := runMillMirror(t, m, t0.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(m.copies) != 2 {
		t.Fatalf("copies = %v, want two", m.copies)
	}
	if got, want := strings.Join(m.copies[0], " "), "grinds.jsonl runs"; got != want {
		t.Errorf("first copy named %q, want %q", got, want)
	}
	if got, want := strings.Join(m.copies[1], " "), "grinds.jsonl runs/direct:new"; got != want {
		t.Errorf("second copy named %q, want %q", got, want)
	}
}

func TestPosternMirrorCopiesAllTheMillStateWhenThereIsNoCursorOrItIsOlderThanSevenDays(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	for name, cursor := range map[string]time.Time{"no cursor": {}, "eight days old": now.Add(-8 * 24 * time.Hour)} {
		m := &millMirrorer{runs: map[string]time.Time{"direct:a": now.Add(-time.Hour)}}
		m.cursor, m.hasCursor = cursor, !cursor.IsZero()
		if _, err := runMillMirror(t, m, now); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(m.copies) != 1 || strings.Join(m.copies[0], " ") != "grinds.jsonl runs" {
			t.Errorf("%s: copies = %v, want the whole of runs", name, m.copies)
		}
	}
}

func TestPosternMirrorCopiesOnlyTheChangesWhenTheCursorIsSixDaysOld(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m := &millMirrorer{runs: map[string]time.Time{"direct:a": now.Add(-time.Hour)}}
	m.cursor, m.hasCursor = now.Add(-6*24*time.Hour), true
	if _, err := runMillMirror(t, m, now); err != nil {
		t.Fatal(err)
	}
	if len(m.copies) != 1 || strings.Join(m.copies[0], " ") != "grinds.jsonl runs/direct:a" {
		t.Errorf("copies = %v", m.copies)
	}
}

func TestPosternMirrorFailedMillCopyLeavesTheCursorWhereItWas(t *testing.T) {
	t0 := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m := &millMirrorer{runs: map[string]time.Time{"direct:a": t0.Add(-time.Hour)}}
	if _, err := runMillMirror(t, m, t0); err != nil {
		t.Fatal(err)
	}
	first := m.cursor
	m.runs["direct:b"] = t0.Add(time.Minute)
	m.failCopy = errors.New("rsync to desktop:/mill/: signal: killed")
	if _, err := runMillMirror(t, m, t0.Add(10*time.Minute)); err == nil {
		t.Fatal("a failed copy returned no error")
	}
	if !m.cursor.Equal(first) {
		t.Fatalf("cursor moved to %v from %v on a failed copy", m.cursor, first)
	}
	m.runs["direct:c"] = t0.Add(11 * time.Minute)
	m.failCopy = nil
	if _, err := runMillMirror(t, m, t0.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	last := strings.Join(m.copies[len(m.copies)-1], " ")
	if last != "grinds.jsonl runs/direct:b runs/direct:c" {
		t.Errorf("the copy after the failure named %q, want the missed run too", last)
	}
}
