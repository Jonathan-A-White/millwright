package beads

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Reading the beads' head by starting a bd costs the server several new
// connections each time (3.7 on 2026-10-18), and the follower reads it every
// second: 3.8 million connections in four days, and a Dolt that never idled
// (mw-gq6.596). A Gateway given a database reads the head over one connection
// kept open for its life, and starts a bd only when that connection fails.

// HeadConnRetryWait is how long a head connection that failed is left alone,
// the reads in the meantime going by bd as they always did: a server that is
// down is not dialled every second.
const HeadConnRetryWait = 30 * time.Second

// headQueryBound is how long one read of the head over the connection may take.
const headQueryBound = 10 * time.Second

// WithHeadDB gives the Gateway the database its head is read over: one
// connection, kept open, in place of a bd started for each read. A failure of
// the connection is not a failure of the read: bd answers it, and the
// connection is tried again after HeadConnRetryWait.
func WithHeadDB(db *sql.DB) Option {
	return func(g *Gateway) { g.headDB = db }
}

// ServerHeadDB opens, without yet dialling, the database of the vault's beads
// server, from the same environment bd reads (BEADS_DOLT_SERVER_HOST, _PORT,
// _USER and BEADS_DOLT_PASSWORD) and the database name in .beads/metadata.json.
// It holds at most one connection, which it never closes of its own accord.
// When the host is not served by a beads server, or says too little to reach
// one, it returns nil: the head is then read by bd, as before.
func ServerHeadDB(vault string) *sql.DB {
	host := strings.TrimSpace(os.Getenv("BEADS_DOLT_SERVER_HOST"))
	if host == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(vault, ".beads", "metadata.json"))
	if err != nil {
		return nil
	}
	var meta struct {
		Database string `json:"dolt_database"`
	}
	if json.Unmarshal(data, &meta) != nil || meta.Database == "" {
		return nil
	}
	port := strings.TrimSpace(os.Getenv("BEADS_DOLT_SERVER_PORT"))
	if port == "" {
		port = "3307"
	}
	user := strings.TrimSpace(os.Getenv("BEADS_DOLT_SERVER_USER"))
	if user == "" {
		user = "root"
	}
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = os.Getenv("BEADS_DOLT_PASSWORD")
	cfg.Net = "tcp"
	cfg.Addr = host + ":" + port
	cfg.DBName = meta.Database
	cfg.Timeout = 5 * time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	return db
}

// headOverConnection reads the head over the kept connection, and says whether
// that is the answer: false when there is no connection, it is being left
// alone after a failure, or it failed now.
func (g *Gateway) headOverConnection(ctx context.Context) (string, bool) {
	if g.headDB == nil {
		return "", false
	}
	g.headMu.Lock()
	skip := g.clock().Before(g.headSkipUntil)
	g.headMu.Unlock()
	if skip {
		return "", false
	}
	bounded, cancel := context.WithTimeout(ctx, headQueryBound)
	defer cancel()
	var head string
	err := g.headDB.QueryRowContext(bounded, headQuery).Scan(&head)
	if head = strings.TrimSpace(head); err == nil && head != "" {
		return head, true
	}
	if ctx.Err() == nil {
		g.headMu.Lock()
		g.headSkipUntil = g.clock().Add(HeadConnRetryWait)
		g.headMu.Unlock()
	}
	return "", false
}
