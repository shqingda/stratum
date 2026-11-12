package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFileChangeStorage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "changes.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err := s.CreateSession(ctx, Session{ID: "changes", Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ before, after []byte }{
		{nil, []byte{}}, {[]byte{}, []byte("same")}, {[]byte("same"), nil}, {nil, []byte("same")},
	} {
		if err := s.RecordFileChange(ctx, "changes", 12, "file", c.before, c.after, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM change_blobs`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("same/empty content not deduplicated: %d %v", count, err)
	}
	rows, err := s.FileChanges(ctx, "changes", 12)
	if err != nil || len(rows) != 4 {
		t.Fatalf("changes: %+v %v", rows, err)
	}
	if rows[0].Before != nil || rows[0].After == nil || len(rows[0].After) != 0 || rows[2].After != nil {
		t.Fatalf("missing and empty snapshots collapsed: %+v", rows)
	}
	if err := s.SetFileChangesReverted(ctx, []int64{rows[0].ID, rows[1].ID}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFileChangesReverted(ctx, []int64{rows[2].ID, -1}, true); err != ErrNotFound {
		t.Fatalf("missing change: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s.FileChanges(ctx, "changes", 0)
	if err != nil || len(rows) != 4 || !rows[0].Reverted || !rows[1].Reverted || rows[2].Reverted || rows[3].Reverted || rows[0].Mode != 0o640 {
		t.Fatalf("restore state not atomic/durable: %+v %v", rows, err)
	}
	if err := s.DeleteSession(ctx, "changes"); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.FileChanges(ctx, "changes", 0); err != nil || len(rows) != 0 {
		t.Fatalf("session changes not deleted: %+v %v", rows, err)
	}
	if err := s.RecordFileChange(ctx, "missing", 1, "file", nil, []byte("orphan"), 0o600); err == nil {
		t.Fatal("record accepted missing session")
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM change_blobs`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("failed record left blob: %d %v", count, err)
	}
}
