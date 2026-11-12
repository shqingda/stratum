// Package store is the only package that touches SQLite. It keeps
// sessions, an append-only log of each session's events, and receipts of
// commands, in ~/.talos/talos.db.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/shqingda/talos/llm"
)

// ErrNotFound means no such session or receipt.
var ErrNotFound = errors.New("store: not found")

// Store is the database.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	title      TEXT NOT NULL DEFAULT '',
	cwd        TEXT NOT NULL,
	account    TEXT NOT NULL DEFAULT '',
	model      TEXT NOT NULL DEFAULT '',
	effort     TEXT NOT NULL DEFAULT '',
	mode       TEXT NOT NULL DEFAULT 'ask',
	parent_id  TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	seq        INTEGER NOT NULL,
	type       TEXT NOT NULL,
	data       BLOB NOT NULL,
	at         INTEGER NOT NULL,
	PRIMARY KEY (session_id, seq)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS events_type ON events(session_id, type, seq);
CREATE TABLE IF NOT EXISTS receipts (
	id         TEXT PRIMARY KEY,
	session_id TEXT NOT NULL DEFAULT '',
	method     TEXT NOT NULL,
	status     TEXT NOT NULL,
	error      TEXT NOT NULL DEFAULT '',
	at         INTEGER NOT NULL
);
PRAGMA user_version = 1;
CREATE TABLE IF NOT EXISTS change_blobs (
	hash TEXT PRIMARY KEY,
	data BLOB NOT NULL
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS file_changes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	turn_seq INTEGER NOT NULL,
	path TEXT NOT NULL,
	before_hash TEXT REFERENCES change_blobs(hash),
	after_hash TEXT REFERENCES change_blobs(hash),
	mode INTEGER NOT NULL,
	reverted INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS file_changes_turn ON file_changes(session_id, turn_seq, id);
`

// Open opens or creates the database at path; ":memory:" for tests.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		dsn = "file:" + path
	}
	db, err := sql.Open("sqlite", dsn+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	// One connection: writes are serialized, and ":memory:" stays one
	// database.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Session is a conversation in a workspace.
type Session struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Cwd      string    `json:"cwd"`
	Account  string    `json:"account"`
	Model    string    `json:"model"`
	Effort   string    `json:"effort,omitempty"`
	Mode     string    `json:"mode"`
	ParentID string    `json:"parentId,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

const sessionCols = `id, title, cwd, account, model, effort, mode, parent_id, created_at, updated_at`

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var x Session
	var c, u int64
	err := row.Scan(&x.ID, &x.Title, &x.Cwd, &x.Account, &x.Model, &x.Effort, &x.Mode, &x.ParentID, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	x.Created, x.Updated = time.UnixMilli(c), time.UnixMilli(u)
	return x, err
}

// CreateSession stores a new session.
func (s *Store) CreateSession(ctx context.Context, x Session) error {
	now := time.Now()
	if x.Created.IsZero() {
		x.Created = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (`+sessionCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.Title, x.Cwd, x.Account, x.Model, x.Effort, x.Mode, x.ParentID, x.Created.UnixMilli(), now.UnixMilli())
	return err
}

// UpdateSession saves a session's settings and title.
func (s *Store) UpdateSession(ctx context.Context, x Session) error {
	r, err := s.db.ExecContext(ctx, `UPDATE sessions SET title=?, account=?, model=?, effort=?, mode=?, updated_at=? WHERE id=?`,
		x.Title, x.Account, x.Model, x.Effort, x.Mode, time.Now().UnixMilli(), x.ID)
	if err == nil {
		if n, _ := r.RowsAffected(); n == 0 {
			err = ErrNotFound
		}
	}
	return err
}

// GetSession loads a session.
func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	return scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE id=?`, id))
}

// Sessions lists sessions, most recently updated first.
func (s *Store) Sessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionCols+` FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// DeleteSession removes a session and its events.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id)
	return err
}

// Event is an entry of a session's log. Seq counts from 1 per session and
// doubles as the session's revision.
type Event struct {
	SessionID string          `json:"sessionId"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	At        time.Time       `json:"at"`
}

// Append adds an event to a session's log and marks the session updated.
func (s *Store) Append(ctx context.Context, sessionID, typ string, data any) (Event, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return Event{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM events WHERE session_id=?`, sessionID).Scan(&seq); err != nil {
		return Event{}, err
	}
	now := time.Now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO events (session_id, seq, type, data, at) VALUES (?,?,?,?,?)`,
		sessionID, seq, typ, b, now.UnixMilli()); err != nil {
		return Event{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at=? WHERE id=?`, now.UnixMilli(), sessionID); err != nil {
		return Event{}, err
	}
	return Event{SessionID: sessionID, Seq: seq, Type: typ, Data: b, At: now}, tx.Commit()
}

// Events lists a session's events after seq, oldest first; types filters
// when given.
func (s *Store) Events(ctx context.Context, sessionID string, after int64, limit int, types ...string) ([]Event, error) {
	q := `SELECT seq, type, data, at FROM events WHERE session_id=? AND seq>?`
	args := []any{sessionID, after}
	if len(types) > 0 {
		q += ` AND type IN (?` + repeat(",?", len(types)-1) + `)`
		for _, t := range types {
			args = append(args, t)
		}
	}
	q += ` ORDER BY seq LIMIT ?`
	if limit <= 0 {
		limit = -1
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e := Event{SessionID: sessionID}
		var at int64
		if err := rows.Scan(&e.Seq, &e.Type, &e.Data, &at); err != nil {
			return nil, err
		}
		e.At = time.UnixMilli(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Revision is the seq of a session's last event.
func (s *Store) Revision(ctx context.Context, sessionID string) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM events WHERE session_id=?`, sessionID).Scan(&seq)
	return seq, err
}

// MessageEvent is the event type that holds a message of the conversation.
const MessageEvent = "message"

// Messages loads a session's conversation.
func (s *Store) Messages(ctx context.Context, sessionID string) ([]llm.Message, error) {
	events, err := s.Events(ctx, sessionID, 0, 0, MessageEvent)
	if err != nil {
		return nil, err
	}
	out := make([]llm.Message, 0, len(events))
	for _, e := range events {
		var m llm.Message
		if err := json.Unmarshal(e.Data, &m); err != nil {
			return nil, fmt.Errorf("store: message %d of %s: %w", e.Seq, sessionID, err)
		}
		out = append(out, m)
	}
	return out, nil
}

// Receipt records what became of a command, so a client that lost the
// answer can ask.
type Receipt struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId,omitempty"`
	Method    string    `json:"method"`
	Status    string    `json:"status"` // accepted, completed, failed
	Error     string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}

// PutReceipt records or updates a receipt.
func (s *Store) PutReceipt(ctx context.Context, r Receipt) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO receipts (id, session_id, method, status, error, at) VALUES (?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, error=excluded.error, at=excluded.at`,
		r.ID, r.SessionID, r.Method, r.Status, r.Error, time.Now().UnixMilli())
	return err
}

// GetReceipt loads a receipt.
func (s *Store) GetReceipt(ctx context.Context, id string) (Receipt, error) {
	var r Receipt
	var at int64
	err := s.db.QueryRowContext(ctx, `SELECT id, session_id, method, status, error, at FROM receipts WHERE id=?`, id).
		Scan(&r.ID, &r.SessionID, &r.Method, &r.Status, &r.Error, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	r.At = time.UnixMilli(at)
	return r, err
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
