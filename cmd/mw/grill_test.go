package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// grillHome is channelHome with a stand-in bd that knows mw-a.1 as a grilling
// ticket with no note yet; it returns what the sends carried and the bd log.
func grillHome(t *testing.T) (plaintexts *[]string, bdLog string) {
	t.Helper()
	plaintexts, bdLog = channelHome(t)
	bin := t.TempDir()
	stub := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
while [ "$1" = "-C" ] || [ "$1" = "--actor" ]; do shift 2; done
case "$1" in
show) echo '[{"id":"mw-a.1","title":"A grilling","status":"open","issue_type":"task","labels":["grilling"]}]';;
kv) echo "$3 (not set)"; exit 1;;
esac
exit 0
`, bdLog)
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return plaintexts, bdLog
}

func runGrill(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"grill"}, args...))
	err := root.Execute()
	return out.String(), err
}

// Over his review-queue limit the question is written on the ticket as a
// QUESTION for Monday, no card is sent, one line names the bead, the queue and
// the limit, and it exits 0.
func TestGrillAskOverTheReviewQueueLimitIsHeldForMonday(t *testing.T) {
	withReviewQueue(t, 11, 8)
	plaintexts, bdLog := grillHome(t)

	out, err := runGrill(t, "mw-a.1", "--ask", "Where does it live?", "--recommend", "A: yes", "--option", "A: yes", "--option", "B: no")

	if err != nil {
		l, _ := os.ReadFile(bdLog)
		t.Fatalf("a held question is not an error: %v\n%s\nbd:\n%s", err, out, l)
	}
	if len(*plaintexts) != 0 {
		t.Fatalf("expected no card sent, got %v", *plaintexts)
	}
	calls, _ := os.ReadFile(bdLog)
	if !strings.Contains(string(calls), "comment mw-a.1 QUESTION for Monday: Where does it live?") {
		t.Errorf("expected the question commented on the ticket, bd was asked:\n%s", calls)
	}
	if strings.Contains(string(calls), "kv set") {
		t.Errorf("expected the grilling note left alone, bd was asked:\n%s", calls)
	}
	for _, want := range []string{"mw-a.1", "11", "8", "Monday"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the line to name %q, got:\n%s", want, out)
		}
	}
}

func TestGrillAskRefusesAnEmptyWhyNow(t *testing.T) {
	withReviewQueue(t, 11, 8)
	plaintexts, bdLog := grillHome(t)

	for _, why := range []string{"", "   "} {
		out, err := runGrill(t, "mw-a.1", "--ask", "Where?", "--why-now", why)
		if err == nil || !strings.Contains(err.Error(), "--why-now") {
			t.Errorf("--why-now %q: expected a refusal naming --why-now, got %v\n%s", why, err, out)
		}
	}
	calls, _ := os.ReadFile(bdLog)
	if len(*plaintexts) != 0 || strings.Contains(string(calls), "comment") {
		t.Errorf("expected nothing sent or written, got %v / %s", *plaintexts, calls)
	}
}

func TestGrillAskHelpSaysWhyNowAndMonday(t *testing.T) {
	out, err := runGrill(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--why-now", "Monday", "review_queue"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected --help to say %q:\n%s", want, out)
		}
	}
}
