package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// A database made before the sidebar's columns gains them, and keeping a
// session's flags leaves its time.
func TestFlags(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "stratum.db")
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', cwd TEXT NOT NULL,
		account TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', effort TEXT NOT NULL DEFAULT '', mode TEXT NOT NULL DEFAULT 'ask',
		parent_id TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
		INSERT INTO sessions (id, cwd, created_at, updated_at) VALUES ('old', '/w', 1, 2)`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	x, err := s.GetSession(ctx, "old")
	if err != nil || x.Pinned || x.Mark != "" {
		t.Fatalf("old session %+v, %v", x, err)
	}
	x.Pinned, x.Archived, x.Mark = true, true, "fail"
	if err := s.SetFlags(ctx, x); err != nil {
		t.Fatal(err)
	}
	y, _ := s.GetSession(ctx, "old")
	if !y.Pinned || !y.Archived || y.Mark != "fail" || !y.Updated.Equal(x.Updated) {
		t.Fatalf("flagged %+v", y)
	}
}
