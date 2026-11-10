package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// aStandInBeadsServer is a stand-in for the beads server a command reaches over
// its kept connection: it holds a small vault (an epic, a story under it that
// waits on a closed one) and records every query it is sent. A command that
// asks the vault's server for its database is given it. With fail set it
// answers every query with an error.
type aStandInBeadsServer struct {
	fail atomic.Bool
	mu   sync.Mutex
	seen []string
}

var standInSeq atomic.Int32

func aServerToReach(t *testing.T) *aStandInBeadsServer {
	t.Helper()
	s := &aStandInBeadsServer{}
	name := fmt.Sprintf("cmd-kept-stand-in-%d", standInSeq.Add(1))
	sql.Register(name, s)
	was := openBeadsServer
	openBeadsServer = func(string) *sql.DB {
		db, err := sql.Open(name, "")
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	t.Cleanup(func() { openBeadsServer = was })
	return s
}

// queries is how many queries sent held the text.
func (s *aStandInBeadsServer) queries(text string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, q := range s.seen {
		if strings.Contains(strings.ToLower(q), text) {
			n++
		}
	}
	return n
}

type standInConn struct{ s *aStandInBeadsServer }
type standInRows struct {
	cols []string
	rows [][]driver.Value
	next int
}

func (s *aStandInBeadsServer) Open(string) (driver.Conn, error) { return standInConn{s}, nil }
func (c standInConn) Prepare(string) (driver.Stmt, error)       { return nil, errors.New("not used") }
func (c standInConn) Close() error                              { return nil }
func (c standInConn) Begin() (driver.Tx, error)                 { return nil, errors.New("not used") }
func (c standInConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.s.mu.Lock()
	c.s.seen = append(c.s.seen, query)
	c.s.mu.Unlock()
	if c.s.fail.Load() {
		return nil, errors.New("server went away")
	}
	q := strings.ToLower(query)
	switch {
	case strings.Contains(q, "from issues i") && strings.Contains(q, "leases"):
		return &standInRows{cols: []string{"id", "title", "description", "acceptance_criteria", "status", "issue_type", "assignee",
			"estimated_minutes", "created_at", "started_at", "updated_at", "closed_at", "priority", "metadata", "lease_expires_at", "comment_count"},
			rows: [][]driver.Value{
				{"e-1", "The epic", "", "", "open", "epic", nil, nil, "2026-10-01 10:00:00", nil, "2026-10-01 10:00:00", nil, int64(1), `{"rig":"millwright"}`, nil, int64(0)},
				{"e-1.1", "The story", "do it", "", "open", "task", nil, nil, "2026-10-01 10:05:00", nil, "2026-10-01 10:05:00", nil, int64(2), `{}`, nil, int64(0)},
				{"e-0", "Long gone", "", "", "closed", "task", nil, nil, "2026-08-01 00:00:00", nil, "2026-08-01 00:00:00", "2026-08-01 00:00:00", int64(2), `{}`, nil, int64(0)},
			}}, nil
	case strings.Contains(q, "select i.id, i.title, i.status"):
		return &standInRows{cols: []string{"id", "title", "status", "issue_type", "priority", "created_at"}, rows: [][]driver.Value{
			{"e-1", "The epic", "open", "epic", int64(1), "2026-10-01 10:00:00"},
			{"e-1.1", "The story", "open", "task", int64(2), "2026-10-01 10:05:00"},
		}}, nil
	case strings.Contains(q, "from labels"):
		return &standInRows{cols: []string{"issue_id", "label"}}, nil
	case strings.Contains(q, "from dependencies"):
		return &standInRows{cols: []string{"issue_id", "depends_on_id", "type"}, rows: [][]driver.Value{
			{"e-1.1", "e-1", "parent-child"}, {"e-1.1", "e-0", "blocks"},
		}}, nil
	case strings.Contains(q, "`value` as v from config"):
		return &standInRows{cols: []string{"k", "v"}, rows: [][]driver.Value{{"hands.x", "a note"}}}, nil
	case strings.Contains(q, "from config"):
		return &standInRows{cols: []string{"k", "id", "a", "b", "t", "at"}}, nil
	}
	return nil, fmt.Errorf("unexpected query %q", query)
}
func (r *standInRows) Columns() []string { return r.cols }
func (r *standInRows) Close() error      { return nil }
func (r *standInRows) Next(dest []driver.Value) error {
	if r.next >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}

// aBdThatRecords puts a bd on the PATH that records what it was asked and
// answers a read of the stand-in vault as bd would; it returns the file the
// calls are written to.
func aBdThatRecords(t *testing.T) (asked string) {
	t.Helper()
	bin := t.TempDir()
	asked = filepath.Join(t.TempDir(), "asked.log")
	listing := `[{"id":"e-1.1","title":"The story","status":"open","issue_type":"task","parent":"e-1","priority":2,"dependencies":[{"issue_id":"e-1.1","depends_on_id":"e-1","type":"parent-child"}]}]`
	epic := `[{"id":"e-1","title":"The epic","status":"open","issue_type":"epic","priority":1}]`
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + asked + "\n" +
		"case \"$*\" in\n" +
		"*\" show \"*) echo '" + epic + "' ;;\n" +
		"*\" kv \"*) echo '{}' ;;\n" +
		"*\" list \"*) echo '" + listing + "' ;;\n" +
		"*) echo '[]' ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, name := range []string{"BEADS_DIR", "BEADS_DB", "BEADS_DOLT_SERVER_HOST", "BEADS_DOLT_SERVER_MODE"} {
		t.Setenv(name, "")
	}
	return asked
}

// bdsAsked is how many bd were started.
func bdsAsked(t *testing.T, asked string) int {
	t.Helper()
	data, err := os.ReadFile(asked)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

// readAsked is what the stand-in bd was asked.
func readAsked(t *testing.T, asked string) string {
	t.Helper()
	data, err := os.ReadFile(asked)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(data)
}
