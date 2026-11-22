// Package store is the only package that touches SQLite. It keeps
// sessions, an append-only log of each session's events, and receipts of
// commands, in ~/.stratum/stratum.db.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/shqingda/stratum/llm"
)

// ErrNotFound means no such session or receipt.
var ErrNotFound = errors.New("store: not found")

// Store is the database.
type Store struct {
	db *sql.DB

	searchReady atomic.Bool
	searchMu    sync.Mutex
	searchDone  chan struct{}
	searchErr   error
	bgCancel    context.CancelFunc
	bgWG        sync.WaitGroup
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
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value BLOB NOT NULL
) WITHOUT ROWID;
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
	if err := migrateFlags(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: flags migration: %w", err)
	}
	s := &Store{
		db:         db,
		searchDone: make(chan struct{}),
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: user_version: %w", err)
	}
	if version >= 2 {
		s.searchReady.Store(true)
		close(s.searchDone)
		return s, nil
	}
	bgCtx, bgCancel := context.WithCancel(context.Background())
	s.bgCancel = bgCancel
	s.bgWG.Add(1)
	go s.runSearchMigration(bgCtx)
	return s, nil
}

func (s *Store) Close() error {
	if s.bgCancel != nil {
		s.bgCancel()
	}
	s.bgWG.Wait()
	return s.db.Close()
}

// migrateFlags adds the sidebar's columns to a sessions table made
// before them.
func migrateFlags(db *sql.DB) error {
	has := map[string]bool{}
	rows, err := db.Query(`SELECT name FROM pragma_table_info('sessions')`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		rows.Scan(&name)
		has[name] = true
	}
	rows.Close()
	for _, col := range []struct{ name, def string }{
		{"pinned", "INTEGER NOT NULL DEFAULT 0"}, {"archived", "INTEGER NOT NULL DEFAULT 0"}, {"mark", "TEXT NOT NULL DEFAULT ''"},
		{"agent", "TEXT NOT NULL DEFAULT ''"},
	} {
		if !has[col.name] {
			if _, err := db.Exec(`ALTER TABLE sessions ADD COLUMN ` + col.name + ` ` + col.def); err != nil {
				return err
			}
		}
	}
	return nil
}

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
	// Pinned, Archived and Mark are the user's, as the sidebar keeps
	// them: Mark is "unread" or "fail" for a turn that ended unseen.
	Pinned   bool   `json:"pinned,omitempty"`
	Archived bool   `json:"archived,omitempty"`
	Mark     string `json:"mark,omitempty"`
	// Agent is the primary agent that leads the turns; "" is build.
	Agent string `json:"agent,omitempty"`
}

const sessionCols = `id, title, cwd, account, model, effort, mode, parent_id, created_at, updated_at, pinned, archived, mark, agent`

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var x Session
	var c, u int64
	err := row.Scan(&x.ID, &x.Title, &x.Cwd, &x.Account, &x.Model, &x.Effort, &x.Mode, &x.ParentID, &c, &u, &x.Pinned, &x.Archived, &x.Mark, &x.Agent)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (`+sessionCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.Title, x.Cwd, x.Account, x.Model, x.Effort, x.Mode, x.ParentID, x.Created.UnixMilli(), now.UnixMilli(), x.Pinned, x.Archived, x.Mark, x.Agent)
	return err
}

// UpdateSession saves a session's settings, folder and title.
func (s *Store) UpdateSession(ctx context.Context, x Session) error {
	r, err := s.db.ExecContext(ctx, `UPDATE sessions SET title=?, cwd=?, account=?, model=?, effort=?, mode=?, agent=?, updated_at=? WHERE id=?`,
		x.Title, x.Cwd, x.Account, x.Model, x.Effort, x.Mode, x.Agent, time.Now().UnixMilli(), x.ID)
	if err == nil {
		if n, _ := r.RowsAffected(); n == 0 {
			err = ErrNotFound
		}
	}
	return err
}

// SetFlags keeps a session's pin, archive and mark, leaving when it was
// updated as it was.
func (s *Store) SetFlags(ctx context.Context, x Session) error {
	r, err := s.db.ExecContext(ctx, `UPDATE sessions SET pinned=?, archived=?, mark=? WHERE id=?`, x.Pinned, x.Archived, x.Mark, x.ID)
	if err == nil {
		if n, _ := r.RowsAffected(); n == 0 {
			err = ErrNotFound
		}
	}
	return err
}

// Setting reads the value kept under key, nil when there is none.
func (s *Store) Setting(ctx context.Context, key string) ([]byte, error) {
	var v []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// SetSetting keeps value under key.
func (s *Store) SetSetting(ctx context.Context, key string, value []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// DraftKey is the setting a session's draft is kept under.
func DraftKey(id string) string { return "draft:" + id }

// DeleteSetting forgets what is kept under key.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, key)
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_search WHERE session_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, DraftKey(id)); err != nil {
		return err
	}
	return tx.Commit()
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
	if typ == MessageEvent {
		ready := s.searchReady.Load()
		if !ready {
			s.searchMu.Lock()
			ready = s.searchReady.Load()
			s.searchMu.Unlock()
		}
		if ready {
			if err := indexMessage(ctx, tx, sessionID, seq, b); err != nil {
				return Event{}, err
			}
		}
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

// Truncate drops a session's log from seq on, and the search rows of
// those messages. File-change records stay, so a restore can still find them.
func (s *Store) Truncate(ctx context.Context, sessionID string, fromSeq int64) error {
	if fromSeq <= 0 {
		return errors.New("store: truncate needs a positive seq")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_search WHERE session_id=? AND seq>=?`, sessionID, fromSeq); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE session_id=? AND seq>=?`, sessionID, fromSeq); err != nil {
		return err
	}
	return tx.Commit()
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

// OpenTurnSessions returns the IDs of all sessions whose latest turn event
// is startType (i.e. left mid-turn without a matching endType).
func (s *Store) OpenTurnSessions(ctx context.Context, startType, endType string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.session_id
		FROM events e
		INNER JOIN (
			SELECT session_id, MAX(seq) AS max_seq
			FROM events
			WHERE type IN (?, ?)
			GROUP BY session_id
		) last ON e.session_id = last.session_id AND e.seq = last.max_seq
		WHERE e.type = ?`, startType, endType, startType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
