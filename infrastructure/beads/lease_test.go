package beads_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// A lease bd 1.3.0 grants runs five minutes and cannot be shortened, so what
// the gateway makes of one that has run out — a reclaim report, a listing
// with expired leases in it — is shown here with a stand-in for bd that
// prints what the real one printed when a lease really did run out
// (2026-09-25, in a throwaway vault).

// leaseStandIn writes a stand-in bd that answers each subcommand it is given
// with what says holds for it — printing it and exiting well, or, for a
// subcommand whose answer starts "fail:", printing the rest to standard error
// and exiting 1 — and records every argv. It returns a Gateway that runs it
// and the file the argvs are written to.
func leaseStandIn(t *testing.T, says map[string]string) (*beads.Gateway, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "asked.log")
	var script strings.Builder
	fmt.Fprintf(&script, "#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\n", log)
	script.WriteString("for a in \"$@\"; do\n  case \"$a\" in\n")
	for sub, answer := range says {
		if failing, ok := strings.CutPrefix(answer, "fail:"); ok {
			fmt.Fprintf(&script, "  %s) printf '%%s\\n' %s >&2; exit 1;;\n", sub, shellQuoted(failing))
			continue
		}
		fmt.Fprintf(&script, "  %s) printf '%%s\\n' %s; exit 0;;\n", sub, shellQuoted(answer))
	}
	script.WriteString("  esac\ndone\nexit 0\n")
	path := filepath.Join(dir, "bd-stand-in")
	if err := os.WriteFile(path, []byte(script.String()), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(dir, beads.WithProgram(path), beads.WithActor("mw@vps")), log
}

// shellQuoted is text as one single-quoted word of sh.
func shellQuoted(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

func TestReclaimStoryReadsWhatBdReclaimed(t *testing.T) {
	gateway, log := leaseStandIn(t, map[string]string{
		"reclaim": `{"count": 1, "reclaimed": [{"id": "t-e0o", "previous_owner": "alice"}], "schema_version": 1, "scoped": true}`,
	})
	reclaimed, err := gateway.ReclaimStory(context.Background(), "t-e0o")
	if err != nil || !reclaimed {
		t.Fatalf("expected t-e0o reclaimed, got %v: %v", reclaimed, err)
	}
	asked, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("reading what bd was asked: %v", err)
	}
	// The grace window is none: the caller has already judged the lease stale,
	// and bd still refuses one that holds. No --any-replica: a lease another
	// replica granted is that replica's to reap.
	if want := "reclaim --older-than 0s --id t-e0o --json"; !strings.Contains(string(asked), want) {
		t.Fatalf("expected bd to be asked %q, got %q", want, asked)
	}
	if strings.Contains(string(asked), "--any-replica") {
		t.Fatalf("expected no --any-replica, got %q", asked)
	}
}

func TestReclaimStoryReportsALeaseThatHoldsNotReclaimed(t *testing.T) {
	gateway, _ := leaseStandIn(t, map[string]string{
		"reclaim": `{"count": 0, "reclaimed": null, "schema_version": 1, "scoped": true}`,
	})
	reclaimed, err := gateway.ReclaimStory(context.Background(), "t-e0o")
	if err != nil || reclaimed {
		t.Fatalf("expected t-e0o not reclaimed, got %v: %v", reclaimed, err)
	}
}

func TestStaleClaimsListsOnlyLeasesPastNow(t *testing.T) {
	gateway, _ := leaseStandIn(t, map[string]string{
		"list": `[
  {"id": "t-old", "title": "Gone quiet", "status": "in_progress", "assignee": "alice", "lease_expires_at": "2026-09-25T23:11:26Z"},
  {"id": "t-new", "title": "Heartbeating", "status": "in_progress", "assignee": "alice", "lease_expires_at": "2026-09-25T23:30:00Z"},
  {"id": "t-none", "title": "Claimed elsewhere", "status": "in_progress", "assignee": "bob", "lease_expires_at": null}
]`,
	})
	now := time.Date(2026, 9, 25, 23, 20, 0, 0, time.UTC)
	stale, err := gateway.StaleClaims(context.Background(), now)
	if err != nil {
		t.Fatalf("listing the stale claims: %v", err)
	}
	if len(stale) != 1 || stale[0].Story.ID != "t-old" {
		t.Fatalf("expected only t-old stale at %v, got %+v", now, stale)
	}
}

// A claim bd refuses for some other reason than a holder is not reported as
// held: only a story read back in progress under someone else is.
func TestAClaimRefusedOnAnOpenStoryIsNotReportedHeld(t *testing.T) {
	gateway, _ := leaseStandIn(t, map[string]string{
		"update": "fail:Error: database is locked",
		"show":   `[{"id": "t-1", "title": "Free", "status": "open"}]`,
	})
	err := gateway.ClaimStory(context.Background(), "t-1")
	if err == nil {
		t.Fatalf("expected the claim to fail")
	}
	var held *application.ClaimHeldError
	if errors.As(err, &held) {
		t.Fatalf("expected a plain failure, got held by %s", held.Holder)
	}
	if !strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("expected bd's own words, got %v", err)
	}
}

func TestAClaimRefusedOnAStorySomeoneElseHoldsIsReportedHeld(t *testing.T) {
	gateway, _ := leaseStandIn(t, map[string]string{
		"update": "fail:Error updating t-1: issue already claimed by mw@laptop",
		"show":   `[{"id": "t-1", "title": "Taken", "status": "in_progress", "assignee": "mw@laptop"}]`,
	})
	err := gateway.ClaimStory(context.Background(), "t-1")
	var held *application.ClaimHeldError
	if !errors.As(err, &held) || held.Holder != "mw@laptop" || held.ID != "t-1" {
		t.Fatalf("expected t-1 held by mw@laptop, got %v", err)
	}
}
