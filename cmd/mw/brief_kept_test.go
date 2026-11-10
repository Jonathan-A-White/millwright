package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func briefOf(t *testing.T, id string, said *bytes.Buffer) (string, func()) {
	t.Helper()
	tracker, stop := keptBriefTracker(t.TempDir(), "laptop", said)
	var out bytes.Buffer
	if _, err := (application.Brief{Tracker: tracker, Out: &out}).Run(context.Background(), id); err != nil {
		stop()
		t.Fatalf("brief %s: %v", id, err)
	}
	return out.String(), stop
}

// mw-t5shinh.13: mw brief started a bd for the epic, one for its children and
// one for what they wait on. With a server to reach it reads them over one
// connection, from one list of every bead, and starts no bd.
func TestBriefReadsOverTheKeptConnectionAndStartsNoBd(t *testing.T) {
	asked := aBdThatRecords(t)
	server := aServerToReach(t)
	var said bytes.Buffer

	out, stop := briefOf(t, "e-1", &said)
	defer stop()
	if !strings.Contains(out, "The epic") || !strings.Contains(out, "The story") {
		t.Errorf("expected the epic and its story in the brief, got:\n%s", out)
	}
	if got := bdsAsked(t, asked); got != 0 {
		t.Errorf("expected no bd started while the connection answers, got %d", got)
	}
	if got := server.queries("leases"); got != 1 {
		t.Errorf("expected the list of every bead read once, got %d reads", got)
	}
}

// With no server to reach it reads by bd for the epic and its children, as it
// did, and not by a list of every bead.
func TestBriefReadsByBdWhenThereIsNoServerToReach(t *testing.T) {
	asked := aBdThatRecords(t)
	var said bytes.Buffer

	out, stop := briefOf(t, "e-1", &said)
	defer stop()
	if !strings.Contains(out, "The story") {
		t.Errorf("expected the story in the brief, got:\n%s", out)
	}
	data := readAsked(t, asked)
	if strings.Contains(data, "list --all --limit 0 --json") && !strings.Contains(data, "--parent") {
		t.Errorf("expected bd to list the epic's children, not every bead:\n%s", data)
	}
	if !strings.Contains(data, "show e-1") || !strings.Contains(data, "--parent e-1") {
		t.Errorf("expected bd show of the epic and a list of its children, got:\n%s", data)
	}
	if said.Len() != 0 {
		t.Errorf("expected nothing said, got %q", said.String())
	}
}

// A kept read that fails leaves the brief to bd, with the one line on stderr.
func TestBriefReadsByBdWhenTheKeptReadFails(t *testing.T) {
	aBdThatRecords(t)
	server := aServerToReach(t)
	server.fail.Store(true)
	var said bytes.Buffer

	out, stop := briefOf(t, "e-1", &said)
	defer stop()
	if !strings.Contains(out, "The story") {
		t.Errorf("expected the story in the brief, got:\n%s", out)
	}
	if got := strings.Count(said.String(), keptTried); got != 1 {
		t.Errorf("expected the one line on stderr, got %d: %q", got, said.String())
	}
}
