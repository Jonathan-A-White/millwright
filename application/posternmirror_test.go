package application_test

import (
	"bytes"
	"context"
	"errors"
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
