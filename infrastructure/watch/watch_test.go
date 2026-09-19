package watch

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// None of these reach a network or a host: URLs are answered by a transport of
// the test's own, and ssh is a script standing in for it.

type transport func(*http.Request) (*http.Response, error)

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }

func answering(status int) *http.Client {
	return &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: http.NoBody, Request: r}, nil
	})}
}

func TestAnyReplyIsAnAnswer(t *testing.T) {
	for _, status := range []int{200, 301, 404, 502} {
		p := &Probes{Client: answering(status)}
		if !p.Reach(context.Background(), "https://blog.example") {
			t.Errorf("expected a %d to count as the URL answering", status)
		}
	}
}

func TestNoReplyIsNoAnswer(t *testing.T) {
	p := &Probes{Client: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: lookup blog.example: no such host")
	})}}
	if p.Reach(context.Background(), "https://blog.example") {
		t.Error("expected a URL that could not be reached not to answer")
	}
}

func TestAURLThatIsNotOneDoesNotAnswer(t *testing.T) {
	p := New(t.TempDir())
	if p.Reach(context.Background(), "nosuch://one.example") || p.Reach(context.Background(), "::") {
		t.Error("expected a URL nobody can fetch not to answer")
	}
}

// standInSSH is a script that records the arguments it was given, one per line,
// and then prints out and leaves with status.
func standInSSH(t *testing.T, out string, status int) (program, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	program = filepath.Join(dir, "ssh")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >>" + argsFile + "; done\n" +
		"printf '%s' '" + out + "'\nexit " + strconv.Itoa(status) + "\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return program, argsFile
}

func TestSSHRunsExactlyCatOfTheHealthFileInBatchMode(t *testing.T) {
	program, argsFile := standInSSH(t, "2026-09-19T09:15:02Z verdict=ok\n", 0)
	p := &Probes{SSH: program}

	got, err := p.ReadHealth(context.Background(), "vps-ssh")
	if err != nil {
		t.Fatalf("expected the read to work, got %v", err)
	}
	if got != "2026-09-19T09:15:02Z verdict=ok\n" {
		t.Errorf("expected the line the host printed, got %q", got)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "-o\nBatchMode=yes\n-o\nConnectTimeout=10\n--\nvps-ssh\ncat ~/.mw-health\n"
	if string(args) != want {
		t.Errorf("expected ssh to be run with exactly\n%s\ngot\n%s", want, args)
	}
}

func TestSSHFailingIsAnError(t *testing.T) {
	program, _ := standInSSH(t, "", 255)
	if _, err := (&Probes{SSH: program}).ReadHealth(context.Background(), "vps-ssh"); err == nil {
		t.Error("expected ssh's own failure, status 255, to be an error")
	}
}

func TestSSHThatIsNotThereIsAnError(t *testing.T) {
	p := &Probes{SSH: filepath.Join(t.TempDir(), "no-such-ssh")}
	if _, err := p.ReadHealth(context.Background(), "vps-ssh"); err == nil {
		t.Error("expected a program that cannot be run to be an error")
	}
}

func TestAMissingHealthFileIsAnEmptyAnswerNotAFailedSSH(t *testing.T) {
	program, _ := standInSSH(t, "", 1)
	got, err := (&Probes{SSH: program}).ReadHealth(context.Background(), "vps-ssh")
	if err != nil || got != "" {
		t.Errorf("expected ssh getting through to the host to be an answer, of nothing, got %q, %v", got, err)
	}
}

func TestMemoryIsKeptAndForgotten(t *testing.T) {
	p := New(filepath.Join(t.TempDir(), "state"))
	ctx := context.Background()

	if got, err := p.LoadMemory(ctx); err != nil || !got.FirstFailure.IsZero() {
		t.Fatalf("expected nothing remembered at first, got %v, %v", got, err)
	}

	first := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	if err := p.SaveMemory(ctx, application.WatchMemory{FirstFailure: first}); err != nil {
		t.Fatal(err)
	}
	if got, err := p.LoadMemory(ctx); err != nil || !got.FirstFailure.Equal(first) {
		t.Errorf("expected %v remembered, got %v, %v", first, got, err)
	}

	if err := p.SaveMemory(ctx, application.WatchMemory{}); err != nil {
		t.Fatal(err)
	}
	if got, err := p.LoadMemory(ctx); err != nil || !got.FirstFailure.IsZero() {
		t.Errorf("expected the failure forgotten, got %v, %v", got, err)
	}
	if err := p.SaveMemory(ctx, application.WatchMemory{}); err != nil {
		t.Errorf("expected forgetting what is not remembered to be fine, got %v", err)
	}
}

func TestMemoryThatIsNotATimeIsNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MemoryFile), []byte("garbled\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := New(dir).LoadMemory(context.Background()); err != nil || !got.FirstFailure.IsZero() {
		t.Errorf("expected nothing remembered, got %v, %v", got, err)
	}
}

func TestTheLogGrowsOneLineAtATime(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	p := New(dir)
	for _, line := range []string{"2026-09-19T12:00:00Z ok", "2026-09-19T12:15:00Z local-fault"} {
		if err := p.AppendLog(context.Background(), line); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, LogFile))
	if err != nil {
		t.Fatal(err)
	}
	if want := "2026-09-19T12:00:00Z ok\n2026-09-19T12:15:00Z local-fault\n"; string(got) != want {
		t.Errorf("expected %q, got %q", want, strings.TrimSpace(string(got)))
	}
}
