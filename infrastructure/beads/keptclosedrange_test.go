package beads_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// closedAsk is one statement the recording server was sent, with its arguments.
type closedAsk struct {
	query string
	args  []string
}

// closedRecorder is a stand-in server that answers every read with no rows and
// keeps what it was asked.
type closedRecorder struct {
	mu   sync.Mutex
	seen []closedAsk
}

type closedRecorderConn struct{ s *closedRecorder }

type noRows struct{}

func (noRows) Columns() []string {
	return []string{"id", "title", "issue_type", "closed_at", "close_reason", "metadata"}
}
func (noRows) Close() error                   { return nil }
func (noRows) Next(dest []driver.Value) error { return io.EOF }

func (s *closedRecorder) Open(string) (driver.Conn, error) { return closedRecorderConn{s}, nil }
func (c closedRecorderConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not used")
}
func (c closedRecorderConn) Close() error              { return nil }
func (c closedRecorderConn) Begin() (driver.Tx, error) { return nil, errors.New("not used") }
func (c closedRecorderConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	ask := closedAsk{query: query}
	for _, a := range args {
		ask.args = append(ask.args, fmt.Sprint(a.Value))
	}
	c.s.mu.Lock()
	c.s.seen = append(c.s.seen, ask)
	c.s.mu.Unlock()
	return noRows{}, nil
}

func (s *closedRecorder) open(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("closed-recorder-%d", headDriverSeq.Add(1))
	sql.Register(name, s)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func (s *closedRecorder) closedReads() []closedAsk {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []closedAsk
	for _, a := range s.seen {
		if strings.Contains(a.query, "i.status = 'closed'") && strings.Contains(a.query, "from issues i where") {
			found = append(found, a)
		}
	}
	return found
}

// mw-gq6.1619: the closed stories of the fortnight, 15,000 of the 80,000 closed
// beads, took 1 to 2 s of the server at load: the only index on a closed bead is
// (status, updated_at), so a filter on closed_at alone reads every closed bead
// whole, its metadata too. A bead is closed at its last update (the two differ
// by up to a second at most), so a bound on updated_at, a little before the one
// on closed_at, lets the server read the fortnight's range of the index and the
// closed_at condition keeps the exact answer.
func TestTheClosedStoriesReadsRangeOverTheIndexedUpdateTime(t *testing.T) {
	since := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	reads := map[string]func(g *beads.Gateway) error{
		"the fortnight's closed stories": func(g *beads.Gateway) error {
			_, err := g.ClosedStories(context.Background(), since)
			return err
		},
		"one rig's closed stories": func(g *beads.Gateway) error {
			_, err := g.ClosedStoriesOfRig(context.Background(), "millwright", since, 5)
			return err
		},
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			s := &closedRecorder{}
			program, _ := standInSnapshotProgram(t, bdListing, bdTable)
			g := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(s.open(t)))
			if err := read(g); err != nil {
				t.Fatal(err)
			}
			asked := s.closedReads()
			if len(asked) != 1 {
				t.Fatalf("expected one read of the closed stories, got %d: %v", len(asked), asked)
			}
			q, args := asked[0].query, asked[0].args
			if !strings.Contains(q, "i.closed_at >= ?") || !strings.Contains(q, "i.updated_at >= ?") {
				t.Fatalf("expected a bound on closed_at and one on updated_at, got %s", q)
			}
			if got, want := strings.Count(q, "?"), len(args); got != want {
				t.Fatalf("%d placeholders and %d arguments in %s %v", got, want, q, args)
			}
			closedFrom, err := time.Parse(time.DateTime, args[0])
			if err != nil {
				t.Fatalf("the first argument is not the closed_at bound: %v", args)
			}
			updatedFrom, err := time.Parse(time.DateTime, args[1])
			if err != nil {
				t.Fatalf("the second argument is not the updated_at bound: %v", args)
			}
			if !closedFrom.Equal(since) {
				t.Errorf("expected the closed_at bound %s, got %s", since, closedFrom)
			}
			if updatedFrom.After(closedFrom) || closedFrom.Sub(updatedFrom) > 24*time.Hour {
				t.Errorf("expected the updated_at bound a little before the closed_at bound, got %s and %s", updatedFrom, closedFrom)
			}
		})
	}
}
