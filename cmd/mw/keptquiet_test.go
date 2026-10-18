package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// aBeadsServerThatRefuses points the commands at a beads server on a port
// nothing listens on, with a vault that names a database: a command that reads
// over a connection kept open tries it first and says in its log that it fell
// back to bd; one that starts a bd for each read never tries it.
func aBeadsServerThatRefuses(t *testing.T) {
	t.Helper()
	vault, err := config.Vault()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vault, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".beads", "metadata.json"), []byte(`{"dolt_database":"beads"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	listener.Close()
	t.Setenv("BEADS_DOLT_SERVER_HOST", "127.0.0.1")
	t.Setenv("BEADS_DOLT_SERVER_PORT", port)
}

const keptTried = "the kept beads connection failed"

// mw-gq6.613: contrib/mail-notify runs mw nudge, mw postern inbox --unread-count
// and, when the beads have moved, mw postern snapshot every minute, and each
// read of theirs started a bd. They read over one connection kept open for the
// run, as the follower and mw doctor do.
func TestThePosternSnapshotReadsOverAKeptBeadsConnection(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	aBeadsServerThatRefuses(t)

	out, err := runPostern(t, "snapshot", "--json")
	if err != nil {
		t.Fatalf("mw postern snapshot --json failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, keptTried) {
		t.Errorf("expected the snapshot to try the kept beads connection first, got:\n%s", out)
	}
}

func TestThePosternUnreadCountReadsOverAKeptBeadsConnection(t *testing.T) {
	url, _ := fakePosternBackend(t, map[string]string{"/api/messages?since=0&limit=200": `{"records":[],"next":0}`})
	f := loadPosternRecordFixture(t)
	posternHome(t, url, f.RecipientWIF, "")
	aBeadsServerThatRefuses(t)

	out, err := runPostern(t, "inbox", "--unread-count")
	if err != nil {
		t.Fatalf("mw postern inbox --unread-count failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, keptTried) {
		t.Errorf("expected the count to try the kept beads connection first, got:\n%s", out)
	}
}

func TestTheQuietAlarmReadsOverAKeptBeadsConnection(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	aBeadsServerThatRefuses(t)

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"nudge"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw nudge failed: %v\n%s", err, out)
	}
	if !strings.Contains(out.String(), keptTried) {
		t.Errorf("expected the quiet alarm to try the kept beads connection first, got:\n%s", out)
	}
}
