// Package todoindex is the SQLite file that application.TodoIndex is: one
// FTS5 table with a row for each field of a To do card (its title, its
// description, each comment), which mw todo search fills from beads and asks.
// The file is a cache of beads: delete it and the next search makes it again.
package todoindex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Jonathan-A-White/millwright/application"

	_ "modernc.org/sqlite" // the pure-Go driver, so mw stays free of cgo
)

var _ application.TodoIndex = (*Index)(nil)

// Index is the index file at Path.
type Index struct {
	Path string
}

// snippetTokens is how many words of the matching field a hit's line shows.
const snippetTokens = 16

// Rebuild implements application.TodoIndex. It writes the new index beside the
// old one and moves it into place, so a search running at that moment reads
// whole file and a rebuild that fails leaves the old file as it was.
func (i Index) Rebuild(ctx context.Context, docs []application.TodoDoc) error {
	if err := os.MkdirAll(filepath.Dir(i.Path), 0o755); err != nil {
		return fmt.Errorf("making the directory of the To do index: %w", err)
	}
	next := fmt.Sprintf("%s.%d.new", i.Path, os.Getpid())
	_ = os.Remove(next)
	defer os.Remove(next)
	if err := fill(ctx, next, docs); err != nil {
		return err
	}
	if err := os.Rename(next, i.Path); err != nil {
		return fmt.Errorf("putting the new To do index in place: %w", err)
	}
	return nil
}

func fill(ctx context.Context, path string, docs []application.TodoDoc) (err error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("opening the To do index: %w", err)
	}
	defer func() {
		if cerr := db.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("closing the To do index: %w", cerr)
		}
	}()
	if _, err := db.ExecContext(ctx, `create virtual table doc using fts5(
		id unindexed, status unindexed, title unindexed, kind unindexed, body,
		tokenize = 'unicode61 remove_diacritics 2')`); err != nil {
		return fmt.Errorf("making the To do index: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("filling the To do index: %w", err)
	}
	defer tx.Rollback()
	insert, err := tx.PrepareContext(ctx, `insert into doc(id, status, title, kind, body) values (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("filling the To do index: %w", err)
	}
	defer insert.Close()
	for _, d := range docs {
		if _, err := insert.ExecContext(ctx, d.ID, d.Status, d.Title, d.Kind, d.Body); err != nil {
			return fmt.Errorf("filling the To do index with %s: %w", d.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("filling the To do index: %w", err)
	}
	return nil
}

// Search implements application.TodoIndex: the rows that hold every one of the
// words, best match first, at most limit. A word is matched whole, apart from
// case and accents, and the punctuation around it does not matter.
func (i Index) Search(ctx context.Context, words []string, limit int) ([]application.TodoHit, error) {
	match := matchOf(words)
	if match == "" {
		return nil, nil
	}
	if _, err := os.Stat(i.Path); err != nil {
		return nil, fmt.Errorf("reading the To do index: %w", err)
	}
	db, err := sql.Open("sqlite", i.Path)
	if err != nil {
		return nil, fmt.Errorf("opening the To do index: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `select id, status, title, kind,
		snippet(doc, 4, '', '', '…', ?) from doc where doc match ? order by rank limit ?`, snippetTokens, match, limit)
	if err != nil {
		return nil, fmt.Errorf("asking the To do index: %w", err)
	}
	defer rows.Close()
	var hits []application.TodoHit
	for rows.Next() {
		var h application.TodoHit
		if err := rows.Scan(&h.ID, &h.Status, &h.Title, &h.Kind, &h.Line); err != nil {
			return nil, fmt.Errorf("reading the To do index: %w", err)
		}
		h.Line = strings.Join(strings.Fields(h.Line), " ")
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// matchOf is the FTS5 query for words: each one a quoted phrase, so that no
// word of his can be read as FTS5's own syntax, all of them required. A word
// with no letter or digit in it has nothing to find and is left out.
func matchOf(words []string) string {
	var phrases []string
	for _, w := range words {
		if !strings.ContainsFunc(w, isWordRune) {
			continue
		}
		phrases = append(phrases, `"`+strings.ReplaceAll(w, `"`, `""`)+`"`)
	}
	return strings.Join(phrases, " ")
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
