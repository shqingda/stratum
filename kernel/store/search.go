package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shqingda/talos/llm"
	"modernc.org/sqlite"
)

const searchSchema = `CREATE VIRTUAL TABLE IF NOT EXISTS message_search USING fts5(
	session_id UNINDEXED, seq UNINDEXED, role UNINDEXED, text, tokenize='trigram'
);`

func migrateSearch(db *sql.DB) error {
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= 2 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, searchSchema); err != nil {
		return err
	}
	if err := backfillMessages(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version=2`); err != nil {
		return err
	}
	return tx.Commit()
}

func backfillMessages(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT session_id,seq,data FROM events WHERE type=? ORDER BY session_id,seq`, MessageEvent)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sessionID string
		var seq int64
		var data []byte
		if err := rows.Scan(&sessionID, &seq, &data); err != nil {
			return err
		}
		if err := indexMessage(ctx, tx, sessionID, seq, data); err != nil {
			return err
		}
	}
	return rows.Err()
}

func indexMessage(ctx context.Context, tx *sql.Tx, sessionID string, seq int64, data []byte) error {
	var message llm.Message
	if err := json.Unmarshal(data, &message); err != nil {
		return err
	}
	text := message.PlainText()
	if text == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO message_search(session_id,seq,role,text) VALUES (?,?,?,?)`, sessionID, seq, string(message.Role), text)
	return err
}

type SearchHit struct {
	SessionID string
	Seq       int64
	Role      string
	Snippet   string
	At        time.Time
}

// Search returns matching message text, ordered by relevance and newest
// first. Trigrams cannot match a query of fewer than three characters, as
// a two-character Chinese word: that one is looked for as a substring.
func (s *Store) Search(ctx context.Context, query, sessionID string, limit int) ([]SearchHit, error) {
	if strings.TrimSpace(query) == "" {
		return []SearchHit{}, nil
	}
	// FTS treats NUL as end-of-input even inside a quoted phrase.
	if strings.IndexByte(query, 0) >= 0 {
		return []SearchHit{}, nil
	}
	if limit <= 0 {
		limit = 500
	}
	limit = min(limit, 2000)
	if utf8.RuneCountInString(strings.TrimSpace(query)) < 3 {
		return s.searchShort(ctx, strings.TrimSpace(query), sessionID, limit)
	}
	hits, err := s.search(ctx, query, sessionID, limit)
	var sqlErr *sqlite.Error
	if errors.As(err, &sqlErr) && sqlErr.Code() == 1 {
		// Quoting the complete expression makes punctuation and bad FTS syntax literal.
		return s.search(ctx, `"`+strings.ReplaceAll(query, `"`, `""`)+`"`, sessionID, limit)
	}
	return hits, err
}

func (s *Store) search(ctx context.Context, query, sessionID string, limit int) ([]SearchHit, error) {
	q := `SELECT f.session_id,f.seq,f.role,snippet(message_search,3,'[',']','…',24),e.at
		FROM message_search f JOIN events e ON e.session_id=f.session_id AND e.seq=f.seq
		WHERE message_search MATCH ?`
	args := []any{query}
	if sessionID != "" {
		q += ` AND f.session_id=?`
		args = append(args, sessionID)
	}
	q += ` ORDER BY f.rank,e.at DESC,f.session_id,f.seq DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var hit SearchHit
		var at int64
		if err := rows.Scan(&hit.SessionID, &hit.Seq, &hit.Role, &hit.Snippet, &at); err != nil {
			return nil, err
		}
		hit.At = time.UnixMilli(at)
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

// searchShort finds query as a substring, newest first.
func (s *Store) searchShort(ctx context.Context, query, sessionID string, limit int) ([]SearchHit, error) {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	q := `SELECT f.session_id,f.seq,f.role,f.text,e.at
		FROM message_search f JOIN events e ON e.session_id=f.session_id AND e.seq=f.seq
		WHERE f.text LIKE ? ESCAPE '\'`
	args := []any{"%" + esc + "%"}
	if sessionID != "" {
		q += ` AND f.session_id=?`
		args = append(args, sessionID)
	}
	q += ` ORDER BY e.at DESC,f.session_id,f.seq DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var hit SearchHit
		var text string
		var at int64
		if err := rows.Scan(&hit.SessionID, &hit.Seq, &hit.Role, &text, &at); err != nil {
			return nil, err
		}
		hit.Snippet, hit.At = snippet(text, query, 12), time.UnixMilli(at)
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

// snippet is text around the first match of query, bracketed as FTS
// snippets are, with about n characters on each side.
func snippet(text, query string, n int) string {
	i := strings.Index(strings.ToLower(text), strings.ToLower(query))
	if i < 0 || len(strings.ToLower(text)) != len(text) {
		return text
	}
	pre, hit, post := []rune(text[:i]), text[i:i+len(query)], []rune(text[i+len(query):])
	out := ""
	if len(pre) > n {
		out, pre = "…", pre[len(pre)-n:]
	}
	out += string(pre) + "[" + hit + "]"
	if len(post) > n*2 {
		return out + string(post[:n*2]) + "…"
	}
	return out + string(post)
}
