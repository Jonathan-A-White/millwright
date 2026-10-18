package beads_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// headDriver is a stand-in for the beads server's SQL driver: it counts the
// connections opened to it and the queries it answers, and says one hash, or
// fails every query when fail is set.
type headDriver struct {
	opens   atomic.Int32
	queries atomic.Int32
	fail    atomic.Bool
}

type headConn struct{ d *headDriver }
type headRows struct{ done bool }

var headDriverSeq atomic.Int32

// open registers a new driver under a name of its own (a test cannot share
// one) and returns the database opened on it.
func (d *headDriver) open(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("head-stand-in-%d", headDriverSeq.Add(1))
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("opening the stand-in database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func (d *headDriver) Open(string) (driver.Conn, error) {
	d.opens.Add(1)
	return headConn{d}, nil
}
func (c headConn) Prepare(query string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (c headConn) Close() error                              { return nil }
func (c headConn) Begin() (driver.Tx, error)                 { return nil, errors.New("not used") }
func (c headConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.d.queries.Add(1)
	if c.d.fail.Load() {
		return nil, errors.New("server went away")
	}
	if !strings.Contains(query, "dolt_hashof_db") {
		return nil, fmt.Errorf("unexpected query %q", query)
	}
	return &headRows{}, nil
}
func (r *headRows) Columns() []string { return []string{"head"} }
func (r *headRows) Close() error      { return nil }
func (r *headRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = "hash-of-the-beads"
	return nil
}

// bdStandIn is a stand-in bd that counts how many times it was started in a
// file, and answers a head query with a different hash, so a test can tell
// which road an answer came by.
func bdStandIn(t *testing.T) (program, started string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	started = filepath.Join(dir, "started.log")
	program = filepath.Join(dir, "bd-standin")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %s\nprintf '[{\"head\": \"hash-from-bd\"}]'\n", started)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, started
}

func startedCount(t *testing.T, started string) int {
	t.Helper()
	data, err := os.ReadFile(started)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("reading what the stand-in recorded: %v", err)
	}
	return strings.Count(string(data), "\n")
}

// The follower reads the beads' head every second. Reading it by starting a bd
// each time opened several connections to the server for every read (about
// 3.7 on 2026-10-18, mw-gq6.596): a Gateway given a database keeps one
// connection for its life, so a hundred reads open one, and start no bd.
func TestHeadIsReadOverOneConnectionKeptOpen(t *testing.T) {
	program, started := bdStandIn(t)
	d := &headDriver{}
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(d.open(t)))

	for i := 0; i < 100; i++ {
		head, err := g.Head(context.Background())
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if head != "hash-of-the-beads" {
			t.Fatalf("read %d: expected the hash the connection answered, got %q", i, head)
		}
	}
	if got := d.opens.Load(); got != 1 {
		t.Fatalf("expected one connection opened for 100 reads of the head, got %d", got)
	}
	if got := startedCount(t, started); got != 0 {
		t.Fatalf("expected no bd started while the connection answers, got %d", got)
	}
}

func TestHeadFallsBackToBdWhenTheConnectionFails(t *testing.T) {
	program, started := bdStandIn(t)
	d := &headDriver{}
	d.fail.Store(true)
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(d.open(t)))

	head, err := g.Head(context.Background())
	if err != nil {
		t.Fatalf("a failing connection must not fail the read: %v", err)
	}
	if head != "hash-from-bd" {
		t.Fatalf("expected the hash bd answered, got %q", head)
	}
	if got := startedCount(t, started); got != 1 {
		t.Fatalf("expected one bd started, got %d", got)
	}
}

// A connection that failed is not tried again on every second's read: the
// failing server would be dialled each time, as bd was.
func TestHeadLeavesAFailedConnectionAloneForAWhile(t *testing.T) {
	program, started := bdStandIn(t)
	d := &headDriver{}
	d.fail.Store(true)
	var mu sync.Mutex
	now := timeZero()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(d.open(t)), beads.WithClock(clock))

	for i := 0; i < 10; i++ {
		if _, err := g.Head(context.Background()); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}
	if got := d.queries.Load(); got != 1 {
		t.Fatalf("expected the failed connection to be asked once within the wait, got %d", got)
	}
	if got := startedCount(t, started); got != 10 {
		t.Fatalf("expected bd to answer all 10 reads, got %d", got)
	}

	d.fail.Store(false)
	mu.Lock()
	now = now.Add(beads.HeadConnRetryWait)
	mu.Unlock()
	head, err := g.Head(context.Background())
	if err != nil || head != "hash-of-the-beads" {
		t.Fatalf("expected the connection used again after the wait, got %q, %v", head, err)
	}
}

func timeZero() time.Time { return time.Date(2026, 10, 18, 6, 0, 0, 0, time.UTC) }
