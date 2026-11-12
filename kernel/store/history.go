package store

import (
	"context"
	"slices"
	"time"
)

// EventsBefore returns the nearest events below before in ascending sequence
// order. Types filters when given; a nonpositive limit returns all matches.
func (s *Store) EventsBefore(ctx context.Context, sessionID string, before int64, limit int, types ...string) ([]Event, error) {
	q := `SELECT seq, type, data, at FROM events WHERE session_id=? AND seq<?`
	args := []any{sessionID, before}
	if len(types) > 0 {
		q += ` AND type IN (?` + repeat(",?", len(types)-1) + `)`
		for _, typ := range types {
			args = append(args, typ)
		}
	}
	q += ` ORDER BY seq DESC LIMIT ?`
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
	slices.Reverse(out)
	return out, rows.Err()
}
