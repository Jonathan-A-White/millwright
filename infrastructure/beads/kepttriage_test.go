package beads_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

type triageServer struct{}
type triageConn struct{}
type triageRows struct {
	cols []string
	rows [][]driver.Value
	next int
}

func (triageServer) Open(string) (driver.Conn, error)  { return triageConn{}, nil }
func (triageConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (triageConn) Close() error                        { return nil }
func (triageConn) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }
func (r *triageRows) Columns() []string                { return r.cols }
func (r *triageRows) Close() error                     { return nil }
func (triageConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	q := strings.ToLower(query)
	switch {
	case strings.Contains(q, "from issues i left join leases"):
		return &triageRows{cols: []string{"id", "title", "description", "acceptance_criteria", "status", "issue_type", "assignee",
			"estimated_minutes", "created_at", "started_at", "updated_at", "closed_at", "priority", "metadata", "lease_expires_at", "comment_count"},
			rows: [][]driver.Value{
				{"s-1", "A landing", "", "", "closed", "task", nil, nil, "2026-11-01 10:00:00", nil, "2026-11-02 10:00:00", "2026-11-02 10:00:00", int64(2), `{}`, nil, int64(0)},
			}}, nil
	case strings.Contains(q, "from labels"):
		return &triageRows{cols: []string{"issue_id", "label"}}, nil
	case strings.Contains(q, "from dependencies"):
		return &triageRows{cols: []string{"issue_id", "depends_on_id", "type"}}, nil
	case strings.Contains(q, "close_reason"):
		return &triageRows{cols: []string{"id", "close_reason"}, rows: [][]driver.Value{{"s-1", "landed abc123"}}}, nil
	}
	return nil, fmt.Errorf("unexpected query %q", query)
}
func (r *triageRows) Next(dest []driver.Value) error {
	if r.next >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}

var triageSeq atomic.Int32

// mw-gq6.1546: mw needs sweep read every bead by a bd of its own. Over the kept
// connection it reads them, with the reason each was closed for (which the
// listing of every bead leaves out), and starts no bd.
func TestTriageBeadsAreReadOverTheKeptConnectionWithTheirCloseReasons(t *testing.T) {
	program, asked := recordingBd(t)
	name := fmt.Sprintf("triage-stand-in-%d", triageSeq.Add(1))
	sql.Register(name, triageServer{})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	gateway := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithHeadDB(db))
	all, err := gateway.TriageBeads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != "s-1" || all[0].CloseReason != "landed abc123" {
		t.Errorf("expected the one bead with its close reason, got %+v", all)
	}
	if calls := bdCallsSince(t, asked, 0); len(calls) != 0 {
		t.Errorf("expected no bd started, got %v", calls)
	}
}
