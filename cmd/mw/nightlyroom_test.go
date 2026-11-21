package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/apartproc"
)

func testNightlyHosts(t *testing.T) sshNightlyHosts {
	t.Helper()
	apart := func(label string) *apartproc.Processes {
		return &apartproc.Processes{Dir: t.TempDir(), Program: "unused", Scope: noScopeRun(t), Poll: 20 * time.Millisecond, Label: label}
	}
	h := newSSHNightlyHosts("desktop", apart(""), apart("the timing run of "))
	h.hosts.reach = func(context.Context) (map[string]string, map[string]string, error) {
		return map[string]string{"desktop": "ssh desktop", "cloud2": "ssh mw@cloud2", "cloud14": "ssh mw@cloud14"}, nil, nil
	}
	return h
}

func TestTheNightlyHostBookAsksForRoomAndRunsTheSuiteAndTheTimingRunOnTheHost(t *testing.T) {
	log := aBookOfHostsOverSSH(t)
	h := testNightlyHosts(t)
	rooms, err := h.Rooms(context.Background(), "lampas")
	if err != nil || len(rooms) != 2 {
		t.Fatalf("expected the two other hosts asked, got %+v %v", rooms, err)
	}
	if err := h.Run(context.Background(), "cloud14", application.NightlySuiteKind, "lampas"); err != nil {
		t.Fatal(err)
	}
	if err := h.Run(context.Background(), "cloud14", application.NightlyTimingKind, "lampas"); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{
		"mw@cloud14 bash -lc 'mw suite '\\''lampas'\\'' --file'",
		"mw@cloud14 bash -lc 'mw timing '\\''lampas'\\'''",
	} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("expected %q run over ssh, got %q", want, calls)
		}
	}
	if err := h.Run(context.Background(), "cloud99", application.NightlySuiteKind, "lampas"); err == nil || !strings.Contains(err.Error(), "no way to reach cloud99") {
		t.Fatalf("expected a host that is not in the book refused, got %v", err)
	}
	if err := h.Run(context.Background(), "cloud14", "todo", "lampas"); err == nil {
		t.Fatal("expected a kind of run it does not know refused")
	}
}
