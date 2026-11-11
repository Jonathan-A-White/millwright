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

// A connect that times out twice is a boost that is down (a reboot, a sleeping
// host): the run says so and succeeds.
func TestPosternMirrorTakesAConnectThatTimesOutTwiceForABoostDown(t *testing.T) {
	m := &retryMirrorer{indexErrs: []error{errors.New(connectTimedOut), errors.New(connectTimedOut)}}
	out, waits, err := runRetryMirror(t, m)
	if err != nil || !strings.Contains(out, "unreachable") {
		t.Fatalf("err = %v, out %q; want success saying the boost is unreachable", err, out)
	}
	if m.indexCalls != 2 || len(waits) != 1 {
		t.Fatalf("index asked %d times with %d waits; want 2 and 1", m.indexCalls, len(waits))
	}
}

func TestPosternMirrorFailsAtOnceOnAnyOtherSSHFailure(t *testing.T) {
	for _, text := range []string{
		"ssh desktop: exit status 255: Permission denied (publickey).",
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

const connectRefused = "ssh desktop: exit status 255: ssh: connect to host 10.88.0.3 port 22: Connection refused"

// runDownBoostMirror runs the mirror with a watchdog target and a mill state
// directory, over m, and returns what it said and every destination copied to.
func runDownBoostMirror(t *testing.T, m *downBoostMirrorer) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	err := application.PosternMirror{
		Files:          &apptest.FakeHomeFile{Text: "laptop 2026-10-01T00:00:00Z mw@laptop\n"},
		Mirrorer:       m,
		Out:            out,
		Host:           "laptop",
		DataDir:        "/data",
		MillStateDir:   "/state",
		Reach:          map[string]string{"desktop": "ssh desktop"},
		WatchdogTarget: "root@vps:/var/lib/postern-watchdog/",
		Sleep:          func(context.Context, time.Duration) error { return nil },
	}.Run(context.Background())
	return out.String(), err
}

// downBoostMirrorer answers every ask of the boost with askErr and records the
// destinations copied to.
type downBoostMirrorer struct {
	retryMirrorer
	askErr error
	dests  []string
}

func (d *downBoostMirrorer) LastIndexTime(ctx context.Context, ssh []string, dir string) (time.Time, bool, error) {
	if len(ssh) > 0 {
		d.indexCalls++
		return time.Time{}, false, d.askErr
	}
	return d.retryMirrorer.LastIndexTime(ctx, ssh, dir)
}

func (d *downBoostMirrorer) LastGrindTime(_ context.Context, ssh []string, _ string) (time.Time, bool, error) {
	if len(ssh) > 0 {
		d.indexCalls++
		return time.Time{}, false, d.askErr
	}
	return time.Time{}, false, nil
}

func (d *downBoostMirrorer) DirExists(context.Context, string) (bool, error) { return true, nil }

func (d *downBoostMirrorer) Copy(_ context.Context, _ string, _ []string, _ []string, dest string) error {
	d.dests = append(d.dests, dest)
	return nil
}

// A boost that refuses the connection is down, not a fault of the run: the
// watchdog's files still go, one line says so, and the unit does not fail, so
// the doctor has nothing to mail every ten minutes (mw status shows the host).
func TestPosternMirrorWithTheBoostRefusingSSHStillFeedsTheWatchdogSaysSoOnceAndSucceeds(t *testing.T) {
	for _, text := range []string{
		connectRefused,
		"ssh desktop: exit status 255: ssh: connect to host 10.88.0.3 port 22: No route to host",
		"ssh desktop: exit status 255: ssh: connect to host 10.88.0.3 port 22: Network is unreachable",
		connectTimedOut,
	} {
		m := &downBoostMirrorer{askErr: errors.New(text)}
		out, err := runDownBoostMirror(t, m)
		if err != nil {
			t.Fatalf("%q: a down boost failed the run: %v", text, err)
		}
		if len(m.dests) != 1 || m.dests[0] != "root@vps:/var/lib/postern-watchdog/" {
			t.Fatalf("%q: copied to %v, want only the watchdog", text, m.dests)
		}
		if n := strings.Count(out, "desktop is unreachable ("); n != 1 || !strings.Contains(out, "unreachable") {
			t.Fatalf("%q: want one line saying desktop is unreachable, got:\n%s", text, out)
		}
		// A timeout is asked once more after the wait; a refusal is not.
		want := 1
		if application.SSHConnectTimedOut(errors.New(text)) {
			want = 2
		}
		if m.indexCalls != want {
			t.Fatalf("%q: the boost was asked %d times, want %d", text, m.indexCalls, want)
		}
	}
}

// A boost that is up and fails otherwise still fails the unit.
func TestPosternMirrorWithTheBoostUpStillFailsOnItsOtherFaults(t *testing.T) {
	for _, text := range []string{
		"ssh desktop: exit status 255: Permission denied (publickey).",
		"ssh desktop: exit status 255: Host key verification failed.",
		"ssh desktop: exit status 1: tail: cannot open",
	} {
		m := &downBoostMirrorer{askErr: errors.New(text)}
		_, err := runDownBoostMirror(t, m)
		if err == nil || !strings.Contains(err.Error(), "asking desktop for its index") {
			t.Fatalf("%q: err = %v, want the index failure", text, err)
		}
		if len(m.dests) != 1 {
			t.Fatalf("%q: the watchdog was not still fed: %v", text, m.dests)
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

// mw-6ww.163.1: a Boost the Governor retired is not asked at all (no ssh), one
// line says so, the watchdog's files still go, and the run succeeds.
func TestPosternMirrorSkipsARetiredBoostWithoutSSHAndStillFeedsTheWatchdog(t *testing.T) {
	m := &downBoostMirrorer{askErr: errors.New(connectRefused)}
	out := &bytes.Buffer{}
	err := application.PosternMirror{
		Files:          &apptest.FakeHomeFile{Text: "laptop 2026-10-01T00:00:00Z mw@laptop\n"},
		Mirrorer:       m,
		Out:            out,
		Host:           "laptop",
		DataDir:        "/data",
		MillStateDir:   "/state",
		Reach:          map[string]string{"desktop": "ssh desktop"},
		Retired:        []string{"desktop", "vps"},
		WatchdogTarget: "root@vps:/var/lib/postern-watchdog/",
		Sleep:          func(context.Context, time.Duration) error { return nil },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("a retired boost failed the run: %v", err)
	}
	if m.indexCalls != 0 {
		t.Errorf("the retired boost was asked %d times, want none", m.indexCalls)
	}
	if len(m.dests) != 1 || m.dests[0] != "root@vps:/var/lib/postern-watchdog/" {
		t.Errorf("copied to %v, want only the watchdog", m.dests)
	}
	if n := strings.Count(out.String(), "desktop is retired"); n != 1 || strings.Contains(out.String(), "unreachable") {
		t.Errorf("want one line saying desktop is retired, got:\n%s", out.String())
	}
}
