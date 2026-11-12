package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
)

// FileChange preserves nil (absent) separately from a non-nil empty file.
type FileChange struct {
	ID, TurnSeq   int64
	Path          string
	Before, After []byte
	Mode          uint32
	Reverted      bool
}

func (s *Store) RecordFileChange(ctx context.Context, sessionID string, turnSeq int64, path string, before, after []byte, mode uint32) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hashes [2]any
	for i, data := range [][]byte{before, after} {
		if data == nil {
			continue
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(data))
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO change_blobs(hash,data) VALUES (?,?)`, hash, data); err != nil {
			return err
		}
		hashes[i] = hash
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO file_changes(session_id,turn_seq,path,before_hash,after_hash,mode) VALUES (?,?,?,?,?,?)`, sessionID, turnSeq, path, hashes[0], hashes[1], mode); err != nil {
		return err
	}
	return tx.Commit()
}

// FileChanges returns changes in recording order. A zero turnSeq lists all turns.
func (s *Store) FileChanges(ctx context.Context, sessionID string, turnSeq int64) ([]FileChange, error) {
	query := `SELECT c.id,c.turn_seq,c.path,c.before_hash,c.after_hash,b.data,a.data,c.mode,c.reverted
		FROM file_changes c LEFT JOIN change_blobs b ON b.hash=c.before_hash LEFT JOIN change_blobs a ON a.hash=c.after_hash
		WHERE c.session_id=?`
	args := []any{sessionID}
	if turnSeq != 0 {
		query += ` AND c.turn_seq=?`
		args = append(args, turnSeq)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY c.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FileChange{}
	for rows.Next() {
		var c FileChange
		var before, after sql.NullString
		if err := rows.Scan(&c.ID, &c.TurnSeq, &c.Path, &before, &after, &c.Before, &c.After, &c.Mode, &c.Reverted); err != nil {
			return nil, err
		}
		if before.Valid && c.Before == nil {
			c.Before = []byte{}
		}
		if after.Valid && c.After == nil {
			c.After = []byte{}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetFileChangesReverted updates one file's entire turn atomically.
func (s *Store) SetFileChangesReverted(ctx context.Context, ids []int64, reverted bool) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		r, err := tx.ExecContext(ctx, `UPDATE file_changes SET reverted=? WHERE id=?`, reverted, id)
		if err != nil {
			return err
		}
		if n, err := r.RowsAffected(); err != nil || n != 1 {
			if err != nil {
				return err
			}
			return ErrNotFound
		}
	}
	return tx.Commit()
}
